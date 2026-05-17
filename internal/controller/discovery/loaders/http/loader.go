package http

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gnmic/operator/internal/controller/discovery/core"
	loaderUtils "github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
	"github.com/google/uuid"
)

type Loader struct {
	commonCfg core.CommonLoaderConfig
}

// New instantiates the http loader with the provided config
func New(cfg core.CommonLoaderConfig) core.Loader {
	return &Loader{commonCfg: cfg}
}

func (l *Loader) Name() string {
	return "http"
}

func (l *Loader) Run(ctx context.Context, out chan<- []core.DiscoveryMessage) error {
	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", l.commonCfg.TargetsourceNN,
	)

	logger.Info(
		"HTTP loader started",
		"targetsource", l.commonCfg.TargetsourceNN.Name,
		"namespace", l.commonCfg.TargetsourceNN.Namespace,
	)

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
			snapshotID := fmt.Sprintf("%s-%s-%s", l.commonCfg.TargetsourceNN.Namespace, l.commonCfg.TargetsourceNN.Name, uuid.NewString())
			targets := []core.DiscoveredTarget{
				{
					Name:    "ceos1",
					Address: "clab-3-nodes-ceos1:6030",
					Labels:  map[string]string{"TargetSource": l.commonCfg.TargetsourceNN.String()},
				},
				{
					Name:    "leaf1",
					Address: "clab-3-nodes-leaf1:57400",
					Labels:  map[string]string{"TargetSource": l.commonCfg.TargetsourceNN.String()},
				},
			}

			if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.commonCfg.ChunkSize); err != nil {
				return err
			}
		}
	}
}
