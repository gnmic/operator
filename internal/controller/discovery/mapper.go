package discovery

// This file makes diff between existing and new targets
// file decides which targets to create/update/delete

import (
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

func GenerateEvents(existing []gnmicv1alpha1.Target, discovered []core.DiscoveredTarget) []core.DiscoveryEvent {
	var events []core.DiscoveryEvent

	discoveredMap := make(map[string]core.DiscoveredTarget)
	for _, d := range discovered {
		discoveredMap[d.Name] = d
	}

	for _, e := range existing {
		if _, found := discoveredMap[e.Name]; !found {
			events = append(events, core.DiscoveryEvent{
				Target: core.DiscoveredTarget{
					Name: e.Name,
				},
				Event: core.DELETE,
			})
		}
	}

	for _, d := range discovered {
		events = append(events, core.DiscoveryEvent{
			Target: d,
			Event:  core.APPLY,
		})
	}

	return events
}
