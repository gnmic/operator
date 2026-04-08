package gnmic

import (
	"fmt"
	"hash/fnv"
	"sort"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

type PlacementStrategyType string

const (
	PlacementStrategyBoundedHashing PlacementStrategyType = "boundedLoadHashing"
)

// bounded load rendezvous hashing placement implementation
type blrh struct {
}

// podScore is a struct to store the index and score (hash value) of a pod for a target
type podScore struct {
	index int
	score uint64
}

func (p *blrh) distributeTargets(targets map[string]*gapi.TargetConfig, options *PlacementStrategyOpts) Assignment {
	opts := normalizeOptions(options)
	return boundedLoadRendezvousHash(targets, &opts)
}

func normalizeOptions(opts *PlacementStrategyOpts) PlacementStrategyOpts {
	if opts == nil {
		return PlacementStrategyOpts{
			Strategy: PlacementStrategyBoundedHashing,
			NumPods:  1,
		}
	}
	n := *opts
	if n.NumPods <= 0 {
		n.NumPods = 1
	}
	return n
}

func (p *blrh) String() string {
	return string(PlacementStrategyBoundedHashing)
}

func boundedLoadRendezvousHash(targets map[string]*gapi.TargetConfig, options *PlacementStrategyOpts) Assignment {
	numTargets := len(targets)
	if numTargets == 0 {
		return make(Assignment)
	}
	capacity := options.Capacity
	if capacity == 0 {
		// calculate capacity per pod: ceil(numTargets/numPods)
		// this ensures distribution differs by at most 1 between pods
		capacity = (numTargets + options.NumPods - 1) / options.NumPods
	}

	// sort target names for deterministic assignment order
	sortedTargets := make([]string, 0, numTargets)
	for targetNN := range targets {
		sortedTargets = append(sortedTargets, targetNN)
	}
	sort.Strings(sortedTargets)

	assignments := make(Assignment, options.NumPods)

	// keep track of pre-assigned targets to avoid re-assigning them
	var preAssignedTargets = make(map[string]struct{})

	// keep current assignment if it exists
	if options.CurrentAssignment != nil {
		for podIndex, targets := range options.CurrentAssignment {
			if podIndex >= options.NumPods {
				continue
			}
			// sort by hash score to make sure we skip targets with the lower scores if we have to
			// move existing targets from a pod that is already at capacity.
			sort.Slice(targets, func(i, j int) bool {
				hash1 := hashScore(targets[i], podIndex)
				hash2 := hashScore(targets[j], podIndex)
				return hash1 > hash2
			})
			for _, targetNN := range targets {
				if assignments[podIndex] == nil {
					assignments[podIndex] = make([]string, 0, 1)
				}
				if len(assignments[podIndex]) >= capacity {
					// do not keep pre-assigned targets in pods that are already at capacity
					continue
				}
				// add existing target to new assignment
				assignments[podIndex] = append(assignments[podIndex], targetNN)
				preAssignedTargets[targetNN] = struct{}{}
			}
		}
	}

	// assign each target to its highest-scoring pod that has capacity
	for _, targetNN := range sortedTargets {
		// skip pre-assigned targets
		if _, ok := preAssignedTargets[targetNN]; ok {
			continue
		}
		assignedPod := boundedRendezvousHash(targetNN, options.NumPods, capacity, assignments)
		if assignedPod == nil {
			continue
		}
		assignments[*assignedPod] = append(assignments[*assignedPod], targetNN)
	}

	return assignments
}

// boundedRendezvousHash returns the pod index with the highest score that has capacity.
func boundedRendezvousHash(targetNN string, numPods, capacity int, assignments Assignment) *int {
	scores := make([]podScore, numPods)
	for i := range numPods {
		scores[i] = podScore{index: i, score: hashScore(targetNN, i)}
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].index < scores[j].index
		}
		return scores[i].score > scores[j].score
	})

	// find the highest-scoring pod with capacity
	for _, ps := range scores {
		if len(assignments[ps.index]) < capacity {
			return &ps.index
		}
	}

	// fallback (shouldn't happen with proper capacity)
	// return &scores[0].index
	// no pods with capacity found, return nil
	return nil
}

// hashScore computes a deterministic score for a target-pod pair
func hashScore(targetNN string, podIndex int) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s:%d", targetNN, podIndex)
	return h.Sum64()
}
