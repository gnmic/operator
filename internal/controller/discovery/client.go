package discovery

// File may become obsolete, depends on how the logic to compare desired vs. existing state will get implemented

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func FetchExistingTargets(ctx context.Context, c client.Client, ts gnmicv1alpha1.TargetSource) ([]gnmicv1alpha1.Target, error) {
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
