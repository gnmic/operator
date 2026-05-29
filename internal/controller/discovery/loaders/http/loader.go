package http

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	i := 1

	for {
		select {
		case <-ctx.Done():
			logger.Info("HTTP loader stopped")
			return nil

		case <-ticker.C:
			l.commonCfg.Client.UpdateStatus(
				ctx,
				core.StatusUpdate{
					Conditions: []metav1.Condition{
						{
							Type:    core.ConditionTypeReconciling,
							Status:  metav1.ConditionStatus("True"),
							Reason:  string(core.ReasonSyncStarted),
							Message: "Started fetching targets",
						},
					},
				},
			)
			time.Sleep(10 * time.Second)
			// Switch case + i only needed to test behavior for messages with different values.
			switch i {
			case 1:
				snapshotID := fmt.Sprintf("%s-%s-%s", l.commonCfg.TargetsourceNN.Namespace, l.commonCfg.TargetsourceNN.Name, uuid.NewString())
				targets := []core.DiscoveredTarget{
					{
						Name:    "spine1",
						Address: "clab-t1-spine1",
						Port:    57400,
						Labels:  map[string]string{},
					},
					{
						Name:    "leaf1",
						Address: "clab-leaf1",
						Port:    57400,
						Labels:  map[string]string{},
					},
				}

				if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.commonCfg.ChunkSize); err != nil {
					return err
				}
			case 2:
				snapshotID := fmt.Sprintf("%s-%s-%s", l.commonCfg.TargetsourceNN.Namespace, l.commonCfg.TargetsourceNN.Name, uuid.NewString())
				targets := []core.DiscoveredTarget{
					{
						Name:    "spine1",
						Address: "clab-t1-spine1",
						Port:    57400,
						Labels:  map[string]string{},
					},
					{
						Name:    "leaf2",
						Address: "clab-t1-leaf2",
						Port:    57400,
						Labels:  map[string]string{},
					},
				}

				if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.commonCfg.ChunkSize); err != nil {
					return err
				}

			default:
				snapshotID := fmt.Sprintf("%s-%s-%s", l.commonCfg.TargetsourceNN.Namespace, l.commonCfg.TargetsourceNN.Name, uuid.NewString())
				targets := []core.DiscoveredTarget{
					{
						Name:    "spine1",
						Address: "clab-t1-spine1",
						Port:    57400,
						Labels:  map[string]string{},
					},
					{
						Name:    "leaf1",
						Address: "clab-t1-leaf1",
						Port:    57400,
						Labels:  map[string]string{},
					},
					{
						Name:    "leaf2",
						Address: "clab-t1-leaf2",
						Port:    57400,
						Labels:  map[string]string{},
					},
				}

				if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.commonCfg.ChunkSize); err != nil {
					return err
				}
			}

			i++
		}
	}
}
