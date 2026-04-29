package discovery

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
)

type snapshotBuffer struct {
	snapshotID  string
	totalChunks int
	received    map[int][]core.DiscoveredTarget
	complete    bool
}

// TargetReconciler consumes discovered targets and applies them to Kubernetes
type TargetReconciler struct {
	ctx            context.Context
	client         client.Client
	scheme         *runtime.Scheme
	targetSource   *gnmicv1alpha1.TargetSource
	in             <-chan []core.DiscoveryMessage
	queue          []core.DiscoveryMessage
	activeSnapshot *snapshotBuffer
	// Events are deferred while snapshot is in progress
	deferredEvents []core.DiscoveryEvent
}

// NewTargetReconciler wires a TargetReconciler instance
func NewTargetReconciler(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *TargetReconciler {
	return &TargetReconciler{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (r *TargetReconciler) Run(ctx context.Context) error {
	r.ctx = ctx

	logger := log.FromContext(r.ctx).
		WithValues(
			"name", r.targetSource.Name,
			"namespace", r.targetSource.Namespace,
		)
	logger.Info("target reconciler started")

	for r.ctx.Err() == nil {
		select {
		case batch, ok := <-r.in:
			if !ok {
				// Channel closed, pipeline is shutting down
				logger.Info("input channel closed, stopping target reconciler")
				return nil
			}
			r.queue = append(r.queue, batch...)

		case <-ctx.Done():
			logger.Info("context canceled, stopping target reconciler")
			return nil
		}

		for len(r.queue) > 0 {
			if ctx.Err() != nil {
				return nil // why return nil?
			}

			msg := r.queue[0]
			r.queue = r.queue[1:]

			if err := r.processMessage(r.ctx, msg, logger); err != nil {
				// Returning error lets the supervisor (controller)
				// tear down and restart the pipeline via reconciliation
				// Q: when to return an error vs just log and continue?
				return err
			}

		}
	}

	logger.Info("target reconciler stopped")
	return nil
}

func (r *TargetReconciler) processMessage(ctx context.Context, message core.DiscoveryMessage, logger logr.Logger) error {
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
			"index", msg.ChunkIndex,
			"targetCount", len(msg.Targets),
		)
		return r.processSnapshot(ctx, msg, logger)

	case core.DiscoveryEvent:
		// Process individual event-driven update
		logger.Info(
			"received discovery event",
			"target", msg.Target.Name,
		)
		return r.processEvent(ctx, msg, logger)

	default:
		return fmt.Errorf("unknonw discovery message type %T", msg)
	}
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (r *TargetReconciler) processSnapshot(ctx context.Context, chunk core.DiscoverySnapshot, logger logr.Logger) error {
	if r.activeSnapshot == nil {
		r.startNewSnapshot(chunk, logger)
		return nil
	}

	snapshot := r.activeSnapshot
	// Check if a new snapshot arrived
	if snapshot.snapshotID != chunk.SnapshotID {
		// If current snapshot is complete apply it first
		if snapshot.complete {
			if err := r.applySnapshot(ctx, snapshot, logger); err != nil {
				return err
			}
		} else {
			// If a new snapshot is started before the old one completed
			// the old one can be discarded
			logger.Info(
				"discarding incomplete snapshot",
				"snapshotID", snapshot.snapshotID,
			)
		}

		// Start collecting the new snapshot
		r.startNewSnapshot(chunk, logger)
		return nil
	}

	return r.collectSnapshot(chunk, logger)
}

func (r *TargetReconciler) startNewSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) {
	r.activeSnapshot = &snapshotBuffer{
		snapshotID:  chunk.SnapshotID,
		totalChunks: chunk.TotalChunks,
		received:    make(map[int][]core.DiscoveredTarget),
		complete:    false,
	}
	// Delete buffered events that will be current with new snapshot
	r.deferredEvents = nil

	r.collectSnapshot(chunk, logger)
}

func (r *TargetReconciler) collectSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) error {
	snapshot := r.activeSnapshot

	if chunk.TotalChunks != snapshot.totalChunks {
		logger.Error(nil, "snapshot totalChunks mismatch", "snapshotID", snapshot.snapshotID)
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= snapshot.totalChunks {
		logger.Error(nil, "snapshot chunk index out of range", "index", chunk.ChunkIndex)
		r.activeSnapshot = nil
		return nil
	}
	if _, exists := snapshot.received[chunk.ChunkIndex]; exists {
		logger.Error(nil, "duplicate snapshot chunk", "index", chunk.ChunkIndex)
		r.activeSnapshot = nil
		return nil
	}

	snapshot.received[chunk.ChunkIndex] = chunk.Targets

	if len(snapshot.received) == snapshot.totalChunks {
		snapshot.complete = true
	}

	return nil
}

func (r *TargetReconciler) applySnapshot(ctx context.Context, snapshot *snapshotBuffer, logger logr.Logger) error {
	select {
	case <-ctx.Done():
		r.activeSnapshot = nil
		return nil
	default:
	}

	var allTargets []core.DiscoveredTarget
	for i := 0; i < snapshot.totalChunks; i++ {
		select {
		case <-ctx.Done():
			r.activeSnapshot = nil
			return nil
		default:
		}

		chunk, ok := snapshot.received[i]
		if !ok {
			logger.Error(nil, "missing snapshot chunk", "index", i)
			r.activeSnapshot = nil
			return nil
		}
		allTargets = append(allTargets, chunk...)
	}

	logger.Info(
		"applying snapshot",
		"snapshotID", snapshot.snapshotID,
		"targetCount", len(allTargets),
	)

	// apply all targets
	// a.applyTargets

	// Replay deferred events
	for _, event := range r.deferredEvents {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := r.applyEvent(ctx, event, logger); err != nil {
			return err
		}
	}

	r.activeSnapshot = nil
	r.deferredEvents = nil
	return nil
}

func (r *TargetReconciler) processEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	// If snapshot collecting is active defer events
	if r.activeSnapshot != nil {
		r.deferredEvents = append(r.deferredEvents, event)
		return nil
	}

	// Apply events
	return r.applyEvent(ctx, event, logger)
}

func (r *TargetReconciler) applyEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	switch event.Event {
	case core.EventDelete:
		logger.Info("Would delete target", "name", event.Target.Name)
	case core.EventApply:
		logger.Info("Would apply target", "name", event.Target.Name, "address", event.Target.Address, "labels", event.Target.Labels)
	}
	return nil
}
