//go:build integration

// Package processors covers Processor binding and ordering on Pipeline outputs.
package processors

import (
	"fmt"
	"os"
	"testing"
	"time"

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
	outFile = "/tmp/out.jsonl"
)

var allTargets = []string{leaf1, leaf2, spine1}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "006-processors",
		RequireTargets: allTargets,
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

func waitClusterReady(t *testing.T) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, cluster)
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

func waitIdle(t *testing.T) {
	t.Helper()
	var pipes gnmicv1alpha1.PipelineList
	if err := s.K8s.Client.List(s.Ctx, &pipes, client.InNamespace(s.Namespace)); err == nil {
		for i := range pipes.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &pipes.Items[i])
		}
	}
	var procs gnmicv1alpha1.ProcessorList
	if err := s.K8s.Client.List(s.Ctx, &procs, client.InNamespace(s.Namespace)); err == nil {
		for i := range procs.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &procs.Items[i])
		}
	}
	harness.Wait(t, harness.Medium, "pipelines gone", func() (bool, string) {
		var left gnmicv1alpha1.PipelineList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d remain", len(left.Items))
	})
	harness.Wait(t, harness.Medium, "processors gone", func() (bool, string) {
		var left gnmicv1alpha1.ProcessorList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d remain", len(left.Items))
	})
	harness.Wait(t, harness.Medium, "targets idle", func() (bool, string) {
		for _, name := range allTargets {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
	// Collector may still be Pending right after baseline; wait before exec.
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
	pod := s.K8s.FirstCollectorPod(t, cluster)
	_ = s.K8s.Exec(t, pod, harness.CollectorContainer, "sh", "-c", fmt.Sprintf("rm -f %q", outFile))
}

func applyAddTag(t *testing.T, name, chain, key, value string) {
	t.Helper()
	b, err := os.ReadFile("fixtures/processor-add-tag.yaml")
	if err != nil {
		t.Fatalf("reading add-tag fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), map[string]any{
		"Name":     name,
		"Chain":    chain,
		"TagKey":   key,
		"TagValue": value,
	}); err != nil {
		t.Fatalf("applying processor %s: %v", name, err)
	}
}

func applyJQ(t *testing.T, name, chain string) {
	t.Helper()
	b, err := os.ReadFile("fixtures/processor-jq.yaml")
	if err != nil {
		t.Fatalf("reading jq fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), map[string]any{
		"Name":  name,
		"Chain": chain,
	}); err != nil {
		t.Fatalf("applying processor %s: %v", name, err)
	}
}

func applyPipeline(t *testing.T, name, chain string, refs []string) {
	t.Helper()
	if refs == nil {
		refs = []string{}
	}
	b, err := os.ReadFile("fixtures/pipeline.yaml")
	if err != nil {
		t.Fatalf("reading pipeline fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), map[string]any{
		"Name":           name,
		"ProcessorChain": chain,
		"ProcessorRefs":  refs,
	}); err != nil {
		t.Fatalf("applying pipeline %s: %v", name, err)
	}
}

func startBound(t *testing.T, chain string) {
	t.Helper()
	applyPipeline(t, "p1", chain, nil)
	waitClusterReady(t)
	harness.WaitConfigApplied(t, s.K8s, cluster)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

func TestProc001_SelectedProcessorApplied(t *testing.T) {
	waitIdle(t)
	applyAddTag(t, "add-env", "a", "env", "test")
	startBound(t, "a")

	events := s.K8s.WaitEventsHaveTag(t, cluster, outFile, "env", "test")
	if !harness.EventsHaveSource(events, leaf1) {
		t.Errorf("processor replaced rather than added tags; events=%+v", events[:min(3, len(events))])
	}
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		PipelinesCount:     harness.I32(1),
		TargetsCount:       harness.I32(3),
		SubscriptionsCount: harness.I32(1),
		OutputsCount:       harness.I32(1),
	})
}

func TestProc002_UnboundProcessorNotApplied(t *testing.T) {
	waitIdle(t)
	applyAddTag(t, "add-a", "a", "env", "alpha")
	applyAddTag(t, "add-b", "b", "env", "beta")
	startBound(t, "a")

	s.K8s.WaitEventsHaveTag(t, cluster, outFile, "env", "alpha")
	s.K8s.ConsistentlyEventsLackTag(t, cluster, outFile, "env", "beta", 15*time.Second)
}

func TestProc003_ProcessorOrderDeterministic(t *testing.T) {
	waitIdle(t)
	// Selector order is by name: add-stage then rewrite-stage.
	// add sets stage=first; jq rewrites to second only if stage is already set.
	// Documented order therefore yields stage=second.
	applyAddTag(t, "add-stage", "ord", "stage", "first")
	applyJQ(t, "rewrite-stage", "ord")

	var got string
	for i := 0; i < 3; i++ {
		if i > 0 {
			pipe := s.K8s.Pipeline(t, "p1")
			_ = s.K8s.Client.Delete(s.Ctx, pipe)
			s.K8s.WaitAbsent(t, "p1", &gnmicv1alpha1.Pipeline{})
			pod := s.K8s.FirstCollectorPod(t, cluster)
			_ = s.K8s.Exec(t, pod, harness.CollectorContainer, "sh", "-c", fmt.Sprintf("rm -f %q", outFile))
			harness.Wait(t, harness.Medium, "idle between recreates", func() (bool, string) {
				for _, name := range allTargets {
					if n := s.GnmiGen.StreamCount(name); n != 0 {
						return false, fmt.Sprintf("%s=%d", name, n)
					}
				}
				return true, ""
			})
		}
		startBound(t, "ord")
		events := s.K8s.WaitEventsHaveTag(t, cluster, outFile, "stage", "second")
		if harness.EventsHaveTag(events, "stage", "first") && !harness.EventsHaveTag(events, "stage", "second") {
			t.Fatalf("run %d: got stage=first (jq-before-add), want stage=second", i+1)
		}
		if !harness.EventsHaveTag(events, "stage", "second") {
			t.Fatalf("run %d: missing stage=second", i+1)
		}
		if got == "" {
			got = "second"
		} else if got != "second" {
			t.Fatalf("non-deterministic order across recreates")
		}
	}
}

func TestProc004_AddingProcessorLive(t *testing.T) {
	waitIdle(t)
	applyAddTag(t, "add-a", "a", "env", "alpha")
	applyAddTag(t, "add-b", "", "zone", "z1") // unbound until labelled
	startBound(t, "a")
	s.K8s.WaitEventsHaveTag(t, cluster, outFile, "env", "alpha")
	before := s.K8s.ReadCollectorFile(t, cluster, outFile)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	proc := &gnmicv1alpha1.Processor{}
	s.K8s.WaitExists(t, "add-b", proc)
	s.K8s.Patch(t, proc, `{"metadata":{"labels":{"chain":"a"}}}`)

	harness.WaitConfigApplied(t, s.K8s, cluster)
	s.K8s.WaitEventsHaveTag(t, cluster, outFile, "zone", "z1")
	after := s.K8s.ReadCollectorFile(t, cluster, outFile)
	if !harness.EventsHaveTag(harness.ParseEvents(before), "env", "alpha") {
		t.Fatal("expected earlier lines to retain env=alpha")
	}
	if harness.EventsHaveTag(harness.ParseEvents(before), "zone", "z1") {
		t.Fatal("zone tag appeared before the live edit")
	}
	if !harness.EventsHaveTag(harness.ParseEvents(after), "zone", "z1") {
		t.Fatal("zone tag missing after live edit")
	}
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestProc005_RemovingProcessorStopsTransform(t *testing.T) {
	waitIdle(t)
	applyAddTag(t, "add-a", "a", "env", "alpha")
	applyAddTag(t, "add-b", "a", "zone", "z1")
	startBound(t, "a")
	s.K8s.WaitEventsHaveTag(t, cluster, outFile, "env", "alpha")
	s.K8s.WaitEventsHaveTag(t, cluster, outFile, "zone", "z1")

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	mark := len(s.K8s.ReadCollectorFile(t, cluster, outFile))

	proc := &gnmicv1alpha1.Processor{}
	s.K8s.WaitExists(t, "add-b", proc)
	s.K8s.Patch(t, proc, `{"metadata":{"labels":{"chain":null}}}`)
	harness.WaitConfigApplied(t, s.K8s, cluster)

	// Wait for new events that have env but not zone.
	harness.Wait(t, harness.Medium, "new events without zone", func() (bool, string) {
		body := s.K8s.ReadCollectorFile(t, cluster, outFile)
		if len(body) <= mark {
			return false, "no new bytes"
		}
		tail := harness.ParseEvents(body[mark:])
		if len(tail) == 0 {
			return false, "no new events"
		}
		if harness.EventsHaveTag(tail, "zone", "z1") {
			return false, "zone still present in new events"
		}
		return harness.EventsHaveTag(tail, "env", "alpha"), "env missing"
	})

	proc = &gnmicv1alpha1.Processor{}
	s.K8s.WaitExists(t, "add-b", proc)
	if err := s.K8s.Client.Delete(s.Ctx, proc); err != nil {
		t.Fatalf("deleting processor: %v", err)
	}
	s.K8s.WaitAbsent(t, "add-b", &gnmicv1alpha1.Processor{})

	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, allTargets...)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}
