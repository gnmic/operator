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

// MessageProcessor consumes discovery messages and applies them to Kubernetes
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
	targetCount    int32
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

	logger := log.FromContext(ctx).WithValues(
		"component", "message-processor",
		"targetsource", m.targetSource.Name,
		"namespace", m.targetSource.Namespace,
	)

	logger.Info("Message processor started")

	// Update internal counter in case of a process restart
	if existing, err := fetchExistingTargets(m.ctx, m.client, m.targetSource); err != nil {
		logger.Error(err, "error fetching existing targets")
	} else {
		m.targetCount = int32(len(existing))
	}

	for m.ctx.Err() == nil {
		select {
		case batch, ok := <-m.in:
			if !ok {
				// Channel closed, pipeline is shutting down
				logger.Info("Input channel closed; stopping message processor")
				return nil
			}
			m.queue = append(m.queue, batch...)

		case <-ctx.Done():
			logger.Info("Context was canceled; stopping message processor")
			return nil
		}

		for len(m.queue) > 0 {
			if ctx.Err() != nil {
				return ctx.Err()
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

	logger.Info("Message processor stopped")
	return nil
}

// processMessage handles all of the incoming messages from the channel
func (m *MessageProcessor) processMessage(ctx context.Context, message core.DiscoveryMessage, logger logr.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Type assert to determine if this is a snapshot or event
	switch msg := message.(type) {
	case core.DiscoverySnapshot:
		// Collect snapshot chunks
		logger.Info(
			"Received discovery snapshot chunk",
			"snapshotID", msg.SnapshotID,
			"chunkIndex", msg.ChunkIndex,
			"targets", len(msg.Targets),
		)

		for i := range msg.Targets {
			msg.Targets[i] = normalizeTarget(msg.Targets[i], m.targetSource.Namespace)
		}

		return m.processSnapshot(ctx, msg, logger)

	case core.DiscoveryEvent:
		// Process individual event-driven update
		logger.Info(
			"Received discovery event",
			"event", msg.Event,
			"target", msg.Target.Name,
		)

		msg.Target = normalizeTarget(msg.Target, m.targetSource.Namespace)
		return m.processEvent(ctx, msg, logger)

	default:
		return fmt.Errorf("Unknown discovery message type %T", msg)
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
				"Discarded incomplete discovery snapshot",
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
		logger.Error(
			nil,
			"Snapshot totalChunks mismatch",
			"snapshotID", snapshot.snapshotID,
		)
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= snapshot.totalChunks {
		logger.Error(
			nil,
			"Snapshot chunk index out of range",
			"chunkIndex", chunk.ChunkIndex,
		)
		m.activeSnapshot = nil
		return nil
	}
	if _, exists := snapshot.received[chunk.ChunkIndex]; exists {
		logger.Error(
			nil,
			"Duplicate snapshot chunk received",
			"chunkIndex", chunk.ChunkIndex,
		)
		m.activeSnapshot = nil
		return nil
	}

	snapshot.received[chunk.ChunkIndex] = chunk.Targets

	if len(snapshot.received) == snapshot.totalChunks {
		snapshot.complete = true
	}

	return nil
}

// processEvent handles a single DiscoveryEvent message. If a snapshot is in the queue, the events get deferred and applied after.
func (m *MessageProcessor) processEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	// If snapshot collecting is active defer events
	if m.activeSnapshot != nil {
		m.deferredEvents = append(m.deferredEvents, event)
		return nil
	}

	// Apply events
	err := m.applyEvent(ctx, event, logger)
	if err == nil {
		switch event.Event {
		case core.EventApply:
			m.targetCount++
			m.updateStatus(logger)
		case core.EventDelete:
			m.targetCount--
			m.updateStatus(logger)
		}
	}

	return err
}

// applySnapshot is in charge of getting the Events for the discovered targets and applying them through applyEvent
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
			logger.Error(
				nil,
				"Missing snapshot chunk",
				"chunkIndex", i,
			)
			m.activeSnapshot = nil
			return nil
		}
		allTargets = append(allTargets, chunk...)
	}

	logger.Info(
		"Applying discovery snapshot",
		"snapshotID", snapshot.snapshotID,
		"targets", len(allTargets),
	)

	existing, err := fetchExistingTargets(ctx, m.client, m.targetSource)
	if err != nil {
		logger.Error(err, "error fetching existing targets")
	} else {
		logger.Info("fetched existing targets",
			"numOfTargets", len(existing),
		)
	}

	events := generateEvents(existing, allTargets)

	nApply := 0
	nDelete := 0

	for _, e := range events {
		switch e.Event {
		case core.EventApply:
			nApply++
		case core.EventDelete:
			nDelete++
		}
	}

	logger.Info("generated events",
		"numOfApply", nApply,
		"numOfDelete", nDelete,
	)

	for _, e := range events {
		m.applyEvent(ctx, e, logger)
	}

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

	// Because of idempotency, allTargets = desired state = targets existing in Kubernetes. Overwrites the counter to "reset" it.
	m.targetCount = int32(len(allTargets))
	m.updateStatus(logger)

	m.activeSnapshot = nil
	m.deferredEvents = nil
	return nil
}

func (m *MessageProcessor) applyEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	switch event.Event {
	case core.EventDelete:
		if err := deleteTarget(ctx, m.client, event.Target.Name, m.targetSource.Namespace); err != nil {
			logger.Error(err, "error deleting target",
				"targetName", event.Target.Name,
			)
		} else {
			logger.Info("deleted target object",
				"name", event.Target.Name,
			)
		}
	case core.EventApply:
		target := generateTargetResource(event.Target, m.targetSource)

		if err := applyTarget(ctx, m.client, m.scheme, target, m.targetSource); err != nil {
			logger.Error(err, "error applying target",
				"targetName", event.Target.Name,
			)
		} else {
			logger.Info("applied target object",
				"name", event.Target.Name,
			)
		}
	}

	return nil
}

func (m *MessageProcessor) updateStatus(logger logr.Logger) {
	if err := updateTargetSourceStatus(m.ctx, m.client, m.targetSource, m.targetCount); err != nil {
		logger.Error(err, "error updating TargetSource status")
	} else {
		logger.Info("updated target source status",
			"targetCount", m.targetCount,
		)
	}
}
