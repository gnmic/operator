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

// MessageProcessor consumes discovered targets and applies them to Kubernetes
type MessageProcessor struct {
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

// NewMessageProcessor wires a MessageProcessor instance
func NewMessageProcessor(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *MessageProcessor {
	return &MessageProcessor{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (m *MessageProcessor) Run(ctx context.Context) error {
	m.ctx = ctx

	logger := log.FromContext(m.ctx).
		WithValues(
			"name", m.targetSource.Name,
			"namespace", m.targetSource.Namespace,
		)
	logger.Info("target handler started")

	for m.ctx.Err() == nil {
		select {
		case batch, ok := <-m.in:
			if !ok {
				// Channel closed, pipeline is shutting down
				logger.Info("input channel closed, stopping target handler")
				return nil
			}
			m.queue = append(m.queue, batch...)

		case <-ctx.Done():
			logger.Info("context canceled, stopping target handler")
			return nil
		}

		for len(m.queue) > 0 {
			if ctx.Err() != nil {
				return nil // why return nil?
			}

			msg := m.queue[0]
			m.queue = m.queue[1:]

			if err := m.processMessage(m.ctx, msg, logger); err != nil {
				// Returning error lets the supervisor (controller)
				// tear down and restart the pipeline via reconciliation
				// Q: when to return an error vs just log and continue?
				return err
			}

		}
	}

	logger.Info("target handler stopped")
	return nil
}

func (m *MessageProcessor) processMessage(ctx context.Context, message core.DiscoveryMessage, logger logr.Logger) error {
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
		return m.processSnapshot(ctx, msg, logger)

	case core.DiscoveryEvent:
		// Process individual event-driven update
		logger.Info(
			"received discovery event",
			"target", msg.Target.Name,
		)
		return m.processEvent(ctx, msg, logger)

	default:
		return fmt.Errorf("unknonw discovery message type %T", msg)
	}
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (m *MessageProcessor) processSnapshot(ctx context.Context, chunk core.DiscoverySnapshot, logger logr.Logger) error {
	if m.activeSnapshot == nil {
		m.startNewSnapshot(chunk, logger)
		return nil
	}

	snapshot := m.activeSnapshot
	// Check if a new snapshot arrived
	if snapshot.snapshotID != chunk.SnapshotID {
		// If current snapshot is complete apply it first
		if snapshot.complete {
			if err := m.applySnapshot(ctx, snapshot, logger); err != nil {
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
		m.startNewSnapshot(chunk, logger)
		return nil
	}

	return m.collectSnapshot(chunk, logger)
}

func (m *MessageProcessor) startNewSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) {
	m.activeSnapshot = &snapshotBuffer{
		snapshotID:  chunk.SnapshotID,
		totalChunks: chunk.TotalChunks,
		received:    make(map[int][]core.DiscoveredTarget),
		complete:    false,
	}
	// Delete buffered events that will be current with new snapshot
	m.deferredEvents = nil

	m.collectSnapshot(chunk, logger)
}

func (m *MessageProcessor) collectSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) error {
	snapshot := m.activeSnapshot

	if chunk.TotalChunks != snapshot.totalChunks {
		logger.Error(nil, "snapshot totalChunks mismatch", "snapshotID", snapshot.snapshotID)
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= snapshot.totalChunks {
		logger.Error(nil, "snapshot chunk index out of range", "index", chunk.ChunkIndex)
		m.activeSnapshot = nil
		return nil
	}
	if _, exists := snapshot.received[chunk.ChunkIndex]; exists {
		logger.Error(nil, "duplicate snapshot chunk", "index", chunk.ChunkIndex)
		m.activeSnapshot = nil
		return nil
	}

	snapshot.received[chunk.ChunkIndex] = chunk.Targets

	if len(snapshot.received) == snapshot.totalChunks {
		snapshot.complete = true
	}

	return nil
}

func (m *MessageProcessor) applySnapshot(ctx context.Context, snapshot *snapshotBuffer, logger logr.Logger) error {
	select {
	case <-ctx.Done():
		m.activeSnapshot = nil
		return nil
	default:
	}

	var allTargets []core.DiscoveredTarget
	for i := 0; i < snapshot.totalChunks; i++ {
		select {
		case <-ctx.Done():
			m.activeSnapshot = nil
			return nil
		default:
		}

		chunk, ok := snapshot.received[i]
		if !ok {
			logger.Error(nil, "missing snapshot chunk", "index", i)
			m.activeSnapshot = nil
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
	for _, event := range m.deferredEvents {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := m.applyEvent(ctx, event, logger); err != nil {
			return err
		}
	}

	m.activeSnapshot = nil
	m.deferredEvents = nil
	return nil
}

func (m *MessageProcessor) processEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	// If snapshot collecting is active defer events
	if m.activeSnapshot != nil {
		m.deferredEvents = append(m.deferredEvents, event)
		return nil
	}

	// Apply events
	return m.applyEvent(ctx, event, logger)
}

func (m *MessageProcessor) applyEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	switch event.Event {
	case core.CREATE:
		logger.Info("Would create target", "name", event.Target.Name, "address", event.Target.Address, "labels", event.Target.Labels)
	case core.UPDATE:
		logger.Info("Would update target", "name", event.Target.Name, "address", event.Target.Address, "labels", event.Target.Labels)
	case core.DELETE:
		logger.Info("Would delete target", "name", event.Target.Name)
	}
	return nil
}
