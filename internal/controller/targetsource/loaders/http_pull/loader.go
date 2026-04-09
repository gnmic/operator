package http_pull

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gnmic/operator/internal/controller/targetsource"
)

type Loader struct{}

// New instantiates the http_pull loader
func New() targetsource.Loader {
	return &Loader{}
}

func (l *Loader) Name() string {
	return "http_pull"
}

func (l *Loader) Start(ctx context.Context, out chan<- []targetsource.DiscoveredTarget) error {
	logger := log.FromContext(ctx).WithValues("loader", l.Name())

	logger.Info("HTTP pull loader started")

	// Only for debugging: emit a static snapshot every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("HTTP pull loader stopped")
			return nil

		case <-ticker.C:
			// Example snapshot (placeholder)
			targets := []targetsource.DiscoveredTarget{
				{
					Name:    "ceos1",
					Address: "clab-3-nodes-ceos1:6030",
					Labels:  map[string]string{"TargetSourceType": l.Name()},
				},
				{
					Name:    "leaf1",
					Address: "clab-3-nodes-leaf1:57400",
					Labels:  map[string]string{"TargetSourceType": l.Name()},
				},
			}

			// Non-blocking context-aware send
			select {
			case out <- targets:
				logger.V(1).Info(
					"emitted target snapshot",
					"count", len(targets),
				)
			case <-ctx.Done():
				logger.Info("context cancelled while emitting targets")
				return nil
			}
		}
	}
}

func init() {
	targetsource.Register("http_pull", New)
}
