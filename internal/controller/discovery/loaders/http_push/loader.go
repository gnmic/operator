package http_push

import (
	"context"
	"errors"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// this file implements the logic receive target updates via HTTP push
// REST API defined internal/apiserver

// Loader implements the HTTP pull discovery mechanism
type Loader struct{}

// New returns a new http_pull loader instance
func New() core.Loader {
	return &Loader{}
}

func (l *Loader) Name() string {
	return "http_push"
}

func (l *Loader) Start(
	ctx context.Context,
	targetsourceName string,
	spec gnmicv1alpha1.TargetSourceSpec,
	out chan<- []core.DiscoveryMessage,
) error {
	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", targetsourceName,
	)
	logger.Info("HTTP push loader started")

	// Input Validation of spec
	if spec.Provider == nil || spec.Provider.HTTP == nil {
		return errors.New("http_push loader requires spec.provider.http to be set")
	}

	// Receive target updates via HTTP push
	var targetEvents []core.DiscoveryEvent

	if err := core.SendEvents(ctx, out, targetEvents); err != nil {
		logger.Error(err, "failed to send events")
		return nil
	}
	return nil
}
