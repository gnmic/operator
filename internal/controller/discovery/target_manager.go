package discovery

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
)

// TargetManager consumes discovered targets and applies them to Kubernetes
type TargetManager struct {
	client       client.Client
	scheme       *runtime.Scheme
	targetSource *gnmicv1alpha1.TargetSource
	in           <-chan []core.DiscoveryMessage
	collected    map[string][]core.DiscoveredTarget
}

// NewTargetManager wires a TargetManager instance
func NewTargetManager(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *TargetManager {
	return &TargetManager{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
		collected:    make(map[string][]core.DiscoveredTarget),
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (m *TargetManager) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).
		WithValues("targetSource", m.targetSource)

	logger.Info("target manager started")

	for {
		select {
		case <-ctx.Done():
			logger.Info("target manager stopped")
			return nil

		case messages := <-m.in:
			for _, message := range messages {
				// Type assert to determine if this is a snapshot or event
				switch msg := message.(type) {
				case core.DiscoverySnapshot:
					// Collect snapshot chunks
					logger.Info(
						"received snapshot chunk",
						"snapshotID", msg.SnapshotID,
						"targetCount", len(msg.Targets),
					)
					m.collected[msg.SnapshotID] = append(m.collected[msg.SnapshotID], msg.Targets...)
					if msg.IsLastChunk {
						m.processSnapshot(msg.SnapshotID, logger)
					}

				case core.DiscoveryEvent:
					// Process individual event-driven update
					logger.Info(
						"received discovery event",
						"target", msg.Target.Name,
					)
					switch msg.Event {
					case core.CREATE:
						logger.Info("Would create target", "name", msg.Target.Name, "address", msg.Target.Address, "labels", msg.Target.Labels)
					case core.UPDATE:
						logger.Info("Would update target", "name", msg.Target.Name, "address", msg.Target.Address, "labels", msg.Target.Labels)
					case core.DELETE:
						logger.Info("Would delete target", "name", msg.Target.Name)
					}
				}
			}
		}
	}
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (m *TargetManager) processSnapshot(snapshotID string, logger logr.Logger) {
	targets := m.collected[snapshotID]
	delete(m.collected, snapshotID)

	logger.Info("Processing full snapshot", "snapshotID", snapshotID, "totalTargets", len(targets))

	if m.targetSource.Spec.Provider.HTTP != nil {
		logger.Info("Would delete all existing targets for targetsource", "targetsource", m.targetSource.Name)
	}

	for _, target := range targets {
		logger.Info("Would create target", "name", target.Name, "address", target.Address, "labels", target.Labels)
	}
}
