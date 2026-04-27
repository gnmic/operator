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

// TargetHandler consumes discovered targets and applies them to Kubernetes
type TargetHandler struct {
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

// NewTargetHandler wires a TargetHandler instance
func NewTargetHandler(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *TargetHandler {
	return &TargetHandler{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (c *TargetHandler) Run(ctx context.Context) error {
	c.ctx = ctx

	logger := log.FromContext(c.ctx).
		WithValues(
			"name", c.targetSource.Name,
			"namespace", c.targetSource.Namespace,
		)
	logger.Info("target handler started")

	for c.ctx.Err() == nil {
		select {
		case batch, ok := <-c.in:
			if !ok {
				// Channel closed, pipeline is shutting down
				logger.Info("input channel closed, stopping target handler")
				return nil
			}
			c.queue = append(c.queue, batch...)

		case <-ctx.Done():
			logger.Info("context canceled, stopping target handler")
			return nil
		}

		for len(c.queue) > 0 {
			if ctx.Err() != nil {
				return nil // why return nil?
			}

			msg := c.queue[0]
			c.queue = c.queue[1:]

			if err := c.processMessage(c.ctx, msg, logger); err != nil {
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

func (c *TargetHandler) processMessage(ctx context.Context, message core.DiscoveryMessage, logger logr.Logger) error {
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
		return c.processSnapshot(ctx, msg, logger)

	case core.DiscoveryEvent:
		// Process individual event-driven update
		logger.Info(
			"received discovery event",
			"target", msg.Target.Name,
		)
		return c.processEvent(ctx, msg, logger)

	default:
		return fmt.Errorf("unknonw discovery message type %T", msg)
	}
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (c *TargetHandler) processSnapshot(ctx context.Context, chunk core.DiscoverySnapshot, logger logr.Logger) error {
	if c.activeSnapshot == nil {
		c.startNewSnapshot(chunk, logger)
		return nil
	}

	snapshot := c.activeSnapshot
	// Check if a new snapshot arrived
	if snapshot.snapshotID != chunk.SnapshotID {
		// If current snapshot is complete apply it first
		if snapshot.complete {
			if err := c.applySnapshot(ctx, snapshot, logger); err != nil {
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
		c.startNewSnapshot(chunk, logger)
		return nil
	}

	return c.collectSnapshot(chunk, logger)
}

func (c *TargetHandler) startNewSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) {
	c.activeSnapshot = &snapshotBuffer{
		snapshotID:  chunk.SnapshotID,
		totalChunks: chunk.TotalChunks,
		received:    make(map[int][]core.DiscoveredTarget),
		complete:    false,
	}
	// Delete buffered events that will be current with new snapshot
	c.deferredEvents = nil

	c.collectSnapshot(chunk, logger)
}

func (c *TargetHandler) collectSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) error {
	snapshot := c.activeSnapshot

	if chunk.TotalChunks != snapshot.totalChunks {
		logger.Error(nil, "snapshot totalChunks mismatch", "snapshotID", snapshot.snapshotID)
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= snapshot.totalChunks {
		logger.Error(nil, "snapshot chunk index out of range", "index", chunk.ChunkIndex)
		c.activeSnapshot = nil
		return nil
	}
	if _, exists := snapshot.received[chunk.ChunkIndex]; exists {
		logger.Error(nil, "duplicate snapshot chunk", "index", chunk.ChunkIndex)
		c.activeSnapshot = nil
		return nil
	}

	snapshot.received[chunk.ChunkIndex] = chunk.Targets

	if len(snapshot.received) == snapshot.totalChunks {
		snapshot.complete = true
	}

	return nil
}

func (c *TargetHandler) applySnapshot(ctx context.Context, snapshot *snapshotBuffer, logger logr.Logger) error {
	select {
	case <-ctx.Done():
		c.activeSnapshot = nil
		return nil
	default:
	}

	var allTargets []core.DiscoveredTarget
	for i := 0; i < snapshot.totalChunks; i++ {
		select {
		case <-ctx.Done():
			c.activeSnapshot = nil
			return nil
		default:
		}

		chunk, ok := snapshot.received[i]
		if !ok {
			logger.Error(nil, "missing snapshot chunk", "index", i)
			c.activeSnapshot = nil
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
	for _, event := range c.deferredEvents {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := c.applyEvent(ctx, event, logger); err != nil {
			return err
		}
	}

	c.activeSnapshot = nil
	c.deferredEvents = nil
	return nil
}

func (c *TargetHandler) processEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	// If snapshot collecting is active defer events
	if c.activeSnapshot != nil {
		c.deferredEvents = append(c.deferredEvents, event)
		return nil
	}

	// Apply events
	return c.applyEvent(ctx, event, logger)
}

func (c *TargetHandler) applyEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
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

func (c *TargetHandler) fetchExistingTargets() ([]gnmicv1alpha1.Target, error) {
	var targetList gnmicv1alpha1.TargetList

	err := c.client.List(c.ctx, &targetList,
		client.InNamespace(c.targetSource.Namespace),
		client.MatchingLabels{
			"gnmic.io/source": c.targetSource.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return targetList.Items, nil
}
