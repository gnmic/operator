package gnmic

import (
	"fmt"
	"hash/fnv"
	"sort"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

// DistributeTargets creates a copy of the apply plan with only the targets
// assigned to the specified pod index. Other resources (subscriptions, outputs,
// inputs, processors, tunnel-target-matches) are included in full for all pods.
//
// Uses bounded load rendezvous hashing for stable AND even distribution:
// - Targets are assigned to pods based on highest hash score
// - Each pod has a capacity limit of ceil(numTargets/numPods) + 1
// - When a pod is full, the target goes to the next highest scoring pod
func DistributeTargets(plan *ApplyPlan, podIndex, numPods int) *ApplyPlan {
	if numPods <= 0 {
		numPods = 1
	}
	if podIndex < 0 || podIndex >= numPods {
		podIndex = 0
	}

	// create a new plan with the same subscriptions, outputs, inputs, processors, tunnel-target-matches
	distributed := &ApplyPlan{
		Targets:             make(map[string]*gapi.TargetConfig),
		Subscriptions:       plan.Subscriptions,
		Outputs:             plan.Outputs,
		Inputs:              plan.Inputs,
		Processors:          plan.Processors,
		TunnelTargetMatches: plan.TunnelTargetMatches,
	}

	// get all assignments using bounded load rendezvous hashing
	assignments := boundedRendezvousAssign(plan.Targets, numPods)

	// filter to only targets assigned to this pod
	for targetNN, assignedPod := range assignments {
		if assignedPod == podIndex {
			distributed.Targets[targetNN] = plan.Targets[targetNN]
		}
	}

	return distributed
}

// boundedRendezvousAssign assigns targets to pods using bounded load rendezvous hashing.
// returns a map of targetNN -> podIndex
func boundedRendezvousAssign(targets map[string]*gapi.TargetConfig, numPods int) map[string]int {
	numTargets := len(targets)
	if numTargets == 0 {
		return make(map[string]int)
	}

	// calculate capacity per pod: ceil(numTargets/numPods)
	// this ensures distribution differs by at most 1 between pods
	capacity := (numTargets + numPods - 1) / numPods

	// sort target names for deterministic assignment order
	sortedTargets := make([]string, 0, numTargets)
	for targetNN := range targets {
		sortedTargets = append(sortedTargets, targetNN)
	}
	sort.Strings(sortedTargets)

	// track load per pod
	podLoad := make([]int, numPods)
	assignments := make(map[string]int, numTargets)

	// assign each target to its highest-scoring pod that has capacity
	for _, targetNN := range sortedTargets {
		assignedPod := boundedRendezvousHash(targetNN, numPods, capacity, podLoad)
		assignments[targetNN] = assignedPod
		podLoad[assignedPod]++
	}

	return assignments
}

// boundedRendezvousHash returns the pod index with the highest score that has capacity.
func boundedRendezvousHash(targetNN string, numPods, capacity int, podLoad []int) int {
	// get all pods sorted by score (highest first)
	type podScore struct {
		index int
		score uint32
	}
	scores := make([]podScore, numPods)
	for i := 0; i < numPods; i++ {
		scores[i] = podScore{index: i, score: hashScore(targetNN, i)}
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// find the highest-scoring pod with capacity
	for _, ps := range scores {
		if podLoad[ps.index] < capacity {
			return ps.index
		}
	}

	// fallback (shouldn't happen with proper capacity)
	return scores[0].index
}

// hashScore computes a deterministic score for a target-pod pair
func hashScore(targetNN string, podIndex int) uint32 {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s:%d", targetNN, podIndex)
	return h.Sum32()
}

// getTargetAssignments returns a map of podIndex -> list of targetNNs.
// used in tests
func getTargetAssignments(targetNNs []string, numPods int) map[int][]string {
	// build a fake targets map
	targets := make(map[string]*gapi.TargetConfig, len(targetNNs))
	for _, nn := range targetNNs {
		targets[nn] = &gapi.TargetConfig{}
	}

	// get assignments
	assignments := boundedRendezvousAssign(targets, numPods)

	// convert to pod -> targets format
	result := make(map[int][]string)
	for i := 0; i < numPods; i++ {
		result[i] = []string{}
	}
	for targetNN, podIndex := range assignments {
		result[podIndex] = append(result[podIndex], targetNN)
	}

	// sort each pod's targets for consistent output
	for i := range result {
		sort.Strings(result[i])
	}

	return result
}
