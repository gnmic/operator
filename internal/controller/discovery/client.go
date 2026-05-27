package discovery

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func fetchExistingTargets(ctx context.Context, c client.Client, ts *gnmicv1alpha1.TargetSource) ([]gnmicv1alpha1.Target, error) {
	var targetList gnmicv1alpha1.TargetList

	err := c.List(
		ctx,
		&targetList,
		client.InNamespace(ts.Namespace),
		client.MatchingLabels{
			LabelTargetSourceName: ts.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return targetList.Items, nil
}

func applyTarget(ctx context.Context, c client.Client, s *runtime.Scheme, desired *gnmicv1alpha1.Target, ts *gnmicv1alpha1.TargetSource) error {
	existing := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, c, existing, func() error {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels

		return controllerutil.SetControllerReference(ts, existing, s)
	})

	return err
}

func deleteTarget(ctx context.Context, c client.Client, name string, namespace string) error {
	existing := &gnmicv1alpha1.Target{}

	err := c.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, existing)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	err = c.Delete(ctx, existing)
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}
