package discovery

// This file makes diff between existing and new targets
// file decides which targets to create/update/delete

import (
	"maps"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

func generateTargetResource(d core.DiscoveredTarget, ts *gnmicv1alpha1.TargetSource) *gnmicv1alpha1.Target {
	t := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.Name,
			Namespace: ts.Namespace,
			Labels:    make(map[string]string),
		},
	}

	t.Spec.Address = d.Address
	t.Spec.Profile = ts.Spec.TargetProfile

	maps.Copy(t.Labels, ts.Spec.TargetLabels)

	for k, v := range d.Labels {
		if strings.HasPrefix(k, ExternalLabelPrefix) {
			switch k {
			case ExternalLabelTargetProfile:
				t.Spec.Profile = v
			default:
				// handle unknown label
			}
		} else {
			t.Labels[k] = v
		}
	}

	t.Labels[LabelTargetSourceName] = ts.Name

	return t
}

func generateEvents(existing []gnmicv1alpha1.Target, discovered []core.DiscoveredTarget) []core.DiscoveryEvent {
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
				Event: core.EventDelete,
			})
		}
	}

	for _, d := range discovered {
		events = append(events, core.DiscoveryEvent{
			Target: d,
			Event:  core.EventApply,
		})
	}

	return events
}

func normalizeTarget(t core.DiscoveredTarget, tsName string) core.DiscoveredTarget {
	t.Name = tsName + "-" + t.Name
	return t
}
