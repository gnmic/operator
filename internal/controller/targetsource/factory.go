package targetsource

import (
	"fmt"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/targetsource/core"
	"github.com/gnmic/operator/internal/controller/targetsource/loaders/http_pull"
)

// NewLoader creates a loader by name
func NewLoader(name string, namespace string, spec gnmicv1alpha1.TargetSourceSpec) (core.Loader, error) {
	loaderName := namespace + "/" + name

	switch {
	case spec.Provider.HTTP != nil:
		return http_pull.New(), nil
	case spec.Provider.Consul != nil:
		return nil, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", loaderName)
	default:
		return nil, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", loaderName)
	}

}
