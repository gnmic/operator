package reconciler

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

func fetchExistingTargets(
	ctx context.Context,
	c client.Client,
	ts *gnmicv1alpha1.TargetSource,
) ([]gnmicv1alpha1.Target, error) {

	var targetList gnmicv1alpha1.TargetList

	err := c.List(
		ctx,
		&targetList,
		client.InNamespace(ts.Namespace),
		client.MatchingLabels{
			core.LabelTargetSourceName: ts.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return targetList.Items, nil
}
