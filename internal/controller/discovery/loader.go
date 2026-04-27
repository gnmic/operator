package discovery

import (
	"fmt"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	http "github.com/gnmic/operator/internal/controller/discovery/loaders/http"
	"k8s.io/apimachinery/pkg/types"
)

// NewLoader creates a loader by name
func NewLoader(name types.NamespacedName, spec gnmicv1alpha1.TargetSourceSpec, cfg core.LoaderConfig) (core.Loader, error) {

	switch {
	case spec.Provider.HTTP != nil:
		return http.New(cfg), nil
	case spec.Provider.Consul != nil:
		return nil, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", name)
	default:
		return nil, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", name)
	}

}
