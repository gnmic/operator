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

func BuildDiff(existing []gnmicv1alpha1.Target, discovered []core.DiscoveredTarget) []core.DiscoveryEvent {
	var events []core.DiscoveryEvent

	existingMap := make(map[string]gnmicv1alpha1.Target)
	for _, e := range existing {
		existingMap[e.ObjectMeta.Name] = e
	}

	discoveredMap := make(map[string]core.DiscoveredTarget)
	for _, d := range discovered {
		discoveredMap[d.Name] = d
	}

	for name, e := range existingMap {
		if _, found := discoveredMap[name]; !found {
			events = append(events, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name: e.Name,
				},
				Event: core.DELETE,
			})
		}
	}

	for _, d := range discoveredMap {
		events = append(events, core.DiscoveryEvent{
			Target: d,
			Event:  core.APPLY,
		})
	}

	return events
}
