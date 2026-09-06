package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gnmic/operator/internal/discovery/provider"
)

// objectResolver serves Secrets and ConfigMaps to providers from the informer
// cache. A missing object or key comes back as a spec error, because no retry
// will make it appear; the watches on both kinds are what bring the
// TargetSource back once it does.
type objectResolver struct {
	client.Reader
}

func (o objectResolver) SecretData(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	var s corev1.Secret
	if err := o.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, provider.Spec(&provider.NotFoundError{Kind: "Secret", Namespace: namespace, Name: name})
		}
		return nil, err
	}
	return s.Data, nil
}

func (o objectResolver) SecretKey(ctx context.Context, namespace, name, key string) ([]byte, error) {
	data, err := o.SecretData(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	v, ok := data[key]
	if !ok {
		return nil, provider.Spec(&provider.NotFoundError{Kind: "Secret", Namespace: namespace, Name: name, Key: key})
	}
	return v, nil
}

func (o objectResolver) ConfigMapData(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	var cm corev1.ConfigMap
	if err := o.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, provider.Spec(&provider.NotFoundError{Kind: "ConfigMap", Namespace: namespace, Name: name})
		}
		return nil, err
	}
	data := make(map[string][]byte, len(cm.Data)+len(cm.BinaryData))
	for k, v := range cm.Data {
		data[k] = []byte(v)
	}
	for k, v := range cm.BinaryData {
		data[k] = v
	}
	return data, nil
}

func (o objectResolver) ConfigMapKey(ctx context.Context, namespace, name, key string) ([]byte, error) {
	data, err := o.ConfigMapData(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	v, ok := data[key]
	if !ok {
		return nil, provider.Spec(&provider.NotFoundError{Kind: "ConfigMap", Namespace: namespace, Name: name, Key: key})
	}
	return v, nil
}
