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

// TargetApplier consumes discovered targets and applies them to Kubernetes
type TargetApplier struct {
	client       client.Client
	scheme       *runtime.Scheme
	targetSource *gnmicv1alpha1.TargetSource
	in           <-chan []core.DiscoveryMessage
	collected    map[string][]core.DiscoveredTarget
}

// NewTargetApplier wires a TargetApplier instance
func NewTargetApplier(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *TargetApplier {
	return &TargetApplier{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
		collected:    make(map[string][]core.DiscoveredTarget),
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (m *TargetApplier) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).
		WithValues("targetSource", m.targetSource)
	logger.Info("target applier started")

	queue := make([]core.DiscoveryMessage, 0, 265)

	for ctx.Err() == nil {
		select {
		case batch, ok := <-m.in:
			if !ok {
				// Channel closed, pipeline is shutting down
				logger.Info("input channel closed, stopping target applier")
				return nil
			}
			queue = append(queue, batch...)

		case <-ctx.Done():
			logger.Info("context canceled, stopping target applier")
			return nil
		}

		for len(queue) > 0 {
			if ctx.Err() != nil {
				break
			}

			msg := queue[0]
			queue = queue[1:]

			if err := m.handleMessage(ctx, msg, logger); err != nil {
				// Returning error lets the supervisor (controller)
				// tear down and restart the pipeline via reconciliation
				// Q: when to return an error vs just log and continue?
				return err
			}

		}
	}

	logger.Info("target applier stopped")
	return nil
}

func (m *TargetApplier) handleMessage(ctx context.Context, message core.DiscoveryMessage, logger logr.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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

	return nil
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (m *TargetApplier) processSnapshot(snapshotID string, logger logr.Logger) {
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
