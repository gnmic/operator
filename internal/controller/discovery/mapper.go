package discovery

import (
	"maps"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

// generateTargetResource converts a DiscoveredTarget into a Kubernetes Target Object based on the TargetSource Spec.
// Returns the Target Resource and a map of unknown operator labels.
func generateTargetResource(d core.DiscoveredTarget, ts *gnmicv1alpha1.TargetSource) (*gnmicv1alpha1.Target, map[string]string) {
	// Create object instance
	t := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.Name,
			Namespace: ts.Namespace,
			Labels:    make(map[string]string),
		},
	}
	unknownLabels := make(map[string]string)

	// Add Address from DiscoveredTarget
	t.Spec.Address = d.Address
	// Add default Target Profile from the TargetSource Spec TargetProfile
	t.Spec.Profile = ts.Spec.TargetProfile

	// Copy TargetLabels from TargetSource Spec
	maps.Copy(t.Labels, ts.Spec.TargetLabels)

	// Handle labels from Source of Truth
	for k, v := range d.Labels {
		if strings.HasPrefix(k, ExternalLabelPrefix) {
			switch k {
			case ExternalLabelTargetProfile: // Overwrite TargetProfile if specified by SoT
				t.Spec.Profile = v
			default:
				unknownLabels[k] = v
			}
		} else { // Copy all other labels into the Target
			t.Labels[k] = v
		}
	}

	// Add TargetSource Label to the Target (precedence over all labels)
	t.Labels[LabelTargetSourceName] = ts.Name

	return t, unknownLabels
}

// generateEvents returns a list of DiscoveryEvents. Needed for snapshot handling to determine which devices get deleted and which applied.
func generateEvents(existing []gnmicv1alpha1.Target, discovered []core.DiscoveredTarget) []core.DiscoveryEvent {
	var events []core.DiscoveryEvent

	discoveredMap := make(map[string]core.DiscoveredTarget)
	for _, d := range discovered {
		discoveredMap[d.Name] = d
	}

	// Create delete events for targets which are present in existing but not in discovered
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

	// Create apply events for all targets in discovered
	for _, d := range discovered {
		events = append(events, core.DiscoveryEvent{
			Target: d,
			Event:  core.EventApply,
		})
	}

	return events
}

// normalizeTarget adds the prefix to the target name for identification in Kubernetes
func normalizeTarget(t core.DiscoveredTarget, tsName string) core.DiscoveredTarget {
	t.Name = tsName + "-" + t.Name
	return t
}
