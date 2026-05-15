package discovery

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
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
			LabelTargetSourceName: ts.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return targetList.Items, nil
}

// Helper: GetSecretValues returns values from a secret
// If keys are provided -> returns only those keys
// If keys is empty -> returns entire secret data
func GetSecretValues(
	ctx context.Context,
	c client.Client,
	namespace string,
	secretRef string,
	keys ...string,
) (map[string]string, error) {
	var secret corev1.Secret
	if err := c.Get(ctx,
		client.ObjectKey{
			Name:      secretRef,
			Namespace: namespace,
		}, &secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretRef, err)
	}

	result := make(map[string]string)

	// Return full secret
	if len(keys) == 0 {
		for k, v := range secret.Data {
			result[k] = string(v)
		}
		return result, nil
	}

	// Return specific keys
	for _, key := range keys {
		val, ok := secret.Data[key]
		if !ok {
			return nil, fmt.Errorf("key %s missing in secret %s/%s", key, namespace, secretRef)
		}
		result[key] = string(val)
	}

	return result, nil
}
