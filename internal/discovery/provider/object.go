package provider

import (
	"context"
	"fmt"
	"sort"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// objectProvider reads a document from a ConfigMap or a Secret in the
// TargetSource namespace. One implementation serves both kinds; only the
// resolver call differs.
type objectProvider struct {
	kind gnmicv1alpha1.SourceType
}

func init() {
	Register(objectProvider{kind: gnmicv1alpha1.SourceTypeConfigMap})
	Register(objectProvider{kind: gnmicv1alpha1.SourceTypeSecret})
}

func (p objectProvider) Type() gnmicv1alpha1.SourceType { return p.kind }

func (p objectProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	var src *gnmicv1alpha1.ObjectSource
	var data map[string][]byte
	var err error
	switch p.kind {
	case gnmicv1alpha1.SourceTypeConfigMap:
		src = req.Source.ConfigMap
		if src == nil {
			return Result{}, Specf("source.configMap is not set")
		}
		data, err = req.Objects.ConfigMapData(ctx, req.Namespace, src.Name)
	case gnmicv1alpha1.SourceTypeSecret:
		src = req.Source.Secret
		if src == nil {
			return Result{}, Specf("source.secret is not set")
		}
		data, err = req.Objects.SecretData(ctx, req.Namespace, src.Name)
	default:
		return Result{}, Specf("unsupported object source %q", p.kind)
	}
	if err != nil {
		return Result{}, err
	}

	var keys []string
	if src.Key != "" {
		if _, ok := data[src.Key]; !ok {
			return Result{}, Specf("%s %s/%s has no key %q", p.kind, req.Namespace, src.Name, src.Key)
		}
		keys = []string{src.Key}
	} else {
		for k := range data {
			keys = append(keys, k)
		}
		// Sorted so the concatenated inventory, and therefore any duplicate
		// resolution downstream, does not depend on map iteration order.
		sort.Strings(keys)
	}

	failOnError := src.Mapping != nil && src.Mapping.OnError == gnmicv1alpha1.MappingErrorFail
	var result Result
	for _, key := range keys {
		devices, failures, err := Parse(data[key], src.Mapping)
		if err != nil {
			if IsSpecError(err) {
				return Result{}, err
			}
			// A document that does not parse is a spec error too: it lives in
			// an object the user controls and will not fix itself.
			return Result{}, Specf("%s %s/%s key %q: %w", p.kind, req.Namespace, src.Name, key, err)
		}
		if failOnError && len(failures) > 0 {
			return Result{}, fmt.Errorf("%s %s/%s key %q: item %s: %s", p.kind, req.Namespace, src.Name, key, failures[0].Name, failures[0].Reason)
		}
		result.Devices = append(result.Devices, devices...)
		result.Invalid = append(result.Invalid, failures...)
	}
	return result, nil
}
