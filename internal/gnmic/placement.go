package gnmic

import (
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

type placementStrategy interface {
	distributeTargets(targets map[string]*gapi.TargetConfig, options *PlacementStrategyOpts) Assignment
}

type PlacementStrategyOpts struct {
	// Strategy to use for placement
	// if not set, the default strategy will be used
	Strategy PlacementStrategyType `json:"strategy,omitempty"`
	// Number of pods to distribute targets to
	// if not set, the number of pods in the cluster is used
	NumPods int `json:"numPods,omitempty"`
	// Capacity per pod
	// if not set, ceil(target/numPods) is used
	Capacity int `json:"capacity,omitempty"`
	// Current assignment of targets to pods
	// if not set, it is assumed that there is no current assignment
	CurrentAssignment Assignment `json:"currentAssignment,omitempty"`
}

func New(strategy PlacementStrategyType) placementStrategy {
	switch strategy {
	case PlacementStrategyBoundedHashing:
		return &blrh{}
	default:
		return &blrh{}
	}
}

// TargetToPodAssignment is a map of pod index to list of target names
type Assignment map[int][]string
