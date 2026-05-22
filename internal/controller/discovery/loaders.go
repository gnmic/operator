package discovery

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	http "github.com/gnmic/operator/internal/controller/discovery/loaders/http"
)

// NewLoader creates a loader by name
func NewLoader(ctx context.Context, c client.Client, cfg *core.CommonLoaderConfig, spec gnmicv1alpha1.TargetSourceSpec) (core.Loader, error) {

	switch {
	case spec.Provider.HTTP != nil:
		httpSpec := *spec.Provider.HTTP
		cfg.AcceptPush = httpSpec.Push.Enabled
		cfg.ResourceFetcher = newK8sResourceFetcher(c)
		return http.New(*cfg, httpSpec), nil
	default:
		return nil, fmt.Errorf("unknown targetsource provider, check TargetSource CRD for %s", cfg.TargetsourceNN)
	}
}
