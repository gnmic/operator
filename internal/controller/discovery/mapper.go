package discovery

// This file makes diff between existing and new targets
// file decides which targets to create/update/delete

import (
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

type Diff struct {
	ToApply  []core.DiscoveredTarget
	ToDelete []core.DiscoveredTarget
}

func BuildDiff(existing []gnmicv1alpha1.Target, discovered []core.DiscoveredTarget) Diff {
	var diff Diff

	discoveredMap := make(map[string]core.DiscoveredTarget)
	for _, e := range discovered {
		discoveredMap[e.Name] = e
	}

	// Loop for targets to delete, else they get applied
	for _, e := range existing {
		if t, found := discoveredMap[e.ObjectMeta.Name]; !found {
			diff.ToDelete = append(diff.ToDelete, core.DiscoveredTarget{
				Name: e.ObjectMeta.Name,
			})
		} else {
			diff.ToApply = append(diff.ToApply, t)
		}
	}

	return diff
}
