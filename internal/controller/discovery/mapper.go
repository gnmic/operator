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

	existingMap := make(map[string]gnmicv1alpha1.Target)
	for _, e := range existing {
		existingMap[e.ObjectMeta.Name] = e
	}

	discoveredMap := make(map[string]core.DiscoveredTarget)
	for _, d := range discovered {
		discoveredMap[d.Name] = d
	}

	for name, e := range existingMap {
		if d, found := discoveredMap[name]; !found {
			diff.ToDelete = append(diff.ToDelete, core.DiscoveredTarget{
				Name: e.ObjectMeta.Name,
			})
		} else {
			diff.ToApply = append(diff.ToApply, d)
		}
	}

	for name, d := range discoveredMap {
		if _, found := existingMap[name]; !found {
			diff.ToApply = append(diff.ToApply, d)
		}
	}

	return diff
}
