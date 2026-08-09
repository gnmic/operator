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

	// Always emit a plan for every pod, including when there are no targets.
	// An empty PerPodPlans map would skip apply entirely and leave collectors
	// streaming after the last Pipeline is deleted or disabled.
	assigned := make(map[string]struct{})
	result := make(map[int]*ApplyPlan, numPods)
	for podIndex := 0; podIndex < numPods; podIndex++ {
		result[podIndex] = &ApplyPlan{
			Targets:             make(map[string]*gapi.TargetConfig),
			Subscriptions:       plan.Subscriptions,
			Outputs:             plan.Outputs,
			Inputs:              plan.Inputs,
			Processors:          plan.Processors,
			TunnelTargetMatches: plan.TunnelTargetMatches,
		}
	}
	for podIndex, targets := range newAssignment {
		podPlan, ok := result[podIndex]
		if !ok {
			// Placement returned a pod outside 0..numPods-1; ignore.
			continue
		}
		for _, targetNN := range targets {
			podPlan.Targets[targetNN] = plan.Targets[targetNN]
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
