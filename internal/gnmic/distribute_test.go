package gnmic

import (
	"fmt"
	"sort"
	"testing"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

func TestDistributeTargets(t *testing.T) {
	// create a test plan with multiple targets
	plan := &ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"default/target1": {Name: "target1", Address: "10.0.0.1:57400"},
			"default/target2": {Name: "target2", Address: "10.0.0.2:57400"},
			"default/target3": {Name: "target3", Address: "10.0.0.3:57400"},
			"default/target4": {Name: "target4", Address: "10.0.0.4:57400"},
			"default/target5": {Name: "target5", Address: "10.0.0.5:57400"},
		},
		Subscriptions: map[string]*gapi.SubscriptionConfig{
			"default/sub1": {Name: "sub1"},
		},
		Outputs: map[string]map[string]any{
			"default/output1": {"type": "prometheus"},
		},
		Inputs: map[string]map[string]any{
			"default/input1": {"type": "kafka"},
		},
	}

	numPods := 3

	// distribute targets across pods
	distributedPlans := make([]*ApplyPlan, numPods)
	for i := 0; i < numPods; i++ {
		distributedPlans[i] = DistributeTargets(plan, i, numPods)
	}

	// verify each pod gets subscriptions, outputs, inputs
	for i, dp := range distributedPlans {
		if len(dp.Subscriptions) != 1 {
			t.Errorf("pod %d: expected 1 subscription, got %d", i, len(dp.Subscriptions))
		}
		if len(dp.Outputs) != 1 {
			t.Errorf("pod %d: expected 1 output, got %d", i, len(dp.Outputs))
		}
		if len(dp.Inputs) != 1 {
			t.Errorf("pod %d: expected 1 input, got %d", i, len(dp.Inputs))
		}
	}

	// verify all targets are distributed (no duplicates, no missing)
	allTargets := make(map[string]int) // targetNN -> count
	for i, dp := range distributedPlans {
		t.Logf("pod %d targets: %v", i, keys(dp.Targets))
		for targetNN := range dp.Targets {
			allTargets[targetNN]++
		}
	}

	// each target should appear exactly once
	for targetNN, count := range allTargets {
		if count != 1 {
			t.Errorf("target %s appears %d times (expected 1)", targetNN, count)
		}
	}

	// all original targets should be distributed
	if len(allTargets) != len(plan.Targets) {
		t.Errorf("expected %d targets distributed, got %d", len(plan.Targets), len(allTargets))
	}
}

func TestDistributeTargetsDeterministic(t *testing.T) {
	plan := &ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"default/target1": {Name: "target1"},
			"default/target2": {Name: "target2"},
			"default/target3": {Name: "target3"},
		},
		Subscriptions: map[string]*gapi.SubscriptionConfig{},
		Outputs:       map[string]map[string]any{},
		Inputs:        map[string]map[string]any{},
	}

	numPods := 2

	// run distribution multiple times
	for run := 0; run < 10; run++ {
		plan1 := DistributeTargets(plan, 0, numPods)
		plan2 := DistributeTargets(plan, 0, numPods)

		// should get the same targets each time
		if len(plan1.Targets) != len(plan2.Targets) {
			t.Errorf("run %d: non-deterministic distribution", run)
		}

		for targetNN := range plan1.Targets {
			if _, ok := plan2.Targets[targetNN]; !ok {
				t.Errorf("run %d: target %s not consistently assigned", run, targetNN)
			}
		}
	}
}

func TestDistributeTargetsSinglePod(t *testing.T) {
	plan := &ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"default/target1": {Name: "target1"},
			"default/target2": {Name: "target2"},
		},
		Subscriptions: map[string]*gapi.SubscriptionConfig{},
		Outputs:       map[string]map[string]any{},
		Inputs:        map[string]map[string]any{},
	}

	// single pod should get all targets
	distributed := DistributeTargets(plan, 0, 1)
	if len(distributed.Targets) != 2 {
		t.Errorf("single pod should get all targets, got %d", len(distributed.Targets))
	}
}

func TestDistributeTargetsEdgeCases(t *testing.T) {
	plan := &ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"default/target1": {Name: "target1"},
		},
		Subscriptions: map[string]*gapi.SubscriptionConfig{},
		Outputs:       map[string]map[string]any{},
		Inputs:        map[string]map[string]any{},
	}

	// invalid numPods should default to 1
	distributed := DistributeTargets(plan, 0, 0)
	if len(distributed.Targets) != 1 {
		t.Errorf("zero pods should default to 1, got %d targets", len(distributed.Targets))
	}

	// invalid podIndex should default to 0
	distributed = DistributeTargets(plan, -1, 2)
	// just verify it doesn't panic
	t.Logf("negative podIndex: %d targets", len(distributed.Targets))

	distributed = DistributeTargets(plan, 5, 2)
	// should default to pod 0
	t.Logf("out of range podIndex: %d targets", len(distributed.Targets))
}

func TestGetTargetAssignments(t *testing.T) {
	targetNNs := []string{
		"default/target1",
		"default/target2",
		"default/target3",
		"default/target4",
		"default/target5",
	}

	assignments := getTargetAssignments(targetNNs, 3)

	// verify all pods have an entry
	if len(assignments) != 3 {
		t.Errorf("expected 3 pod entries, got %d", len(assignments))
	}

	// verify all targets are assigned
	totalAssigned := 0
	for podIndex, targets := range assignments {
		t.Logf("pod %d: %v", podIndex, targets)
		totalAssigned += len(targets)
	}

	if totalAssigned != len(targetNNs) {
		t.Errorf("expected %d targets assigned, got %d", len(targetNNs), totalAssigned)
	}
}

func TestRendezvousHashStability_AddPod(t *testing.T) {
	// test that when adding a pod, only minimal targets move
	targetNNs := []string{
		"default/router1",
		"default/router2",
		"default/router3",
		"default/router4",
		"default/router5",
		"default/router6",
		"default/router7",
		"default/router8",
		"default/router9",
		"default/router10",
	}

	// get assignments with 3 pods
	assignments3 := getTargetAssignments(targetNNs, 3)
	t.Logf("With 3 pods:")
	for pod, targets := range assignments3 {
		t.Logf("  pod %d: %v", pod, targets)
	}

	// get assignments with 4 pods (add one)
	assignments4 := getTargetAssignments(targetNNs, 4)
	t.Logf("With 4 pods:")
	for pod, targets := range assignments4 {
		t.Logf("  pod %d: %v", pod, targets)
	}

	// count how many targets moved from their original pod (0, 1, or 2)
	moved := 0
	for _, targetNN := range targetNNs {
		oldPod := findPodForTarget(assignments3, targetNN)
		newPod := findPodForTarget(assignments4, targetNN)
		if oldPod != newPod {
			moved++
			t.Logf("  %s: pod %d -> pod %d", targetNN, oldPod, newPod)
		}
	}

	t.Logf("Targets moved: %d/%d", moved, len(targetNNs))

	// with rendezvous hashing, roughly 1/4 should move (new pod gets ~1/(N+1))
	// allow some variance, but it should be much less than 50%
	if moved > len(targetNNs)/2 {
		t.Errorf("too many targets moved: %d (expected < %d)", moved, len(targetNNs)/2)
	}
}

func TestRendezvousHashStability_RemovePod(t *testing.T) {
	// test that when removing a pod, only that pod's targets move
	targetNNs := []string{
		"default/router1",
		"default/router2",
		"default/router3",
		"default/router4",
		"default/router5",
		"default/router6",
	}

	// get assignments with 3 pods
	assignments3 := getTargetAssignments(targetNNs, 3)
	t.Logf("With 3 pods:")
	for pod, targets := range assignments3 {
		t.Logf("  pod %d: %v", pod, targets)
	}

	// get assignments with 2 pods (remove pod 2)
	assignments2 := getTargetAssignments(targetNNs, 2)
	t.Logf("With 2 pods:")
	for pod, targets := range assignments2 {
		t.Logf("  pod %d: %v", pod, targets)
	}

	// targets that were on pod 0 or 1 should stay there
	stayedCount := 0
	for _, targetNN := range targetNNs {
		oldPod := findPodForTarget(assignments3, targetNN)
		newPod := findPodForTarget(assignments2, targetNN)

		if oldPod < 2 && oldPod == newPod {
			stayedCount++
			t.Logf("  %s: stayed on pod %d", targetNN, oldPod)
		} else if oldPod < 2 && oldPod != newPod {
			t.Logf("  %s: UNEXPECTEDLY moved from pod %d to pod %d", targetNN, oldPod, newPod)
		} else {
			t.Logf("  %s: was on removed pod %d, now on pod %d", targetNN, oldPod, newPod)
		}
	}

	// targets on pods 0 and 1 should not move
	targetsOnPod0And1 := len(assignments3[0]) + len(assignments3[1])
	if stayedCount != targetsOnPod0And1 {
		t.Errorf("expected %d targets to stay, but %d stayed", targetsOnPod0And1, stayedCount)
	}
}

func TestBoundedLoadEvenDistribution(t *testing.T) {
	// test that bounded load ensures even distribution
	targetNNs := []string{
		"default/target1",
		"default/target2",
		"default/target3",
		"default/target4",
		"default/target5",
	}

	assignments := getTargetAssignments(targetNNs, 3)
	t.Logf("5 targets across 3 pods:")
	for pod, targets := range assignments {
		t.Logf("  pod %d: %d targets %v", pod, len(targets), targets)
	}

	// with 5 targets and 3 pods, capacity = ceil(5/3) = 2
	// distribution should be 2, 2, 1 (not 1, 1, 3)
	for pod, targets := range assignments {
		if len(targets) > 2 {
			t.Errorf("pod %d has %d targets, exceeds capacity of 2", pod, len(targets))
		}
	}

	// check that max - min <= 1 (even distribution)
	minTargets := len(targetNNs)
	maxTargets := 0
	for _, targets := range assignments {
		if len(targets) < minTargets {
			minTargets = len(targets)
		}
		if len(targets) > maxTargets {
			maxTargets = len(targets)
		}
	}

	if maxTargets-minTargets > 1 {
		t.Errorf("uneven distribution: min=%d, max=%d (diff should be <= 1)", minTargets, maxTargets)
	}
}

func TestBoundedLoadEvenDistribution_LargeScale(t *testing.T) {
	// test with more targets
	targetNNs := make([]string, 100)
	for i := 0; i < 100; i++ {
		targetNNs[i] = fmt.Sprintf("default/router%d", i)
	}

	for numPods := 2; numPods <= 10; numPods++ {
		assignments := getTargetAssignments(targetNNs, numPods)

		minTargets := 100
		maxTargets := 0
		for _, targets := range assignments {
			if len(targets) < minTargets {
				minTargets = len(targets)
			}
			if len(targets) > maxTargets {
				maxTargets = len(targets)
			}
		}

		t.Logf("%d pods: min=%d, max=%d, diff=%d", numPods, minTargets, maxTargets, maxTargets-minTargets)

		// with bounded load, no pod should exceed capacity
		capacity := (100 + numPods - 1) / numPods
		if maxTargets > capacity {
			t.Errorf("%d pods: max=%d exceeds capacity=%d", numPods, maxTargets, capacity)
		}

		// distribution should be reasonably even (diff <= ceil(n/p) - floor(n/p) + small variance)
		// for practical purposes, diff of 3 or less is acceptable
		if maxTargets-minTargets > 3 {
			t.Errorf("%d pods: distribution too uneven, diff=%d (expected <= 3)", numPods, maxTargets-minTargets)
		}
	}
}

func findPodForTarget(assignments map[int][]string, targetNN string) int {
	for pod, targets := range assignments {
		for _, t := range targets {
			if t == targetNN {
				return pod
			}
		}
	}
	return -1
}

func keys[K comparable, V any](m map[K]V) []K {
	result := make([]K, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func PrintChurnOverPodCounts(t *testing.T, targetNNs []string, K int) {
	if K <= 0 {
		K = 1
	}
	if len(targetNNs) == 0 {
		t.Log("no targets")
		return
	}

	// build target -> pod map from assignments map[pod][]target
	buildOwnerMap := func(assignments map[int][]string) map[string]int {
		owner := make(map[string]int, len(targetNNs))
		for pod, targets := range assignments {
			for _, t := range targets {
				owner[t] = pod
			}
		}
		return owner
	}

	t.Logf("Targets: %d\n", len(targetNNs))
	t.Logf("%-10s %-14s %-12s\n", "Pods", "Moved", "Moved%")

	// baseline: 1 pod
	prevAssignments := getTargetAssignments(targetNNs, 1)
	prevOwner := buildOwnerMap(prevAssignments)

	t.Logf("%-10d %-14s %-12s\n", 1, "0", "0.00%")

	// for each P=2..K, compute churn relative to P-1
	for podCount := 2; podCount <= K; podCount++ {
		assignments := getTargetAssignments(targetNNs, podCount)
		owner := buildOwnerMap(assignments)

		moved := 0
		for _, t := range targetNNs {
			if prevOwner[t] != owner[t] {
				moved++
			}
		}

		pct := (float64(moved) / float64(len(targetNNs))) * 100.0
		t.Logf("%-10d %-14d %-11.2f%%\n", podCount, moved, pct)

		prevOwner = owner
		pods := make([]int, 0, len(assignments))
		t.Logf("Distribution at %d pods:", podCount)
		for p := range assignments {
			pods = append(pods, p)
		}
		sort.Ints(pods)
		for _, p := range pods {
			t.Logf("  pod %d: %d targets", p, len(assignments[p]))
		}
	}

}

func TestChurnPrinter(t *testing.T) {
	targetCount := 300
	targets := make([]string, targetCount)
	for i := 0; i < targetCount; i++ {
		targets[i] = fmt.Sprintf("default/router%d", i)
	}
	PrintChurnOverPodCounts(t, targets, 10)
}
