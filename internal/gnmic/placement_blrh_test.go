package gnmic

import (
	"fmt"
	"sort"
	"testing"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

type assigmentResult struct {
	podIndex int
	targets  []string
}

func Test_boundedLoadRendezvousHash(t *testing.T) {
	tests := []struct {
		name    string
		targets map[string]*gapi.TargetConfig
		options *PlacementStrategyOpts
	}{
		{
			name:    "targets=10/numPods=3",
			targets: genTargets(10),
			options: &PlacementStrategyOpts{NumPods: 3},
		},
		{
			name:    "targets=100/numPods=5",
			targets: genTargets(100),
			options: &PlacementStrategyOpts{NumPods: 5},
		},
		{
			name:    "targets=1000/numPods=3",
			targets: genTargets(1000),
			options: &PlacementStrategyOpts{NumPods: 3},
		},
		{
			name:    "targets=1000/numPods=5",
			targets: genTargets(1000),
			options: &PlacementStrategyOpts{NumPods: 5},
		},
		{
			name:    "targets=1000/numPods=10",
			targets: genTargets(1000),
			options: &PlacementStrategyOpts{NumPods: 10},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boundedLoadRendezvousHash(tt.targets, tt.options)

			assertAllTargetsAssignedExactlyOnce(t, tt.targets, got)
			assertCapacityRespected(t, len(tt.targets), tt.options.NumPods, got)

			// determinism: running again with same inputs must yield identical result
			got2 := boundedLoadRendezvousHash(tt.targets, tt.options)
			moved := countMovedTargets(got, got2)
			if moved != 0 {
				t.Errorf("non-deterministic: %d targets moved on re-run", moved)
			}

			rs := make([]assigmentResult, 0, len(got))
			for podIndex, targets := range got {
				rs = append(rs, assigmentResult{podIndex: podIndex, targets: targets})
			}
			sort.Slice(rs, func(i, j int) bool {
				return rs[i].podIndex < rs[j].podIndex
			})
			for _, r := range rs {
				t.Logf("pod %d: %d targets", r.podIndex, len(r.targets))
			}
		})
	}
}

func genTargets(n int) map[string]*gapi.TargetConfig {
	out := make(map[string]*gapi.TargetConfig, n)
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("target-%02d", i)
		out[name] = &gapi.TargetConfig{
			Name:    name,
			Address: fmt.Sprintf("%s:57400", name),
		}
	}
	return out
}

func TestBoundedLoadRendezvousHash_ChurnAcrossScalingAndTargetAddition(t *testing.T) {
	targets := genTargets(35)

	type scaleStep struct {
		numPods int
	}

	steps := []scaleStep{
		{numPods: 1},
		{numPods: 2},
		{numPods: 3},
		{numPods: 4},
		{numPods: 5},
		{numPods: 6},
		{numPods: 7},
		{numPods: 8},
		{numPods: 9},
		{numPods: 10},
	}

	var prev Assignment
	for i, step := range steps {
		opts := &PlacementStrategyOpts{
			NumPods: step.numPods,
		}
		curr := boundedLoadRendezvousHash(targets, opts)

		// sanity checks
		assertAllTargetsAssignedExactlyOnce(t, targets, curr)
		assertCapacityRespected(t, len(targets), step.numPods, curr)

		if i == 0 {
			t.Logf("pods=%d targets=%d moved=N/A (initial placement)", step.numPods, len(targets))
		} else {
			moved := countMovedTargets(prev, curr)
			t.Logf("pods=%d targets=%d moved=%d", step.numPods, len(targets), moved)
		}
		prev = curr
	}

	// Add one target and measure churn among existing targets.
	targetsPlusOne := genTargets(36)
	opts := &PlacementStrategyOpts{
		NumPods: 10,
	}
	withOneMore := boundedLoadRendezvousHash(targetsPlusOne, opts)

	assertAllTargetsAssignedExactlyOnce(t, targetsPlusOne, withOneMore)
	assertCapacityRespected(t, len(targetsPlusOne), opts.NumPods, withOneMore)

	movedExisting := countMovedTargetsForTargetSet(prev, withOneMore, targetNames(targets))
	t.Logf("pods=%d targets=%d->%d existing_targets_moved=%d new_target_assigned=%t",
		opts.NumPods,
		len(targets),
		len(targetsPlusOne),
		movedExisting,
		isTargetAssigned(withOneMore, "target-36"),
	)
}

func targetNames(targets map[string]*gapi.TargetConfig) []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func assertAllTargetsAssignedExactlyOnce(t *testing.T, targets map[string]*gapi.TargetConfig, a Assignment) {
	t.Helper()

	seen := make(map[string]int, len(targets))
	for _, names := range a {
		for _, name := range names {
			seen[name]++
		}
	}

	if len(seen) != len(targets) {
		t.Fatalf("expected %d assigned targets, got %d", len(targets), len(seen))
	}

	for name := range targets {
		if seen[name] != 1 {
			t.Fatalf("target %q assigned %d times, want exactly once", name, seen[name])
		}
	}
}

func assertCapacityRespected(t *testing.T, numTargets, numPods int, a Assignment) {
	t.Helper()

	capacity := (numTargets + numPods - 1) / numPods
	for pod, names := range a {
		if len(names) > capacity {
			t.Fatalf("pod %d has %d targets, exceeds capacity %d", pod, len(names), capacity)
		}
	}
}

func countMovedTargets(prev, curr Assignment) int {
	prevIndex := invertAssignment(prev)
	currIndex := invertAssignment(curr)

	moved := 0
	for target, prevPod := range prevIndex {
		currPod, ok := currIndex[target]
		if !ok {
			continue
		}
		if currPod != prevPod {
			moved++
		}
	}
	return moved
}

func countMovedTargetsForTargetSet(prev, curr Assignment, targets []string) int {
	prevIndex := invertAssignment(prev)
	currIndex := invertAssignment(curr)

	moved := 0
	for _, target := range targets {
		prevPod, okPrev := prevIndex[target]
		currPod, okCurr := currIndex[target]
		if !okPrev || !okCurr {
			continue
		}
		if prevPod != currPod {
			moved++
		}
	}
	return moved
}

func invertAssignment(a Assignment) map[string]int {
	out := make(map[string]int)
	for pod, names := range a {
		for _, name := range names {
			out[name] = pod
		}
	}
	return out
}

func isTargetAssigned(a Assignment, target string) bool {
	for _, names := range a {
		for _, name := range names {
			if name == target {
				return true
			}
		}
	}
	return false
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_Stable(t *testing.T) {
	targets := genTargets(30)
	numPods := 5

	// compute a fresh assignment
	fresh := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: numPods})
	assertAllTargetsAssignedExactlyOnce(t, targets, fresh)
	assertCapacityRespected(t, len(targets), numPods, fresh)

	// feed the fresh result back as CurrentAssignment
	withCurrent := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{
		NumPods:           numPods,
		CurrentAssignment: fresh,
	})
	assertAllTargetsAssignedExactlyOnce(t, targets, withCurrent)
	assertCapacityRespected(t, len(targets), numPods, withCurrent)

	moved := countMovedTargets(fresh, withCurrent)
	if moved != 0 {
		t.Errorf("re-running with same CurrentAssignment should move 0 targets, moved %d", moved)
	}
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_ScaleUp(t *testing.T) {
	targets := genTargets(30)

	// initial: 5 pods
	initial := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: 5})
	assertAllTargetsAssignedExactlyOnce(t, targets, initial)

	// scale to 6 pods, passing initial as CurrentAssignment
	scaled := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{
		NumPods:           6,
		CurrentAssignment: initial,
	})
	assertAllTargetsAssignedExactlyOnce(t, targets, scaled)
	assertCapacityRespected(t, len(targets), 6, scaled)

	moved := countMovedTargets(initial, scaled)
	t.Logf("scale 5->6 pods, %d targets moved", moved)

	// new pod 5 should have some targets
	if len(scaled[5]) == 0 {
		t.Error("new pod 5 should have received targets after scale-up")
	}

	// compare to a fresh assignment without CurrentAssignment
	freshScaled := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: 6})
	movedFresh := countMovedTargets(initial, freshScaled)
	t.Logf("scale 5->6 without CurrentAssignment: %d targets moved", movedFresh)

	// using CurrentAssignment should move fewer (or equal) targets than a fresh recompute
	if moved > movedFresh {
		t.Errorf("CurrentAssignment should reduce churn: moved %d vs fresh %d", moved, movedFresh)
	}
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_ScaleDown(t *testing.T) {
	targets := genTargets(30)

	// initial: 6 pods
	initial := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: 6})
	assertAllTargetsAssignedExactlyOnce(t, targets, initial)

	// scale down to 4 pods -- remove pods 4 and 5
	// only pass assignments for pods 0-3 as CurrentAssignment
	remaining := make(Assignment)
	for pod, tgts := range initial {
		if pod < 4 {
			remaining[pod] = tgts
		}
	}

	scaled := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{
		NumPods:           4,
		CurrentAssignment: remaining,
	})
	assertAllTargetsAssignedExactlyOnce(t, targets, scaled)
	assertCapacityRespected(t, len(targets), 4, scaled)

	// targets that were on pods 0-3 and stayed should not move
	moved := countMovedTargetsForTargetSet(initial, scaled, assignedToPodsInRange(initial, 0, 3))
	t.Logf("scale 6->4: %d targets from surviving pods moved", moved)
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_TargetAdded(t *testing.T) {
	targets := genTargets(30)
	numPods := 5

	initial := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: numPods})
	assertAllTargetsAssignedExactlyOnce(t, targets, initial)

	// add one target
	targets31 := genTargets(31)
	withNew := boundedLoadRendezvousHash(targets31, &PlacementStrategyOpts{
		NumPods:           numPods,
		CurrentAssignment: initial,
	})
	assertAllTargetsAssignedExactlyOnce(t, targets31, withNew)
	assertCapacityRespected(t, len(targets31), numPods, withNew)

	if !isTargetAssigned(withNew, "target-31") {
		t.Error("new target-31 should be assigned")
	}

	moved := countMovedTargetsForTargetSet(initial, withNew, targetNames(targets))
	t.Logf("added 1 target: %d existing targets moved", moved)
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_TargetRemoved(t *testing.T) {
	targets := genTargets(30)
	numPods := 5

	initial := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: numPods})
	assertAllTargetsAssignedExactlyOnce(t, targets, initial)

	// remove target-15
	targets29 := genTargets(30)
	delete(targets29, "target-15")

	// filter CurrentAssignment to exclude the removed target
	filtered := filterAssignment(initial, targets29)

	withRemoved := boundedLoadRendezvousHash(targets29, &PlacementStrategyOpts{
		NumPods:           numPods,
		CurrentAssignment: filtered,
	})
	assertAllTargetsAssignedExactlyOnce(t, targets29, withRemoved)
	assertCapacityRespected(t, len(targets29), numPods, withRemoved)

	if isTargetAssigned(withRemoved, "target-15") {
		t.Error("removed target-15 should not be assigned")
	}

	moved := countMovedTargetsForTargetSet(initial, withRemoved, targetNames(targets29))
	t.Logf("removed 1 target: %d existing targets moved", moved)
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_OverCapacity(t *testing.T) {
	targets := genTargets(20)
	numPods := 4
	// capacity will be ceil(20/4) = 5

	// create a CurrentAssignment where pod 0 has 7 targets (over capacity)
	overloaded := Assignment{
		0: {"target-01", "target-02", "target-03", "target-04", "target-05", "target-06", "target-07"},
		1: {"target-08", "target-09", "target-10"},
		2: {"target-11", "target-12", "target-13", "target-14", "target-15"},
		3: {"target-16", "target-17", "target-18", "target-19", "target-20"},
	}

	result := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{
		NumPods:           numPods,
		CurrentAssignment: overloaded,
	})
	assertAllTargetsAssignedExactlyOnce(t, targets, result)
	assertCapacityRespected(t, len(targets), numPods, result)

	// pod 0 should now be at or under capacity (5)
	if len(result[0]) > 5 {
		t.Errorf("pod 0 should be capped at capacity 5, has %d", len(result[0]))
	}
}

func TestBoundedLoadRendezvousHash_CurrentAssignment_Idempotent(t *testing.T) {
	targets := genTargets(50)
	numPods := 7

	// run three times, each feeding the previous result as CurrentAssignment
	r1 := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: numPods})
	r2 := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: numPods, CurrentAssignment: r1})
	r3 := boundedLoadRendezvousHash(targets, &PlacementStrategyOpts{NumPods: numPods, CurrentAssignment: r2})

	moved12 := countMovedTargets(r1, r2)
	moved23 := countMovedTargets(r2, r3)

	if moved12 != 0 {
		t.Errorf("r1->r2 should move 0 targets, moved %d", moved12)
	}
	if moved23 != 0 {
		t.Errorf("r2->r3 should move 0 targets, moved %d", moved23)
	}
}

func assignedToPodsInRange(a Assignment, lo, hi int) []string {
	var out []string
	for pod, names := range a {
		if pod >= lo && pod <= hi {
			out = append(out, names...)
		}
	}
	return out
}

func filterAssignment(a Assignment, targets map[string]*gapi.TargetConfig) Assignment {
	filtered := make(Assignment, len(a))
	for pod, names := range a {
		for _, name := range names {
			if _, exists := targets[name]; exists {
				filtered[pod] = append(filtered[pod], name)
			}
		}
	}
	return filtered
}
