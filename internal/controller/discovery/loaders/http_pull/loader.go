package http_pull

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/google/uuid"
)

const (
	chunkSize = 100
)

type Loader struct{}

// New instantiates the http_pull loader
func New() core.Loader {
	return &Loader{}
}

func (l *Loader) Name() string {
	return "http_pull"
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
			snapshotID := fmt.Sprintf("snapshot-%s-%s", targetsourceName, uuid.NewString())
			targets := []core.DiscoveredTarget{
				{
					Name:    "ceos1",
					Address: "clab-3-nodes-ceos1:6030",
					Labels:  map[string]string{"TargetSource": targetsourceName},
				},
				{
					Name:    "leaf1",
					Address: "clab-3-nodes-leaf1:57400",
					Labels:  map[string]string{"TargetSource": targetsourceName},
				},
			}

			if err := core.SendSnapshot(ctx, out, targets, snapshotID, chunkSize); err != nil {
				return err
			}
		}
	}
}
