package discovery

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

// TargetApplier consumes discovered targets and applies them to Kubernetes
type TargetApplier struct {
	client         client.Client
	scheme         *runtime.Scheme
	targetSource   *gnmicv1alpha1.TargetSource
	in             <-chan []core.DiscoveryMessage
	queue          []core.DiscoveryMessage
	activeSnapshot *snapshotBuffer
	// Events are deferred while snapshot is in progress
	defferedEvents []core.DiscoveryEvent
}

// NewTargetApplier wires a TargetApplier instance
func NewTargetApplier(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *TargetApplier {
	return &TargetApplier{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (a *TargetApplier) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).
		WithValues(
			"name", a.targetSource.Name,
			"namespace", a.targetSource.Namespace,
		)
	logger.Info("target applier started")

	for ctx.Err() == nil {
		select {
		case batch, ok := <-a.in:
			if !ok {
				// Channel closed, pipeline is shutting down
				logger.Info("input channel closed, stopping target applier")
				return nil
			}
			a.queue = append(a.queue, batch...)

		case <-ctx.Done():
			logger.Info("context canceled, stopping target applier")
			return nil
		}

		for len(a.queue) > 0 {
			if ctx.Err() != nil {
				return nil // why return nil?
			}

			msg := a.queue[0]
			a.queue = a.queue[1:]

			if err := a.processMessage(ctx, msg, logger); err != nil {
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

func (a *TargetApplier) processMessage(ctx context.Context, message core.DiscoveryMessage, logger logr.Logger) error {
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

		for i := range msg.Targets {
			msg.Targets[i] = a.normalizeTarget(msg.Targets[i])
		}

		return a.processSnapshot(ctx, msg, logger)

	case core.DiscoveryEvent:
		// Process individual event-driven update
		logger.Info(
			"received discovery event",
			"target", msg.Target.Name,
		)

		msg.Target = a.normalizeTarget(msg.Target)
		return a.processEvent(ctx, msg, logger)

	default:
		return fmt.Errorf("unknonw discovery message type %T", msg)
	}
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (a *TargetApplier) processSnapshot(ctx context.Context, chunk core.DiscoverySnapshot, logger logr.Logger) error {
	if a.activeSnapshot == nil {
		a.startNewSnapshot(chunk, logger)
		return nil
	}

	snapshot := a.activeSnapshot
	// Check if a new snapshot arrived
	if snapshot.snapshotID != chunk.SnapshotID {
		// If current snapshot is complete apply it first
		if snapshot.complete {
			if err := a.applySnapshot(ctx, snapshot, logger); err != nil {
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
		a.startNewSnapshot(chunk, logger)
		return nil
	}

	return a.collectSnapshot(chunk, logger)
}

func (a *TargetApplier) startNewSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) {
	a.activeSnapshot = &snapshotBuffer{
		snapshotID:  chunk.SnapshotID,
		totalChunks: chunk.TotalChunks,
		received:    make(map[int][]core.DiscoveredTarget),
		complete:    false,
	}
	// Delete buffered events that will be current with new snapshot
	a.defferedEvents = nil

	a.collectSnapshot(chunk, logger)
}

func (a *TargetApplier) collectSnapshot(chunk core.DiscoverySnapshot, logger logr.Logger) error {
	snapshot := a.activeSnapshot

	if chunk.TotalChunks != snapshot.totalChunks {
		logger.Error(nil, "snapshot totalChunks mismatch", "snapshotID", snapshot.snapshotID)
	}
	if chunk.ChunkIndex < 0 || chunk.ChunkIndex >= snapshot.totalChunks {
		logger.Error(nil, "snapshot chunk index out of range", "index", chunk.ChunkIndex)
		a.activeSnapshot = nil
		return nil
	}
	if _, exists := snapshot.received[chunk.ChunkIndex]; exists {
		logger.Error(nil, "duplicate snapshot chunk", "index", chunk.ChunkIndex)
		a.activeSnapshot = nil
		return nil
	}

	snapshot.received[chunk.ChunkIndex] = chunk.Targets

	if len(snapshot.received) == snapshot.totalChunks {
		snapshot.complete = true
	}

	return nil
}

func (a *TargetApplier) processEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	// If snapshot collecting is active defer events
	if a.activeSnapshot != nil {
		a.defferedEvents = append(a.defferedEvents, event)
		return nil
	}

	// Apply events
	return a.applyEvent(ctx, event, logger)
}

func (a *TargetApplier) applySnapshot(ctx context.Context, snapshot *snapshotBuffer, logger logr.Logger) error {
	select {
	case <-ctx.Done():
		a.activeSnapshot = nil
		return nil
	default:
	}

	var allTargets []core.DiscoveredTarget
	for i := 0; i < snapshot.totalChunks; i++ {
		select {
		case <-ctx.Done():
			a.activeSnapshot = nil
			return nil
		default:
		}

		chunk, ok := snapshot.received[i]
		if !ok {
			logger.Error(nil, "missing snapshot chunk", "index", i)
			a.activeSnapshot = nil
			return nil
		}
		allTargets = append(allTargets, chunk...)
	}

	logger.Info(
		"applying snapshot",
		"snapshotID", snapshot.snapshotID,
		"targetCount", len(allTargets),
	)

	existing, err := FetchExistingTargets(ctx, a.client, *a.targetSource)
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
		case core.APPLY:
			nApply++
		case core.DELETE:
			nDelete++
		}
	}

	logger.Info("generated events",
		"numOfApply", nApply,
		"numOfDelete", nDelete,
	)

	for _, e := range events {
		a.processEvent(ctx, e, logger)
	}

	// Replay deffered events
	for _, event := range a.defferedEvents {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := a.applyEvent(ctx, event, logger); err != nil {
			return err
		}
	}

	a.activeSnapshot = nil
	a.defferedEvents = nil

	return nil
}

func (a *TargetApplier) applyEvent(ctx context.Context, event core.DiscoveryEvent, logger logr.Logger) error {
	switch event.Event {
	case core.DELETE:
		if err := a.deleteTarget(ctx, event.Target.Name); err != nil {
			logger.Error(err, "error deleting target",
				"targetName", event.Target.Name,
			)
		} else {
			logger.Info("deleted target object",
				"name", event.Target.Name,
			)
		}
	case core.APPLY:
		target := generateTargetResource(event.Target, a.targetSource)

		if err := a.applyTarget(ctx, target); err != nil {
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

func (a *TargetApplier) applyTarget(ctx context.Context, desired *gnmicv1alpha1.Target) error {
	existing := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, a.client, existing, func() error {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels

		return controllerutil.SetControllerReference(a.targetSource, existing, a.scheme)
	})

	return err
}

func (a *TargetApplier) deleteTarget(ctx context.Context, name string) error {
	existing := &gnmicv1alpha1.Target{}

	err := a.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: a.targetSource.Namespace,
	}, existing)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	err = a.client.Delete(ctx, existing)
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (a *TargetApplier) normalizeTarget(t core.DiscoveredTarget) core.DiscoveredTarget {
	t.Name = a.targetSource.Name + "-" + t.Name
	return t
}
