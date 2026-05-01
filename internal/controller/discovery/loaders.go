package discovery

import (
	"fmt"

	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/gnmic/operator/internal/controller/discovery/loaders/http"
)

func NewLoader(cfg core.LoaderConfig) (core.Loader, error) {
	switch {
	case cfg.Spec.Provider.HTTP != nil:
		return http.New(cfg), nil
		// webhookActivated := targetSource.Spec.Webhook.Enabled != nil && *targetSource.Spec.Webhook.Enabled

	default:
		return nil, fmt.Errorf("unknown targetsource loader, check TargetSource CRD for %s", cfg.TargetsourceNN)
	}
}
