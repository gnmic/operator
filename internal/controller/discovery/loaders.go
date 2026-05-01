package discovery

import (
	"fmt"

	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/loaders/http"
)

func NewLoader(cfg core.LoaderConfig) (core.Loader, error) {
	switch {
	case cfg.Spec.Provider.HTTP != nil:
		cfg.AcceptPush = cfg.Spec.Provider.HTTP.AcceptPush
		return http.New(cfg), nil

	default:
		return nil, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", cfg.TargetsourceNN)
	}
}
