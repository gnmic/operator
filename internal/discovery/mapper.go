package discovery

import (
	"slices"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func GetNewTargets(existing, discovered []gnmicv1alpha1.Target) ([]gnmicv1alpha1.Target, error) {
	var new []gnmicv1alpha1.Target

	for _, t := range discovered {
		if !slices.ContainsFunc(existing, func(e gnmicv1alpha1.Target) bool {
			return e.ObjectMeta.Name == t.ObjectMeta.Name && e.ObjectMeta.Namespace == t.ObjectMeta.Namespace
		}) {
			new = append(new, t)
		}
	}

	return new, nil
}

func GetUpdatedTargets(existing, discovered []gnmicv1alpha1.Target) ([]gnmicv1alpha1.Target, error) {
	var updated []gnmicv1alpha1.Target

	existingMap := make(map[string]gnmicv1alpha1.Target)

	for _, e := range existing {
		key := e.Namespace + "/" + e.Name
		existingMap[key] = e
	}

	for _, t := range discovered {
		key := t.Namespace + "/" + t.Name

		if e, found := existingMap[key]; found {
			if e.Spec.Address != t.Spec.Address || e.Spec.Profile != t.Spec.Address {
				updated = append(updated, t)
			}
		}
	}

	return updated, nil
}

func GetDeletedTargets(existing, discovered []gnmicv1alpha1.Target) ([]gnmicv1alpha1.Target, error) {
	var deleted []gnmicv1alpha1.Target

	for _, e := range existing {
		if !slices.ContainsFunc(discovered, func(d gnmicv1alpha1.Target) bool {
			return d.ObjectMeta.Name == e.ObjectMeta.Name && d.ObjectMeta.Namespace == e.ObjectMeta.Namespace
		}) {
			deleted = append(deleted, e)
		}
	}

	return deleted, nil
}
