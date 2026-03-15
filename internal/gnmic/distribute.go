package gnmic

import (
	"sort"

	"github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

// DistributeResult holds the per-pod plans and any targets that could not be
// assigned due to capacity limits.
type DistributeResult struct {
	PerPodPlans       map[int]*ApplyPlan
	UnassignedTargets []string
}

func DistributeTargets(plan *ApplyPlan, numPods int, targetDistribution *v1alpha1.TargetDistributionConfig) *DistributeResult {
	if numPods <= 0 {
		numPods = 1
	}
	currentAssignment := Assignment{}
	if plan.CurrentTargetAssignment != nil {
		for podIndex, targets := range plan.CurrentTargetAssignment {
			for targetNN := range targets {
				currentAssignment[podIndex] = append(currentAssignment[podIndex], targetNN)
			}
			sort.Strings(currentAssignment[podIndex])
		}
	}
	placement := New(PlacementStrategyBoundedHashing)
	placementOptions := &PlacementStrategyOpts{
		NumPods:           numPods,
		CurrentAssignment: currentAssignment,
	}
	if targetDistribution != nil {
		placementOptions.Capacity = targetDistribution.PodCapacity
	}
	newAssignment := placement.distributeTargets(plan.Targets, placementOptions)

	assigned := make(map[string]struct{})
	result := make(map[int]*ApplyPlan)
	for podIndex, targets := range newAssignment {
		result[podIndex] = &ApplyPlan{
			Targets:             make(map[string]*gapi.TargetConfig),
			Subscriptions:       plan.Subscriptions,
			Outputs:             plan.Outputs,
			Inputs:              plan.Inputs,
			Processors:          plan.Processors,
			TunnelTargetMatches: plan.TunnelTargetMatches,
		}
		for _, targetNN := range targets {
			result[podIndex].Targets[targetNN] = plan.Targets[targetNN]
			assigned[targetNN] = struct{}{}
		}
	}

	var unassigned []string
	for targetNN := range plan.Targets {
		if _, ok := assigned[targetNN]; !ok {
			unassigned = append(unassigned, targetNN)
		}
	}
	sort.Strings(unassigned)

	return &DistributeResult{
		PerPodPlans:       result,
		UnassignedTargets: unassigned,
	}
}
