//go:build integration

// Package targets covers Target / TargetProfile lifecycle, credential and
// address failures, reboot-driven status, and multi-cluster status reporting.
package targets

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster  = "c1"
	pipeline = "collect"
	output   = "prom"
	pathIF   = "/interface/statistics/in-octets"

	leaf1  = "leaf1"
	leaf2  = "leaf2"
	spine1 = "spine1"
	leaf3  = "leaf3" // simulator only until a test points a CR at :57403

	portLeaf1 = 57400
	portLeaf2 = 57401
	portSpine = 57402
	portLeaf3 = 57403
	portGhost = 57999
)

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "003-targets",
		RequireTargets: []string{leaf1, leaf2, spine1, leaf3},
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

func waitClusterReady(t *testing.T) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, cluster)
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

// waitIdle deletes leftover Targets (and optional extra clusters/pipelines)
// from prior tests, then waits until the wire is quiet.
func waitIdle(t *testing.T) {
	t.Helper()
	var targets gnmicv1alpha1.TargetList
	if err := s.K8s.Client.List(s.Ctx, &targets, client.InNamespace(s.Namespace)); err == nil {
		for i := range targets.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &targets.Items[i])
		}
	}
	var pipelines gnmicv1alpha1.PipelineList
	if err := s.K8s.Client.List(s.Ctx, &pipelines, client.InNamespace(s.Namespace)); err == nil {
		for i := range pipelines.Items {
			if pipelines.Items[i].Name == pipeline {
				continue
			}
			_ = s.K8s.Client.Delete(s.Ctx, &pipelines.Items[i])
		}
	}
	var clusters gnmicv1alpha1.ClusterList
	if err := s.K8s.Client.List(s.Ctx, &clusters, client.InNamespace(s.Namespace)); err == nil {
		for i := range clusters.Items {
			if clusters.Items[i].Name == cluster {
				continue
			}
			_ = s.K8s.Client.Delete(s.Ctx, &clusters.Items[i])
		}
	}

	harness.Wait(t, harness.Medium, "extra clusters gone", func() (bool, string) {
		var left gnmicv1alpha1.ClusterList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		n := 0
		for _, c := range left.Items {
			if c.Name != cluster {
				n++
			}
		}
		return n == 0, fmt.Sprintf("%d extra cluster(s)", n)
	})
	harness.Wait(t, harness.Medium, "all Target CRs gone", func() (bool, string) {
		var left gnmicv1alpha1.TargetList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d remain", len(left.Items))
	})
	harness.Wait(t, harness.Medium, "all simulated targets idle", func() (bool, string) {
		for _, name := range []string{leaf1, leaf2, spine1, leaf3} {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
	waitClusterReady(t)
}

func applyTarget(t *testing.T, name string, port int, profile, role string) {
	t.Helper()
	if profile == "" {
		profile = "default"
	}
	s.K8s.ApplyFile(t, "fixtures/target.yaml", map[string]any{
		"Name":    name,
		"Port":    port,
		"Profile": profile,
		"Role":    role,
	})
}

func establishedSnapshot(names ...string) map[string]time.Time {
	out := make(map[string]time.Time, len(names))
	for _, n := range names {
		out[n] = s.GnmiGen.EstablishedAt(n)
	}
	return out
}

func assertEstablishedStable(t *testing.T, before map[string]time.Time, names ...string) {
	t.Helper()
	for _, name := range names {
		was := before[name]
		now := s.GnmiGen.EstablishedAt(name)
		if was.IsZero() || now.IsZero() {
			t.Errorf("%s missing established_at before=%v after=%v", name, was, now)
			continue
		}
		if now.Sub(was).Abs() > time.Second {
			t.Errorf("%s re-established: %s -> %s", name, was, now)
		}
	}
}

func TestTarget001_CreatingStartsOneStream(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")

	s.GnmiGen.WaitStreams(t, leaf1, 1)
	s.GnmiGen.WaitPathPresent(t, leaf1, pathIF)
	s.GnmiGen.WaitStreams(t, leaf2, 0)
	s.GnmiGen.WaitStreams(t, spine1, 0)
	s.GnmiGen.WaitStreams(t, leaf3, 0)

	harness.WaitTargetState(t, s.K8s, leaf1, "READY")
	tgt := s.K8s.Target(t, leaf1)
	if tgt.Status.Clusters != 1 {
		t.Errorf("clusters=%d want 1", tgt.Status.Clusters)
	}
	if got := tgt.Status.ClusterStates[cluster].Pod; got != harness.PodName(cluster, 0) {
		t.Errorf("pod=%q want %s", got, harness.PodName(cluster, 0))
	}
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, leaf1)
}

func TestTarget002_ProfileSuppliesConnectionParams(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")

	// gnmi-gen does not report Subscribe encoding, and an encoding-only target
	// update does not tear down the stream, so we cannot assert a new
	// established_at. Prove the profile edit is live by staying collected
	// without a collector restart.
	restartsBefore := s.K8s.RestartCounts(t, cluster)
	prof := s.K8s.TargetProfile(t, "default")
	s.K8s.Patch(t, prof, `{"spec":{"encoding":"PROTO"}}`)
	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.TargetProfile(t, "default"), `{"spec":{"encoding":"JSON_IETF"}}`)
	})

	harness.WaitTargetState(t, s.K8s, leaf1, "READY")
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, leaf1)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestTarget003_WrongCredentialsFailAndRecover(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	s.GnmiGen.WaitStreams(t, leaf1, 1)

	badSecret := &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: "bad-credentials", Namespace: s.Namespace},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"username": "gnmic", "password": "wrong"},
	}
	if err := s.K8s.ApplyNoCleanup(badSecret); err != nil {
		t.Fatalf("applying bad secret: %v", err)
	}
	badProfile := `
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: bad-auth
spec:
  credentialsRef: bad-credentials
  encoding: JSON_IETF
  timeout: 5s
  tls: {}
  retryTimer: 2s
`
	if _, err := s.K8s.ApplyYAMLNoCleanup(badProfile, nil); err != nil {
		t.Fatalf("applying bad profile: %v", err)
	}
	applyTarget(t, leaf2, portLeaf2, "bad-auth", "leaf")

	harness.Wait(t, harness.Medium, "leaf2 failed auth", func() (bool, string) {
		tgt, err := s.K8s.TargetQuiet(leaf2)
		if err != nil {
			return false, err.Error()
		}
		if tgt.Status.State == "READY" {
			return false, "still READY"
		}
		cs := tgt.Status.ClusterStates[cluster]
		if cs.State != "failed" {
			return false, fmt.Sprintf("state=%q connection=%q", cs.State, cs.ConnectionState)
		}
		if cs.FailedReason == "" {
			return false, "failedReason empty"
		}
		return true, ""
	})
	s.GnmiGen.WaitStreams(t, leaf2, 0)
	// Adding leaf2 re-applied the whole pod's config, briefly reloading
	// leaf1's stream too; settle before proving it holds, or the window can
	// start mid-reconnect and flake on a transient 0.
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	s.GnmiGen.ConsistentlyCollectedOnce(t, 5*time.Second, 1, leaf1)

	// Replace the Secret via YAML apply (avoids managedFields on a reused object),
	// then bump the profile so the Cluster reconciler is guaranteed to rebuild
	// the plan with the new password even if the Secret watch is slow.
	fixedSecret := `
apiVersion: v1
kind: Secret
metadata:
  name: bad-credentials
type: Opaque
stringData:
  username: gnmic
  password: gnmic
`
	if _, err := s.K8s.ApplyYAMLNoCleanup(fixedSecret, nil); err != nil {
		t.Fatalf("fixing credentials: %v", err)
	}
	s.K8s.Patch(t, s.K8s.TargetProfile(t, "bad-auth"), `{"spec":{"timeout":"6s"}}`)

	harness.WaitTargetState(t, s.K8s, leaf2, "READY")
	s.GnmiGen.WaitStreams(t, leaf2, 1)
}

func TestTarget004_AddressChangeMovesStream(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")
	s.K8s.WaitClusterPrometheusSources(t, cluster, pipeline, output, []string{leaf1}, harness.Medium)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	host := harness.GnmiGenHost(s.Namespace)
	s.K8s.Patch(t, s.K8s.Target(t, leaf1),
		fmt.Sprintf(`{"spec":{"address":%q}}`, fmt.Sprintf("%s:%d", host, portLeaf3)))

	s.GnmiGen.WaitStreams(t, leaf3, 1)
	s.GnmiGen.WaitStreams(t, leaf1, 0)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
	// Source label follows the Target CR name, not the simulator listen name.
	s.K8s.WaitClusterPrometheusSources(t, cluster, pipeline, output, []string{leaf1}, harness.Medium)
}

func TestTarget005_UnreachableAddressDegradesCleanly(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")

	applyTarget(t, "ghost", portGhost, "default", "")
	harness.Wait(t, harness.Medium, "ghost not READY", func() (bool, string) {
		tgt, err := s.K8s.TargetQuiet("ghost")
		if err != nil {
			return false, err.Error()
		}
		if tgt.Status.State == "READY" {
			return false, "ghost reached READY"
		}
		cs := tgt.Status.ClusterStates[cluster]
		if cs.State == "" && cs.ConnectionState == "" {
			return false, "no cluster state yet"
		}
		return cs.State != "running" || cs.ConnectionState != "READY",
			fmt.Sprintf("state=%q connection=%q", cs.State, cs.ConnectionState)
	})

	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount: harness.I32(2),
	})
	restartsBefore := s.K8s.RestartCounts(t, cluster)
	// Adding ghost re-applies the pod plan and can briefly flap leaf1's CR
	// connectionState; the wire invariant is what isolation requires.
	s.GnmiGen.ConsistentlyCollectedOnce(t, 20*time.Second, 1, leaf1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
	harness.AssertNoPanics(t, s.K8s.Logs(t, harness.PodName(cluster, 0), 2*time.Minute))
}

func TestTarget006_DeviceRebootDegradesAndRecovers(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")
	beforeEstab := s.GnmiGen.EstablishedAt(leaf1)
	simBefore, err := s.GnmiGen.Target(leaf1)
	if err != nil {
		t.Fatalf("gnmi-gen target: %v", err)
	}

	if err := s.GnmiGen.Reboot(20*time.Second, leaf1); err != nil {
		t.Fatalf("reboot: %v", err)
	}

	harness.Wait(t, harness.Medium, "leaf1 leaves READY during reboot", func() (bool, string) {
		tgt, err := s.K8s.TargetQuiet(leaf1)
		if err != nil {
			return false, err.Error()
		}
		return tgt.Status.State != "READY", "state=" + tgt.Status.State
	})
	harness.Wait(t, harness.Medium, "sim rebooting with zero streams", func() (bool, string) {
		sim, err := s.GnmiGen.Target(leaf1)
		if err != nil {
			return false, err.Error()
		}
		n := s.GnmiGen.StreamCount(leaf1)
		return (sim.Status == "rebooting" || n == 0), fmt.Sprintf("status=%s streams=%d", sim.Status, n)
	})

	s.GnmiGen.WaitTargetsUp(t, leaf1)
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")

	simAfter, err := s.GnmiGen.Target(leaf1)
	if err != nil {
		t.Fatalf("gnmi-gen target after: %v", err)
	}
	if simAfter.RebootCount <= simBefore.RebootCount {
		t.Errorf("reboot_count did not increment: %d -> %d", simBefore.RebootCount, simAfter.RebootCount)
	}
	afterEstab := s.GnmiGen.EstablishedAt(leaf1)
	if afterEstab.IsZero() || afterEstab.Sub(beforeEstab).Abs() <= time.Second {
		t.Errorf("expected fresh established_at; before=%v after=%v", beforeEstab, afterEstab)
	}
	if n := s.GnmiGen.StreamCount(leaf1); n != 1 {
		t.Fatalf("want exactly 1 stream after reboot, got %d", n)
	}
	s.GnmiGen.WaitNotificationsAdvance(t, leaf1)
}

func TestTarget007_RebootOneDoesNotDisturbOthers(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	applyTarget(t, leaf2, portLeaf2, "default", "leaf")
	applyTarget(t, spine1, portSpine, "default", "spine")
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2, spine1)

	estab := establishedSnapshot(leaf1, leaf2)
	notifBefore := map[string]int{
		leaf1: s.GnmiGen.Notifications(leaf1),
		leaf2: s.GnmiGen.Notifications(leaf2),
	}

	if err := s.GnmiGen.Reboot(20*time.Second, spine1); err != nil {
		t.Fatalf("reboot: %v", err)
	}

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if n := s.GnmiGen.StreamCount(leaf1); n != 1 {
			t.Fatalf("leaf1 streams=%d during spine reboot", n)
		}
		if n := s.GnmiGen.StreamCount(leaf2); n != 1 {
			t.Fatalf("leaf2 streams=%d during spine reboot", n)
		}
		time.Sleep(time.Second)
	}
	assertEstablishedStable(t, estab, leaf1, leaf2)
	if s.GnmiGen.Notifications(leaf1) <= notifBefore[leaf1] {
		t.Error("leaf1 notifications did not advance during spine reboot")
	}
	if s.GnmiGen.Notifications(leaf2) <= notifBefore[leaf2] {
		t.Error("leaf2 notifications did not advance during spine reboot")
	}

	s.GnmiGen.WaitTargetsUp(t, spine1)
	s.GnmiGen.WaitStreams(t, spine1, 1)
	harness.WaitTargetState(t, s.K8s, spine1, "READY")
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2, spine1)
}

func TestTarget008_DeletingTargetStopsCollection(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	applyTarget(t, leaf2, portLeaf2, "default", "leaf")
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	s.K8s.WaitClusterPrometheusSources(t, cluster, pipeline, output, []string{leaf1, leaf2}, harness.Medium)
	estab := establishedSnapshot(leaf2)

	s.K8s.Delete(t, s.K8s.Target(t, leaf1))
	s.GnmiGen.WaitStreams(t, leaf1, 0)
	s.K8s.WaitAbsent(t, leaf1, &gnmicv1alpha1.Target{})
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount: harness.I32(1),
	})
	s.GnmiGen.WaitStreams(t, leaf2, 1)
	assertEstablishedStable(t, estab, leaf2)

	s.K8s.WaitClusterPrometheusSources(t, cluster, pipeline, output, []string{leaf2}, harness.Medium)
	harness.Wait(t, harness.Medium, "prometheus dropped leaf1", func() (bool, string) {
		body := s.K8s.ScrapeClusterPrometheus(t, cluster, pipeline, output)
		sources := harness.LabelValues(body, "source")
		for _, src := range sources {
			if src == leaf1 || strings.HasSuffix(src, "/"+leaf1) {
				return false, fmt.Sprintf("leaf1 still in sources=%v", sources)
			}
		}
		return true, fmt.Sprintf("sources=%v", sources)
	})
}

func TestTarget009_TwoClustersReportBoth(t *testing.T) {
	waitIdle(t)
	applyTarget(t, leaf1, portLeaf1, "default", "leaf")
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.WaitTargetState(t, s.K8s, leaf1, "READY")

	const c2 = "c2"
	s.K8s.ApplyFile(t, "fixtures/cluster.yaml", map[string]any{
		"Name":     c2,
		"Replicas": 1,
	})
	s.K8s.ApplyFile(t, "fixtures/pipeline.yaml", map[string]any{
		"Name":    "collect-c2",
		"Cluster": c2,
	})
	s.K8s.WaitReadyPods(t, c2, 1, harness.Long)

	harness.Wait(t, harness.Medium, "leaf1 collected by two clusters", func() (bool, string) {
		tgt, err := s.K8s.TargetQuiet(leaf1)
		if err != nil {
			return false, err.Error()
		}
		if tgt.Status.Clusters != 2 {
			return false, fmt.Sprintf("clusters=%d", tgt.Status.Clusters)
		}
		if tgt.Status.ClusterStates[cluster].Pod == "" || tgt.Status.ClusterStates[c2].Pod == "" {
			return false, fmt.Sprintf("states=%v", tgt.Status.ClusterStates)
		}
		if n := s.GnmiGen.StreamCount(leaf1); n != 2 {
			return false, fmt.Sprintf("streams=%d", n)
		}
		return true, ""
	})

	s.K8s.Delete(t, s.K8s.Cluster(t, c2))
	s.K8s.WaitAbsent(t, c2, &gnmicv1alpha1.Cluster{})
	s.GnmiGen.WaitStreams(t, leaf1, 1)
	harness.Wait(t, harness.Medium, "clusters back to 1", func() (bool, string) {
		tgt, err := s.K8s.TargetQuiet(leaf1)
		if err != nil {
			return false, err.Error()
		}
		return tgt.Status.Clusters == 1, fmt.Sprintf("clusters=%d", tgt.Status.Clusters)
	})
}
