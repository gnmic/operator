package discovery

import (
	"fmt"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/loaders/http"
)

// NewLoader creates a loader by name
func NewLoader(cfg core.CommonLoaderConfig, spec *gnmicv1alpha1.TargetSourceSpec) (core.Loader, core.CommonLoaderConfig, error) {

	switch {
	case spec.Provider.HTTP != nil:
		cfg.AcceptPush = spec.Provider.HTTP.AcceptPush
		return http.New(cfg), cfg, nil
	case spec.Provider.Consul != nil:
		return nil, cfg, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", cfg.TargetsourceNN)
	default:
		return nil, cfg, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", cfg.TargetsourceNN)
	}
}
