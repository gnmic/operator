package http

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/google/uuid"
)

type Loader struct {
	cfg core.LoaderConfig
}

// New instantiates the http loader with the provided config
func New(cfg core.LoaderConfig) core.Loader {
	return &Loader{cfg: cfg}
}

func (l *Loader) Name() string {
	return "http"
}

func (l *Loader) Start(
	ctx context.Context,
	targetsourceNN types.NamespacedName,
	spec gnmicv1alpha1.TargetSourceSpec,
	out chan<- []core.DiscoveryMessage,
) error {
	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", targetsourceNN,
	)

	logger.Info("HTTP loader started")

	// Only for debugging: emit a static snapshot every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("HTTP loader stopped")
			return nil

		case <-ticker.C:
			// Example snapshot (placeholder)
			snapshotID := fmt.Sprintf("snapshot-%s-%s", targetsourceNN, uuid.NewString())
			targets := []core.DiscoveredTarget{
				{
					Name:    "ceos1",
					Address: "clab-3-nodes-ceos1:6030",
					Labels:  map[string]string{"TargetSource": targetsourceNN.String()},
				},
				{
					Name:    "leaf1",
					Address: "clab-3-nodes-leaf1:57400",
					Labels:  map[string]string{"TargetSource": targetsourceNN.String()},
				},
			}

			if err := core.SendSnapshot(ctx, out, targets, snapshotID, l.cfg.ChunkSize); err != nil {
				return err
			}
		}
	}
}
