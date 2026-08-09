//go:build integration

// Package spike is the Phase 0 vertical slice for the integration suite.
//
// It exists to prove the assumptions the rest of the suites are built on,
// before thirteen suites depend on them:
//
//   - gnmi-gen runs in-cluster from a ConfigMap and answers its REST API
//     through a port-forward;
//   - a headless Service lets collector pods dial any simulated target port
//     without that port being enumerated on the Service;
//   - the operator reconciles a Cluster, Pipeline, Target and Subscription into
//     collectors that actually establish gNMI streams;
//   - a suite namespace tears down cleanly, repeatably.
//
// It is not a feature test. Once 001-cluster and 003-targets exist, everything
// here is covered better elsewhere and this package can be deleted.
package spike

import (
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	clusterName = "spike"
	leaf1       = "leaf1"
	leaf2       = "leaf2"
)

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "000-spike",
		RequireTargets: []string{leaf1, leaf2},
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

// TestSpike000_SimulatorIsUp checks the device side alone, so a failure in the
// simulator is not mistaken for a failure in the operator.
func TestSpike000_SimulatorIsUp(t *testing.T) {
	targets, err := s.GnmiGen.Targets()
	if err != nil {
		t.Fatalf("gnmi-gen API unreachable: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("want 2 simulated targets, got %d: %v", len(targets), targets)
	}
	for _, name := range []string{leaf1, leaf2} {
		tgt, ok := targets[name]
		if !ok {
			t.Fatalf("simulated target %s missing", name)
		}
		if tgt.Status != "up" {
			t.Errorf("simulated target %s is %s, want up", name, tgt.Status)
		}
		if tgt.PathCount == 0 {
			t.Errorf("simulated target %s serves no paths", name)
		}
	}
}

// TestSpike001_OperatorBuildsClusterObjects checks the operator turned the
// Cluster CR into the objects it owns.
func TestSpike001_OperatorBuildsClusterObjects(t *testing.T) {
	k := s.K8s

	sts := &appsv1.StatefulSet{}
	k.WaitExists(t, harness.StatefulSetName(clusterName), sts)

	if got := *sts.Spec.Replicas; got != 1 {
		t.Errorf("StatefulSet replicas: want 1, got %d", got)
	}
	if got := sts.Spec.ServiceName; got != harness.HeadlessServiceName(clusterName) {
		t.Errorf("StatefulSet serviceName: want %s, got %s", harness.HeadlessServiceName(clusterName), got)
	}

	// The headless Service and the rendered config both have to exist before
	// any collector can start.
	k.Service(t, harness.HeadlessServiceName(clusterName))
	cm := k.ConfigMap(t, harness.ConfigMapName(clusterName))
	if len(cm.Data) == 0 {
		t.Error("cluster ConfigMap is empty")
	}
}

// TestSpike002_ClusterBecomesReady checks the Cluster reports Ready and that
// its status counters agree with what was applied.
func TestSpike002_ClusterBecomesReady(t *testing.T) {
	harness.WaitClusterReady(t, s.K8s, clusterName)
	harness.WaitClusterCounts(t, s.K8s, clusterName, harness.ClusterCounts{
		ReadyReplicas:      harness.I32(1),
		PipelinesCount:     harness.I32(1),
		TargetsCount:       harness.I32(2),
		SubscriptionsCount: harness.I32(1),
		OutputsCount:       harness.I32(1),
		UnassignedTargets:  harness.I32(0),
	})
}

// TestSpike003_TargetsCollectedExactlyOnce is the load-bearing assertion. It
// proves collectors resolved the headless Service, dialed a port that is not
// declared on it, authenticated, and opened exactly one stream each.
func TestSpike003_TargetsCollectedExactlyOnce(t *testing.T) {
	// One subscription in the baseline, so one collector means one stream.
	s.GnmiGen.AssertCollectedOnce(t, 1, leaf1, leaf2)
	// Held over a window, because a single sample cannot distinguish a settled
	// placement from one that is still flapping.
	s.GnmiGen.ConsistentlyCollectedOnce(t, 15*time.Second, 1, leaf1, leaf2)
}

// TestSpike004_SubscriptionMatchesTheCR checks the wire-level subscription
// reflects the Subscription CR rather than a default.
func TestSpike004_SubscriptionMatchesTheCR(t *testing.T) {
	subs, err := s.GnmiGen.Subscriptions(leaf1)
	if err != nil {
		t.Fatalf("reading subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("want 1 stream on %s, got %d", leaf1, len(subs))
	}
	sub := subs[0]
	if sub.Mode != "STREAM" {
		t.Errorf("subscription mode: want STREAM, got %s", sub.Mode)
	}
	if len(sub.Entries) != 1 {
		t.Fatalf("want 1 subscription entry, got %d", len(sub.Entries))
	}
	entry := sub.Entries[0]
	if entry.Mode != "SAMPLE" {
		t.Errorf("entry mode: want SAMPLE, got %s", entry.Mode)
	}
	if entry.SampleInterval != "1s" {
		t.Errorf("sample interval: want 1s, got %s", entry.SampleInterval)
	}
	s.GnmiGen.WaitPathPresent(t, leaf1, "/interface/statistics/in-octets")
}

// TestSpike005_DataFlows checks streams are carrying notifications, not merely
// open. An established-but-silent stream is a real failure mode.
func TestSpike005_DataFlows(t *testing.T) {
	s.GnmiGen.WaitNotificationsAdvance(t, leaf1)
	s.GnmiGen.WaitNotificationsAdvance(t, leaf2)
}

// TestSpike006_TargetStatusReportsPlacement checks the operator writes back
// what the collectors report, which is how every later suite reads placement.
func TestSpike006_TargetStatusReportsPlacement(t *testing.T) {
	for _, name := range []string{leaf1, leaf2} {
		harness.WaitTargetState(t, s.K8s, name, "READY")

		tgt := s.K8s.Target(t, name)
		state, ok := tgt.Status.ClusterStates[clusterName]
		if !ok {
			t.Fatalf("Target %s has no state for cluster %s", name, clusterName)
		}
		if state.Pod == "" {
			t.Errorf("Target %s names no owning pod", name)
		}
		if state.ConnectionState != "READY" {
			t.Errorf("Target %s connection state: want READY, got %s", name, state.ConnectionState)
		}
	}
}

// TestSpike007_NoPanics checks the collector logs are clean. Cheap, and it
// catches crashes that a status-only assertion would sail past.
func TestSpike007_NoPanics(t *testing.T) {
	pods := s.K8s.ClusterPods(t, clusterName)
	if len(pods) == 0 {
		t.Fatal("no collector pods found")
	}
	for _, p := range pods {
		harness.AssertNoPanics(t, s.K8s.Logs(t, p.Name, 10*time.Minute))
	}
}
