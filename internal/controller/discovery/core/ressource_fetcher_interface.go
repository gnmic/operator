package core

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// ResourceFetcher provides read-only access to namespaced Secret and
// ConfigMap data for loaders without requiring each loader to carry a
// Kubernetes client.
type ResourceFetcher interface {
	GetSecretKey(ctx context.Context, namespace string, selector *corev1.SecretKeySelector) (string, error)
	GetConfigMapKey(ctx context.Context, namespace string, selector *corev1.ConfigMapKeySelector) (string, error)
}
