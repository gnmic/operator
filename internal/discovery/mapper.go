package discovery

import (
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
)

type Diff struct {
	ToCreate []gnmicv1alpha1.Target
	ToUpdate []gnmicv1alpha1.Target
	ToDelete []gnmicv1alpha1.Target
}

func BuildDiff(existing, discovered []gnmicv1alpha1.Target) Diff {
	var diff Diff

	existingMap := make(map[string]gnmicv1alpha1.Target)
	for _, e := range existing {
		key := e.Namespace + "/" + e.Name
		existingMap[key] = e
	}

	discoveredMap := make(map[string]gnmicv1alpha1.Target)
	for _, e := range discovered {
		key := e.Namespace + "/" + e.Name
		discoveredMap[key] = e
	}

	// Loop for targets to create + update
	for _, t := range discovered {
		key := t.Namespace + "/" + t.Name

		// Check if target already exists
		if e, found := existingMap[key]; found {
			// Check if the spec of the target changed
			if !equality.Semantic.DeepEqual(e.Spec, t.Spec) {
				diff.ToUpdate = append(diff.ToUpdate, t)
			}
		} else { // Target is new
			diff.ToCreate = append(diff.ToCreate, t)
		}
	}

	// Loop for targets to delete
	for _, e := range existing {
		key := e.Namespace + "/" + e.Name

		if e, found := discoveredMap[key]; !found {
			diff.ToDelete = append(diff.ToDelete, e)
		}
	}

	return diff
}
