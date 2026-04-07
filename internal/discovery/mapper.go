package discovery

import (
	"slices"

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

	for _, t := range discovered {
		key := t.Namespace + "/" + t.Name

		if e, found := existingMap[key]; found {
			if !equality.Semantic.DeepEqual(e.Spec, t.Spec) {
				diff.ToUpdate = append(diff.ToUpdate, t)
			}
		} else {
			diff.ToCreate = append(diff.ToCreate, t)
		}
	}

	for _, e := range existing {
		if !slices.ContainsFunc(discovered, func(d gnmicv1alpha1.Target) bool {
			return d.ObjectMeta.Name == e.ObjectMeta.Name && d.ObjectMeta.Namespace == e.ObjectMeta.Namespace
		}) {
			diff.ToDelete = append(diff.ToDelete, e)
		}
	}

	return diff
}
