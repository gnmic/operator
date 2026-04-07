package discovery

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func FetchNewTargets(ctx context.Context, ts gnmicv1alpha1.TargetSource) ([]gnmicv1alpha1.Target, error) {
	var targets []gnmicv1alpha1.Target

	for _, e := range ts.Spec.Manual {
		target := &gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{
				Name:      e.Name,
				Namespace: ts.Namespace,
				Labels: map[string]string{
					"gnmic.io/source": ts.Name,
				},
			},
			Spec: gnmicv1alpha1.TargetSpec{
				Address: e.Address,
				Profile: e.TargetProfile,
			},
		}
		targets = append(targets, *target)
	}

	return targets, nil
}

func GetExistingTargets(ctx context.Context, c client.Client, ts gnmicv1alpha1.TargetSource) ([]gnmicv1alpha1.Target, error) {
	var targetList gnmicv1alpha1.TargetList

	err := c.List(ctx, &targetList,
		client.InNamespace(ts.Namespace),
		client.MatchingLabels{
			"gnmic.io/source": ts.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return targetList.Items, nil
}
