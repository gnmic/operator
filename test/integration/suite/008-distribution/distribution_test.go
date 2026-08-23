//go:build integration

// Package distribution covers target placement across collector pods:
// balanced loads, podCapacity, rebalancing on scale, and the collected-once
// invariant during and after membership changes.
package distribution

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

var allTargets = []string{
	"dev1", "dev2", "dev3", "dev4", "dev5", "dev6", "dev7", "dev8", "dev9",
}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "008-distribution",
		RequireTargets: allTargets,
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

// waitIdle blocks until no suite target has an open stream. Previous tests'
// collectors can linger briefly after Cluster deletion; starting the next
// cluster before that settles makes stream-count assertions lie.
func waitIdle(t *testing.T) {
	t.Helper()
	harness.Wait(t, harness.Medium, "all simulated targets idle", func() (bool, string) {
		for _, name := range allTargets {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
}

// waitBalanced waits until every ready pod owns at least one target and loads
// differ by at most one.
func waitBalanced(t *testing.T, cluster string, replicas int, targets []string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("balanced placement across %d pods", replicas), func() (bool, string) {
		as := assignments(t, cluster, targets)
		loads := loadPerPod(as)
		if len(loads) != replicas {
			return false, fmt.Sprintf("owners=%v", loads)
		}
		min, max := -1, -1
		for _, n := range loads {
			if min < 0 || n < min {
				min = n
			}
			if n > max {
				max = n
			}
		}
		if max-min > 1 {
			return false, fmt.Sprintf("unbalanced %v", loads)
		}
		for _, name := range targets {
			if as[name] == "" {
				return false, name + " unassigned"
			}
		}
		return true, ""
	})
}

// startCluster applies a Cluster and a Pipeline binding the suite targets,
// then waits until every expected target shows the expected stream count.
func startCluster(t *testing.T, name string, replicas, podCapacity int, wantTargets []string) {
	t.Helper()
	waitIdle(t)
	// Every optional template key is set: the fixture renderer uses
	// missingkey=error, so a bare {{ if .PodCapacity }} still fails when absent.
	s.K8s.ApplyFile(t, "fixtures/cluster.yaml", map[string]any{
		"Name":        name,
		"Image":       harness.GnmicImage(),
		"Replicas":    replicas,
		"PodCapacity": podCapacity,
	})
	s.K8s.ApplyFile(t, "fixtures/pipeline.yaml", map[string]any{
		"Name":       name,
		"Cluster":    name,
		"ExtraLabel": "",
		"ExtraValue": "",
	})
	s.K8s.WaitReadyPods(t, name, replicas, harness.Long)
	if wantTargets != nil {
		waitAssigned(t, name, wantTargets)
		s.GnmiGen.AssertCollectedOnce(t, 1, wantTargets...)
	}
}

func waitAssigned(t *testing.T, cluster string, targets []string) {
	t.Helper()
	harness.Wait(t, harness.Medium, "all targets assigned", func() (bool, string) {
		as := assignments(t, cluster, targets)
		missing := []string{}
		for _, name := range targets {
			if as[name] == "" {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return false, fmt.Sprintf("unassigned: %v", missing)
		}
		return true, ""
	})
}

// assignments returns target → owning pod for the given cluster.
func assignments(t *testing.T, cluster string, targets []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(targets))
	for _, name := range targets {
		tgt, err := s.K8s.TargetQuiet(name)
		if err != nil {
			out[name] = ""
			continue
		}
		if st, ok := tgt.Status.ClusterStates[cluster]; ok {
			out[name] = st.Pod
		}
	}
	return out
}

func loadPerPod(as map[string]string) map[string]int {
	loads := map[string]int{}
	for _, pod := range as {
		if pod != "" {
			loads[pod]++
		}
	}
	return loads
}

func assertBalanced(t *testing.T, as map[string]string, replicas int) {
	t.Helper()
	loads := loadPerPod(as)
	if len(loads) != replicas {
		t.Fatalf("want %d pods owning targets, got %d: %v", replicas, len(loads), loads)
	}
	min, max := -1, -1
	for _, n := range loads {
		if min < 0 || n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	if max-min > 1 {
		t.Fatalf("unbalanced loads: %v (max-min=%d)", loads, max-min)
	}
}

func assertDisjoint(t *testing.T, as map[string]string) {
	t.Helper()
	seen := map[string]string{} // target → pod already checked; pairs of pods
	byPod := map[string][]string{}
	for tgt, pod := range as {
		if pod == "" {
			t.Errorf("target %s has no owner", tgt)
			continue
		}
		byPod[pod] = append(byPod[pod], tgt)
		if prev, ok := seen[tgt]; ok {
			t.Errorf("target %s claimed by both %s and %s", tgt, prev, pod)
		}
		seen[tgt] = pod
	}
	// Pairwise disjoint is automatic if each target appears once; the map
	// structure guarantees it. Sanity: every target appears exactly once.
	if len(seen) != len(as) {
		t.Errorf("assignment map size mismatch: %d keys, %d assigned", len(as), len(seen))
	}
	_ = byPod
}

func establishedSnapshot(targets []string) map[string]time.Time {
	out := make(map[string]time.Time, len(targets))
	for _, name := range targets {
		out[name] = s.GnmiGen.EstablishedAt(name)
	}
	return out
}

func assertMostlyStable(t *testing.T, before, after map[string]string, maxMoved int) {
	t.Helper()
	moved := 0
	for tgt, prev := range before {
		if prev == "" {
			continue
		}
		if after[tgt] != prev {
			moved++
		}
	}
	if moved > maxMoved {
		t.Fatalf("moved %d targets, want at most %d\nbefore=%v\nafter=%v", moved, maxMoved, before, after)
	}
}

func assertEstablishedPreserved(t *testing.T, before map[string]time.Time, asBefore, asAfter map[string]string) {
	t.Helper()
	for tgt, prevPod := range asBefore {
		if prevPod == "" || asAfter[tgt] != prevPod {
			continue
		}
		was := before[tgt]
		now := s.GnmiGen.EstablishedAt(tgt)
		if was.IsZero() || now.IsZero() {
			continue
		}
		// Allow a small clock skew; a re-establish would jump by seconds.
		if now.Sub(was).Abs() > time.Second {
			t.Errorf("target %s re-established on %s: %s -> %s", tgt, prevPod, was, now)
		}
	}
}

// TestDist001_TargetsSpreadAcrossPods checks three replicas use all three pods
// with balanced loads.
func TestDist001_TargetsSpreadAcrossPods(t *testing.T) {
	const cluster = "spread"
	startCluster(t, cluster, 3, 0, allTargets)

	as := assignments(t, cluster, allTargets)
	assertBalanced(t, as, 3)
	assertDisjoint(t, as)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount:      harness.I32(9),
		UnassignedTargets: harness.I32(0),
	})
}

// TestDist002_ExactlyOneCollectorPerTarget is the invariant on its own.
func TestDist002_ExactlyOneCollectorPerTarget(t *testing.T) {
	const cluster = "once"
	startCluster(t, cluster, 3, 0, allTargets)

	s.GnmiGen.ConsistentlyCollectedOnce(t, 30*time.Second, 1, allTargets...)
	assertDisjoint(t, assignments(t, cluster, allTargets))
}

// TestDist003_PodCapacityCapsPlacement checks podCapacity is a hard limit.
func TestDist003_PodCapacityCapsPlacement(t *testing.T) {
	const cluster = "cap"
	startCluster(t, cluster, 3, 2, nil)

	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		UnassignedTargets: harness.I32(3),
		TargetsCount:      harness.I32(9),
	})
	harness.WaitClusterCondition(t, s.K8s, cluster, harness.CondCapacityExhausted, metav1.ConditionTrue, harness.Medium)

	// Status and the wire can converge at different times; wait until both agree.
	var gotAssigned, gotUnassigned []string
	harness.Wait(t, harness.Medium, "capacity split: 6 collected, 3 idle", func() (bool, string) {
		as := assignments(t, cluster, allTargets)
		gotAssigned, gotUnassigned = nil, nil
		loads := loadPerPod(as)
		for _, n := range loads {
			if n > 2 {
				return false, fmt.Sprintf("load exceeds capacity: %v", loads)
			}
		}
		for _, name := range allTargets {
			streams := s.GnmiGen.StreamCount(name)
			if as[name] == "" {
				gotUnassigned = append(gotUnassigned, name)
				if streams != 0 {
					return false, fmt.Sprintf("%s unassigned but has %d streams", name, streams)
				}
			} else {
				gotAssigned = append(gotAssigned, name)
				if streams != 1 {
					return false, fmt.Sprintf("%s assigned to %s but has %d streams", name, as[name], streams)
				}
			}
		}
		if len(gotAssigned) != 6 || len(gotUnassigned) != 3 {
			return false, fmt.Sprintf("assigned=%v unassigned=%v", gotAssigned, gotUnassigned)
		}
		return true, ""
	})
}

// TestDist004_CapacityExhaustionReported checks overflow is visible in status,
// then clears when capacity is raised by scaling.
func TestDist004_CapacityExhaustionReported(t *testing.T) {
	const cluster = "capstat"
	startCluster(t, cluster, 3, 2, nil)

	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		UnassignedTargets: harness.I32(3),
	})
	harness.WaitClusterCondition(t, s.K8s, cluster, harness.CondCapacityExhausted, metav1.ConditionTrue, harness.Medium)

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":5}}`)
	s.K8s.WaitReadyPods(t, cluster, 5, harness.Long)
	waitAssigned(t, cluster, allTargets)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		UnassignedTargets: harness.I32(0),
		TargetsCount:      harness.I32(9),
	})
	harness.WaitClusterCondition(t, s.K8s, cluster, harness.CondCapacityExhausted, metav1.ConditionFalse, harness.Medium)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

// TestDist005_ScalingUpRebalances checks new pods receive work.
func TestDist005_ScalingUpRebalances(t *testing.T) {
	const cluster = "scaleup"
	startCluster(t, cluster, 1, 0, allTargets)

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":3}}`)
	s.K8s.WaitReadyPods(t, cluster, 3, harness.Long)
	waitBalanced(t, cluster, 3, allTargets)

	as := assignments(t, cluster, allTargets)
	assertBalanced(t, as, 3)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

// TestDist006_ScalingUpMovesFewTargets checks bounded-hashing stability.
func TestDist006_ScalingUpMovesFewTargets(t *testing.T) {
	const cluster = "stable"
	startCluster(t, cluster, 2, 0, allTargets)

	before := assignments(t, cluster, allTargets)
	estab := establishedSnapshot(allTargets)

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":3}}`)
	s.K8s.WaitReadyPods(t, cluster, 3, harness.Long)
	// waitAssigned is not enough: every target stays assigned to the original
	// two pods until bounded-hashing overflows them onto the new replica.
	// Snapshotting at that instant fails assertBalanced with "got 2 pods".
	waitBalanced(t, cluster, 3, allTargets)

	after := assignments(t, cluster, allTargets)
	assertBalanced(t, after, 3)
	assertMostlyStable(t, before, after, 5)
	assertEstablishedPreserved(t, estab, before, after)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

// TestDist007_ScalingDownReassignsOrphans checks targets leave removed pods.
func TestDist007_ScalingDownReassignsOrphans(t *testing.T) {
	const cluster = "scaledown"
	startCluster(t, cluster, 3, 0, allTargets)

	before := assignments(t, cluster, allTargets)
	orphans := []string{}
	pod2 := harness.PodName(cluster, 2)
	for tgt, pod := range before {
		if pod == pod2 {
			orphans = append(orphans, tgt)
		}
	}
	if len(orphans) == 0 {
		t.Fatal("pod-2 owns no targets; cannot assert reassignment")
	}

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":2}}`)
	s.K8s.WaitPodGone(t, pod2)
	s.K8s.WaitReadyPods(t, cluster, 2, harness.Long)
	waitAssigned(t, cluster, allTargets)

	after := assignments(t, cluster, allTargets)
	for _, tgt := range orphans {
		if after[tgt] == "" || after[tgt] == pod2 {
			t.Errorf("orphan %s not reassigned; owner=%q", tgt, after[tgt])
		}
	}
	assertBalanced(t, after, 2)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{ReadyReplicas: harness.I32(2)})
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

// TestDist008_NoDoubleCollectionDuringScale holds the invariant during a
// scale transition, not only after it settles.
//
// A single sample at 2 can be a polling race against gNMIc tearing down a
// stream after config/apply returns. Sustained doubles (≥1s consecutive) are
// the real bug: start-on-new before stop-on-old.
func TestDist008_NoDoubleCollectionDuringScale(t *testing.T) {
	const cluster = "transition"
	startCluster(t, cluster, 2, 0, allTargets)

	var (
		mu          sync.Mutex
		doubleFor   = map[string]time.Duration{}
		doubleSince = map[string]time.Time{}
		zeroFor     = map[string]time.Duration{}
		lastZero    = map[string]time.Time{}
		stop        = make(chan struct{})
		done        = make(chan struct{})
	)
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				now := time.Now()
				for _, name := range allTargets {
					n := s.GnmiGen.StreamCount(name)
					mu.Lock()
					switch {
					case n > 1:
						if doubleSince[name].IsZero() {
							doubleSince[name] = now
						}
					default:
						if !doubleSince[name].IsZero() {
							if d := now.Sub(doubleSince[name]); d > doubleFor[name] {
								doubleFor[name] = d
							}
							doubleSince[name] = time.Time{}
						}
					}
					if n == 0 {
						if lastZero[name].IsZero() {
							lastZero[name] = now
						}
					} else if !lastZero[name].IsZero() {
						if d := now.Sub(lastZero[name]); d > zeroFor[name] {
							zeroFor[name] = d
						}
						lastZero[name] = time.Time{}
					}
					mu.Unlock()
				}
			}
		}
	}()

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":4}}`)
	s.K8s.WaitReadyPods(t, cluster, 4, harness.Long)
	waitAssigned(t, cluster, allTargets)
	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":2}}`)
	s.K8s.WaitReadyPods(t, cluster, 2, harness.Long)
	waitAssigned(t, cluster, allTargets)

	close(stop)
	<-done

	mu.Lock()
	defer mu.Unlock()
	// config/apply returns before the old Subscribe is gone, so a brief overlap
	// is expected. The sampler ticks every 250ms, so a 1.0s overlap reports as
	// 1.25s; rust teardown has been measured just over 1s. 2s still fails a
	// start-on-new-before-stop-on-old bug (seconds of doubles) without treating
	// apply-ACK lag as one.
	const maxDouble = 2 * time.Second
	const maxZero = 30 * time.Second
	for _, name := range allTargets {
		dup := doubleFor[name]
		if !doubleSince[name].IsZero() {
			if d := time.Since(doubleSince[name]); d > dup {
				dup = d
			}
		}
		if dup > maxDouble {
			t.Errorf("%s double-collected for %s (limit %s)", name, dup, maxDouble)
		}
		gap := zeroFor[name]
		if !lastZero[name].IsZero() {
			if d := time.Since(lastZero[name]); d > gap {
				gap = d
			}
		}
		if gap > maxZero {
			t.Errorf("%s was at zero streams for %s (limit %s)", name, gap, maxZero)
		}
	}
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

// TestDist009_PodDeletionRecoversAssignments checks unplanned pod loss, and
// that the apply-cache record for the dead pod is invalidated so the
// recreated pod is re-configured.
//
// The Cluster controller fingerprints each pod's apply payload and skips the
// POST when the hash matches. A restart leaves the desired plan unchanged, so
// without invalidation (TargetState SSE disconnect) the short-circuit would
// skip the re-apply forever and the victim's targets would stay uncollected.
func TestDist009_PodDeletionRecoversAssignments(t *testing.T) {
	const cluster = "poddel"
	startCluster(t, cluster, 3, 0, allTargets)

	before := assignments(t, cluster, allTargets)
	estab := establishedSnapshot(allTargets)
	victim := harness.PodName(cluster, 1)

	var owned, survivors []string
	for _, name := range allTargets {
		switch before[name] {
		case victim:
			owned = append(owned, name)
		case "":
			t.Fatalf("%s has no owner before deletion", name)
		default:
			survivors = append(survivors, name)
		}
	}
	if len(owned) == 0 {
		t.Fatalf("%s owns no targets; cannot assert re-apply", victim)
	}

	pod := &corev1.Pod{}
	pod.Name = victim
	pod.Namespace = s.Namespace
	// The StatefulSet recreates the pod immediately, so do not wait for absence.
	s.K8s.DeleteNow(t, pod)

	// Prove the restart emptied the victim's work before asserting recovery.
	for _, name := range owned {
		s.GnmiGen.WaitStreams(t, name, 0)
	}

	s.K8s.WaitReadyPods(t, cluster, 3, harness.Long)
	waitAssigned(t, cluster, allTargets)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{ReadyReplicas: harness.I32(3)})
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)

	after := assignments(t, cluster, allTargets)
	for _, name := range owned {
		if after[name] == "" {
			t.Errorf("%s has no owner after recovery", name)
		}
	}

	// Survivors must not have been churned by the re-apply of the victim.
	survivorBefore := map[string]string{}
	survivorAfter := map[string]string{}
	for _, name := range survivors {
		survivorBefore[name] = before[name]
		survivorAfter[name] = after[name]
	}
	assertEstablishedPreserved(t, estab, survivorBefore, survivorAfter)

	// Victim's targets must be new streams — same pod name, new process.
	for _, name := range owned {
		was := estab[name]
		now := s.GnmiGen.EstablishedAt(name)
		if was.IsZero() || now.IsZero() {
			t.Errorf("%s missing established_at before=%v after=%v", name, was, now)
			continue
		}
		if now.Sub(was).Abs() <= time.Second {
			t.Errorf("%s stream survived pod restart on %s (established_at=%s); config was not re-applied",
				name, victim, now)
		}
	}
}

// TestDist010_AddingTargetDoesNotChurnExisting checks membership growth is
// minimally disruptive.
func TestDist010_AddingTargetDoesNotChurnExisting(t *testing.T) {
	const cluster = "add"
	// Hold dev9 out of the pipeline by clearing its suite label, then restore.
	dev9 := s.K8s.Target(t, "dev9")
	s.K8s.Patch(t, dev9, `{"metadata":{"labels":{"suite":null}}}`)
	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.Target(t, "dev9"), `{"metadata":{"labels":{"suite":"008"}}}`)
	})

	eight := allTargets[:8]
	startCluster(t, cluster, 3, 0, eight)

	before := assignments(t, cluster, eight)
	estab := establishedSnapshot(eight)

	s.K8s.Patch(t, s.K8s.Target(t, "dev9"), `{"metadata":{"labels":{"suite":"008"}}}`)
	waitAssigned(t, cluster, allTargets)

	after := assignments(t, cluster, allTargets)
	if after["dev9"] == "" {
		t.Fatal("dev9 was not assigned")
	}
	s.GnmiGen.WaitStreams(t, "dev9", 1)

	moved := 0
	for _, name := range eight {
		if before[name] != after[name] {
			moved++
		}
	}
	if moved > 1 {
		t.Fatalf("adding one target moved %d existing owners; want at most 1", moved)
	}
	assertEstablishedPreserved(t, estab, before, after)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

// TestDist011_RemovingTargetDoesNotChurnRest checks clean removal.
func TestDist011_RemovingTargetDoesNotChurnRest(t *testing.T) {
	const cluster = "remove"
	startCluster(t, cluster, 3, 0, allTargets)

	before := assignments(t, cluster, allTargets)
	estab := establishedSnapshot(allTargets)

	s.K8s.Delete(t, s.K8s.Target(t, "dev5"))
	t.Cleanup(func() {
		dev5 := `
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: dev5
  labels:
    suite: "008"
spec:
  address: ` + s.TargetAddress(4) + `
  profile: default
`
		if _, err := s.K8s.ApplyYAMLNoCleanup(dev5, nil); err != nil {
			fmt.Fprintf(os.Stderr, "restore dev5: %v\n", err)
		}
	})

	s.GnmiGen.WaitStreams(t, "dev5", 0)
	rest := []string{"dev1", "dev2", "dev3", "dev4", "dev6", "dev7", "dev8", "dev9"}
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{TargetsCount: harness.I32(8)})

	after := assignments(t, cluster, rest)
	moved := 0
	for _, name := range rest {
		if before[name] != after[name] {
			moved++
		}
	}
	if moved > 1 {
		t.Fatalf("removing one target moved %d others; want at most 1", moved)
	}
	assertEstablishedPreserved(t, estab, before, after)
	s.GnmiGen.AssertCollectedOnce(t, 1, rest...)
}

// TestDist012_AssignmentSurvivesOperatorRestart checks placement is
// deterministic across a manager restart. Runs last in this package: it
// restarts the shared controller in gnmic-system.
//
// An empty apply-cache after restart means every pod is re-configured once, so
// streams may refresh; the assert is that owners do not reshuffle and the
// collected-once invariant holds after the manager is back.
func TestDist012_AssignmentSurvivesOperatorRestart(t *testing.T) {
	const cluster = "oprestart"
	startCluster(t, cluster, 3, 0, allTargets)

	before := assignments(t, cluster, allTargets)
	assertBalanced(t, before, 3)
	assertDisjoint(t, before)

	s.K8s.RestartOperator(t)

	harness.WaitClusterReady(t, s.K8s, cluster)
	s.K8s.WaitReadyPods(t, cluster, 3, harness.Long)
	waitAssigned(t, cluster, allTargets)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)

	after := assignments(t, cluster, allTargets)
	for _, name := range allTargets {
		if before[name] != after[name] {
			t.Errorf("%s reshuffled: %q -> %q", name, before[name], after[name])
		}
	}
	assertBalanced(t, after, 3)
	assertDisjoint(t, after)
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, allTargets...)
}
