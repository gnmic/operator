package discovery

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gnmic/operator/internal/controller/discovery/core"
)

// k8sResourceFetcher implements core.ResourceFetcher using a controller runtime client
type k8sResourceFetcher struct {
	client client.Client
}

// GetSecretKey retrieves the value of a specific key from a Kubernetes Secret
func (f *k8sResourceFetcher) GetSecretKey(ctx context.Context, namespace string, selector *corev1.SecretKeySelector) (string, error) {
	if selector == nil {
		return "", nil
	}
	var secret corev1.Secret
	key := client.ObjectKey{Namespace: namespace, Name: selector.Name}
	if err := f.client.Get(ctx, key, &secret); err != nil {
		return "", err
	}
	if selector.Key == "" {
		return "", fmt.Errorf("secret key selector has empty key for secret %s/%s", namespace, selector.Name)
	}
	val, ok := secret.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s does not contain key %s", namespace, selector.Name, selector.Key)
	}
	return string(val), nil
}

// GetConfigMapKey retrieves the value of a specific key from a Kubernetes ConfigMap
func (f *k8sResourceFetcher) GetConfigMapKey(ctx context.Context, namespace string, selector *corev1.ConfigMapKeySelector) (string, error) {
	if selector == nil {
		return "", nil
	}
	var cm corev1.ConfigMap
	key := client.ObjectKey{Namespace: namespace, Name: selector.Name}
	if err := f.client.Get(ctx, key, &cm); err != nil {
		return "", err
	}
	if selector.Key == "" {
		return "", fmt.Errorf("config map key selector has empty key for config map %s/%s", namespace, selector.Name)
	}
	val, ok := cm.Data[selector.Key]
	if !ok {
		return "", fmt.Errorf("config map %s/%s does not contain key %s", namespace, selector.Name, selector.Key)
	}
	return val, nil
}

func newK8sResourceFetcher(c client.Client) core.ResourceFetcher {
	return &k8sResourceFetcher{client: c}
}
