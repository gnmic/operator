//go:build integration

// Package pipelines covers Pipeline resolution: selectors, refs, label-driven
// membership, enable/disable, isolation between pipelines, and deletion.
package pipelines

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster = "c1"
	leaf1   = "leaf1"
	leaf2   = "leaf2"
	spine1  = "spine1"
	pathIF  = "/interface/statistics/in-octets"
	pathSys = "/system/name"
)

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "002-pipelines",
		RequireTargets: []string{leaf1, leaf2, spine1},
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

func waitClusterReady(t *testing.T) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, cluster)
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

// waitIdle removes any leftover Pipelines from a prior test (cleanup is
// best-effort and the Cluster can keep streaming until it reconciles the
// deletion), then waits until the wire is quiet.
func waitIdle(t *testing.T) {
	t.Helper()
	var list gnmicv1alpha1.PipelineList
	if err := s.K8s.Client.List(s.Ctx, &list, client.InNamespace(s.Namespace)); err == nil {
		for i := range list.Items {
			p := &list.Items[i]
			_ = s.K8s.Client.Delete(s.Ctx, p)
		}
	}
	harness.Wait(t, harness.Medium, "all pipelines gone", func() (bool, string) {
		var left gnmicv1alpha1.PipelineList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d pipeline(s) remain", len(left.Items))
	})
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		PipelinesCount: harness.I32(0),
		TargetsCount:   harness.I32(0),
	})
	harness.Wait(t, harness.Medium, "all simulated targets idle", func() (bool, string) {
		for _, name := range []string{leaf1, leaf2, spine1} {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
}

// applyPipeline applies a Pipeline fixture. Empty optional fields must still be
// present in vars because the renderer uses missingkey=error.
// Cleanup is deferred to waitIdle (and suite teardown) rather than t.Cleanup,
// so a late SSA delete cannot race the next test's Cluster reconcile.
func applyPipeline(t *testing.T, name string, vars map[string]any) {
	t.Helper()
	base := map[string]any{
		"Name":       name,
		"Enabled":    true,
		"TargetRefs": []string{},
		"TargetRole": "",
		"SubGroup":   "",
		"SubGroups":  []string{},
	}
	for k, v := range vars {
		base[k] = v
	}
	b, err := os.ReadFile("fixtures/pipeline.yaml")
	if err != nil {
		t.Fatalf("reading pipeline fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), base); err != nil {
		t.Fatalf("applying pipeline %s: %v", name, err)
	}
}

func waitPipelineReady(t *testing.T, name string, counts harness.PipelineCounts) {
	t.Helper()
	harness.WaitPipelineCounts(t, s.K8s, name, counts)
	harness.WaitPipelineCondition(t, s.K8s, name, harness.CondReady, metav1.ConditionTrue)
	harness.WaitPipelineCondition(t, s.K8s, name, harness.CondResourcesResolved, metav1.ConditionTrue)
}

func restoreSpineRole(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.Target(t, spine1), `{"metadata":{"labels":{"role":"spine"}}}`)
	})
}

func assertEstablishedStable(t *testing.T, before map[string]time.Time, names ...string) {
	t.Helper()
	for _, name := range names {
		was, now := before[name], s.GnmiGen.EstablishedAt(name)
		if !was.IsZero() && !now.IsZero() && now.Sub(was).Abs() > time.Second {
			t.Errorf("%s re-established: %s -> %s", name, was, now)
		}
	}
}

// TestPipe001_BindsTargetsBySelector checks targetSelectors resolve the match
// set and leave non-matching targets uncollected.
func TestPipe001_BindsTargetsBySelector(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "by-sel", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	waitPipelineReady(t, "by-sel", harness.PipelineCounts{
		TargetsCount:       harness.I32(2),
		SubscriptionsCount: harness.I32(1),
		OutputsCount:       harness.I32(1),
	})

	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	s.GnmiGen.WaitStreams(t, spine1, 0)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount: harness.I32(2),
	})
}

// TestPipe002_BindsTargetsByRef checks targetRefs and additive mixing with selectors.
func TestPipe002_BindsTargetsByRef(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "by-ref", map[string]any{
		"TargetRefs": []string{spine1},
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.WaitStreams(t, spine1, 1)
	s.GnmiGen.WaitStreams(t, leaf1, 0)
	s.GnmiGen.WaitStreams(t, leaf2, 0)
	harness.WaitPipelineCounts(t, s.K8s, "by-ref", harness.PipelineCounts{
		TargetsCount: harness.I32(1),
	})

	s.K8s.Patch(t, s.K8s.Pipeline(t, "by-ref"), `{
		"spec":{
			"targetSelectors":[{"matchLabels":{"role":"leaf"}}]
		}
	}`)
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2, spine1)
	harness.WaitPipelineCounts(t, s.K8s, "by-ref", harness.PipelineCounts{
		TargetsCount: harness.I32(3),
	})
}

// TestPipe003_LabelAddPullsTargetIn checks membership follows Target labels.
func TestPipe003_LabelAddPullsTargetIn(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)
	restoreSpineRole(t)

	applyPipeline(t, "label-add", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	estab := map[string]time.Time{
		leaf1: s.GnmiGen.EstablishedAt(leaf1),
		leaf2: s.GnmiGen.EstablishedAt(leaf2),
	}

	s.K8s.Patch(t, s.K8s.Target(t, spine1), `{"metadata":{"labels":{"role":"leaf"}}}`)
	s.GnmiGen.WaitStreams(t, spine1, 1)
	harness.WaitPipelineCounts(t, s.K8s, "label-add", harness.PipelineCounts{
		TargetsCount: harness.I32(3),
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	assertEstablishedStable(t, estab, leaf1, leaf2)
}

// TestPipe004_LabelRemoveDropsTarget checks collection stops when membership ends.
func TestPipe004_LabelRemoveDropsTarget(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)
	restoreSpineRole(t)

	s.K8s.Patch(t, s.K8s.Target(t, spine1), `{"metadata":{"labels":{"role":"leaf"}}}`)

	applyPipeline(t, "label-rm", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2, spine1)
	estab := map[string]time.Time{
		leaf1: s.GnmiGen.EstablishedAt(leaf1),
		leaf2: s.GnmiGen.EstablishedAt(leaf2),
	}

	s.K8s.Patch(t, s.K8s.Target(t, spine1), `{"metadata":{"labels":{"role":"spine"}}}`)
	s.GnmiGen.WaitStreams(t, spine1, 0)
	harness.WaitPipelineCounts(t, s.K8s, "label-rm", harness.PipelineCounts{
		TargetsCount: harness.I32(2),
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	assertEstablishedStable(t, estab, leaf1, leaf2)
}

// TestPipe005_SubscriptionSelectorControlsPaths checks stream count and paths
// track bound Subscriptions.
func TestPipe005_SubscriptionSelectorControlsPaths(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "subs", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathIF)
	s.GnmiGen.WaitPathAbsent(t, leaf1, pathSys)

	s.K8s.Patch(t, s.K8s.Pipeline(t, "subs"), `{
		"spec":{
			"subscriptionSelectors":[
				{"matchLabels":{"group":"interface_metrics"}},
				{"matchLabels":{"group":"system_state"}}
			]
		}
	}`)
	s.GnmiGen.WaitStreams(t, leaf1, 2)
	s.GnmiGen.WaitStreams(t, leaf2, 2)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathIF)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathSys)
	harness.WaitPipelineCounts(t, s.K8s, "subs", harness.PipelineCounts{
		SubscriptionsCount: harness.I32(2),
	})
}

// TestPipe006_DisabledCollectsNothing checks enabled:false stops work without
// deleting bound CRs.
func TestPipe006_DisabledCollectsNothing(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "toggle", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)

	s.K8s.Patch(t, s.K8s.Pipeline(t, "toggle"), `{"spec":{"enabled":false}}`)
	s.GnmiGen.WaitStreams(t, leaf1, 0)
	s.GnmiGen.WaitStreams(t, leaf2, 0)
	harness.WaitPipelineStatusString(t, s.K8s, "toggle", "Disabled")
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount: harness.I32(0),
	})
	_ = s.K8s.Target(t, leaf1)
	_ = s.K8s.Pipeline(t, "toggle")

	s.K8s.Patch(t, s.K8s.Pipeline(t, "toggle"), `{"spec":{"enabled":true}}`)
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
}

// TestPipe007_TwoPipelinesStayIsolated checks pipelines on one cluster do not
// leak resources into each other.
func TestPipe007_TwoPipelinesStayIsolated(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "p-leaf", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	applyPipeline(t, "p-spine", map[string]any{
		"TargetRole": "spine",
		"SubGroup":   "system_state",
	})

	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2, spine1)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathIF)
	s.GnmiGen.WaitPathAbsent(t, leaf1, pathSys)
	s.GnmiGen.WaitPathPresent(t, spine1, pathSys)
	s.GnmiGen.WaitPathAbsent(t, spine1, pathIF)

	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		PipelinesCount: harness.I32(2),
		TargetsCount:   harness.I32(3),
	})

	estab := map[string]time.Time{
		leaf1: s.GnmiGen.EstablishedAt(leaf1),
		leaf2: s.GnmiGen.EstablishedAt(leaf2),
	}
	s.K8s.Delete(t, s.K8s.Pipeline(t, "p-spine"))
	s.GnmiGen.WaitStreams(t, spine1, 0)
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	assertEstablishedStable(t, estab, leaf1, leaf2)
}

// TestPipe008_DeletingPipelineStopsCollection checks deletion removes work
// from the pods without deleting bound CRs or restarting collectors.
func TestPipe008_DeletingPipelineStopsCollection(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	applyPipeline(t, "doomed", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)

	s.K8s.Delete(t, s.K8s.Pipeline(t, "doomed"))
	s.GnmiGen.WaitStreams(t, leaf1, 0)
	s.GnmiGen.WaitStreams(t, leaf2, 0)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		PipelinesCount:     harness.I32(0),
		TargetsCount:       harness.I32(0),
		SubscriptionsCount: harness.I32(0),
		OutputsCount:       harness.I32(0),
	})
	harness.WaitClusterReady(t, s.K8s, cluster)

	_ = s.K8s.Target(t, leaf1)
	sub := &gnmicv1alpha1.Subscription{}
	if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: "if-counters"}, sub); err != nil {
		t.Fatalf("subscription should still exist: %v", err)
	}
	out := &gnmicv1alpha1.Output{}
	if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: "sink"}, out); err != nil {
		t.Fatalf("output should still exist: %v", err)
	}
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

// TestPipe009_OverlappingPipelinesCollectOncePerCluster checks two pipelines
// selecting the same targets do not double-connect; stream count tracks the
// union of subscriptions.
func TestPipe009_OverlappingPipelinesCollectOncePerCluster(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "ov-if", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	applyPipeline(t, "ov-sys", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "system_state",
	})

	// One stream per Subscription, not one per Pipeline.
	s.GnmiGen.AssertCollectedOnce(t, 2, leaf1, leaf2)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathIF)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathSys)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount: harness.I32(2),
	})
	s.GnmiGen.ConsistentlyCollectedOnce(t, 15*time.Second, 2, leaf1, leaf2)
}

// assertUnresolved checks that a Pipeline reports the refs that did not resolve.
func assertUnresolved(t *testing.T, name string, wantRefs ...string) {
	t.Helper()
	harness.WaitPipelineCondition(t, s.K8s, name, harness.CondResourcesResolved, metav1.ConditionFalse)
	harness.WaitPipelineStatusString(t, s.K8s, name, "Error")

	p := s.K8s.Pipeline(t, name)
	cond := meta.FindStatusCondition(p.Status.Conditions, harness.CondResourcesResolved)
	if cond == nil {
		t.Fatalf("pipeline %s has no %s condition", name, harness.CondResourcesResolved)
	}
	if cond.Reason != "UnresolvedReferences" {
		t.Errorf("reason = %q, want UnresolvedReferences", cond.Reason)
	}
	for _, ref := range wantRefs {
		if !strings.Contains(cond.Message, ref) {
			t.Errorf("message %q does not name %q", cond.Message, ref)
		}
	}
	if ready := meta.FindStatusCondition(p.Status.Conditions, harness.CondReady); ready == nil ||
		ready.Status != metav1.ConditionFalse {
		t.Errorf("Ready = %+v, want False", ready)
	}
}

// assertNothingCollected holds the wire quiet, because a pipeline that is skipped
// and then applied a moment later looks identical to one that was never applied
// if you only sample once.
func assertNothingCollected(t *testing.T, d time.Duration) {
	t.Helper()
	harness.Consistently(t, d, time.Second, "no target is collected", func() (bool, string) {
		for _, name := range []string{leaf1, leaf2, spine1} {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
}

// A ref that names something which does not exist leaves the whole Pipeline out
// of the plan. spine1 resolves perfectly well here and still must not be
// collected: applying the pipeline without what it asked for would silently
// collect a different configuration than the one that was written.
func TestPipe010_UnresolvedRefSkipsWholePipeline(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "dangling", map[string]any{
		"TargetRefs": []string{spine1, "no-such-target"},
		"SubGroup":   "interface_metrics",
	})

	assertUnresolved(t, "dangling", "target/no-such-target")
	assertNothingCollected(t, 10*time.Second)

	// The counts stay populated even though the pipeline was skipped: spine1 did
	// resolve, and keeping that visible is what makes a partial resolution
	// diagnosable from the CR alone.
	harness.WaitPipelineCounts(t, s.K8s, "dangling", harness.PipelineCounts{
		TargetsCount: harness.I32(1),
	})

	// Dropping the bad ref resolves it, with no other nudge.
	s.K8s.Patch(t, s.K8s.Pipeline(t, "dangling"), `{"spec":{"targetRefs":["`+spine1+`"]}}`)
	harness.WaitPipelineCondition(t, s.K8s, "dangling", harness.CondResourcesResolved, metav1.ConditionTrue)
	s.GnmiGen.WaitStreams(t, spine1, 1)
}

// Breaking the only Pipeline on a cluster must not tear down what the collectors
// are already running. The plan goes empty when every pipeline is skipped, and an
// empty plan is applied deliberately elsewhere -- it is how collection stops after
// the last Pipeline is deleted -- so without a guard this drains the cluster over
// a name that usually resolves a moment later.
func TestPipe011_UnresolvedRefDoesNotDrainRunningCollectors(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "drainguard", map[string]any{
		"TargetRefs": []string{leaf1},
		"SubGroup":   "interface_metrics",
	})
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	established := s.GnmiGen.EstablishedAt(leaf1)

	s.K8s.Patch(t, s.K8s.Pipeline(t, "drainguard"),
		`{"spec":{"targetRefs":["`+leaf1+`","no-such-target"]}}`)
	assertUnresolved(t, "drainguard", "target/no-such-target")

	harness.Consistently(t, 15*time.Second, time.Second, "collection survives the broken pipeline", func() (bool, string) {
		if n := s.GnmiGen.StreamCount(leaf1); n != 1 {
			return false, fmt.Sprintf("leaf1 has %d streams, want 1", n)
		}
		return true, ""
	})
	if got := s.GnmiGen.EstablishedAt(leaf1); !got.Equal(established) {
		t.Errorf("stream was re-established at %v (was %v); the collector was reconfigured", got, established)
	}

	// Restore, so the Cluster is not left reporting ConfigApplied=False for the next
	// test, and so recovery from the suppressed state is covered too.
	s.K8s.Patch(t, s.K8s.Pipeline(t, "drainguard"), `{"spec":{"targetRefs":["`+leaf1+`"]}}`)
	harness.WaitPipelineCondition(t, s.K8s, "drainguard", harness.CondResourcesResolved, metav1.ConditionTrue)
	harness.WaitClusterCondition(t, s.K8s, cluster, harness.CondConfigApplied, metav1.ConditionTrue, harness.Medium)
}

// One bad ref must cost exactly one pipeline.
func TestPipe012_BrokenPipelineLeavesHealthyOneAlone(t *testing.T) {
	waitClusterReady(t)
	waitIdle(t)

	applyPipeline(t, "healthy", map[string]any{
		"TargetRole": "leaf",
		"SubGroup":   "interface_metrics",
	})
	applyPipeline(t, "broken", map[string]any{
		"TargetRefs": []string{spine1, "no-such-target"},
		"SubGroup":   "system_state",
	})

	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	s.GnmiGen.WaitStreams(t, spine1, 0)

	assertUnresolved(t, "broken", "target/no-such-target")
	harness.WaitPipelineCondition(t, s.K8s, "healthy", harness.CondResourcesResolved, metav1.ConditionTrue)
	harness.WaitPipelineCondition(t, s.K8s, "healthy", harness.CondReady, metav1.ConditionTrue)

	// Only the healthy pipeline's targets reached the plan.
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount: harness.I32(2),
	})

	// And the healthy one keeps collecting while the broken one stays broken.
	harness.Consistently(t, 10*time.Second, time.Second, "healthy pipeline keeps collecting", func() (bool, string) {
		if n := s.GnmiGen.StreamCount(leaf1); n != 1 {
			return false, fmt.Sprintf("leaf1 has %d streams", n)
		}
		if n := s.GnmiGen.StreamCount(spine1); n != 0 {
			return false, fmt.Sprintf("spine1 has %d streams", n)
		}
		return true, ""
	})

	// Deleting the broken pipeline is enough to clear the condition; the healthy one
	// is undisturbed throughout.
	s.K8s.Delete(t, s.K8s.Pipeline(t, "broken"))
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		PipelinesCount: harness.I32(1),
		TargetsCount:   harness.I32(2),
	})
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
}
