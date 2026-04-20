package discovery

import (
	"context"
	"maps"

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
		WithValues(
			"targetSource", m.targetSource.ObjectMeta.Name,
			"namespace", m.targetSource.ObjectMeta.Namespace,
		)

	logger.Info("target manager started")

	for {
		select {
		case <-ctx.Done():
			logger.Info("target manager stopped")
			return nil

		case messages := <-m.in:
			logger.Info(
				"received discovered targets",
				"count", len(messages),
			)

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
						m.processSnapshot(ctx, msg.SnapshotID, logger)
					}

				case core.DiscoveryEvent:
					// Process individual event-driven update
					logger.Info("received discovery event",
						"name", msg.Target.Name,
					)

					switch msg.Event {
					case core.DELETE:
						err := m.deleteTarget(ctx, msg.Target.Name)
						if err != nil {
							logger.Error(err, "error deleting target object",
								"namespace", m.targetSource.ObjectMeta.Namespace,
								"name", msg.Target.Name,
							)
						} else {
							logger.Info("deleted target object",
								"namespace", m.targetSource.ObjectMeta.Namespace,
								"name", msg.Target.Name,
							)
						}

					case core.APPLY:
						err := m.applyTarget(ctx, msg.Target.Name, msg.Target.Address)
						if err != nil {
							logger.Error(err, "error applying target object",
								"namespace", m.targetSource.ObjectMeta.Namespace,
								"name", msg.Target.Name,
							)

						} else {
							logger.Info("applied target object",
								"namespace", m.targetSource.ObjectMeta.Namespace,
								"name", msg.Target.Name,
							)
						}
					}
				}
			}
		}
	}
}

// processSnapshot takes a complete snapshot of discovered targets and reconciles Target CRs accordingly
func (m *TargetManager) processSnapshot(ctx context.Context, snapshotID string, logger logr.Logger) {
	targets := m.collected[snapshotID]
	delete(m.collected, snapshotID)

	logger.Info("processing full snapshot",
		"id", snapshotID,
		"numOfTargets", len(targets),
	)

	existing, err := FetchExistingTargets(ctx, m.client, *m.targetSource)
	if err != nil {
		logger.Error(err, "error fetching existing targets")
	} else {
		logger.Info("fetched existing targets",
			"numOfTargets", len(existing),
		)
	}

	diff := BuildDiff(existing, targets)

	logger.Info("built diff",
		"numOfTargetsToApply", len(diff.ToApply),
		"numOfTargetsToDelete", len(diff.ToDelete),
	)

	for _, t := range diff.ToDelete {
		err := m.deleteTarget(ctx, t.Name)
		if err != nil {
			logger.Error(err, "error deleting target object",
				"namespace", m.targetSource.ObjectMeta.Namespace,
				"name", t.Name,
			)
		} else {
			logger.Info("deleted target object",
				"namespace", m.targetSource.ObjectMeta.Namespace,
				"name", t.Name,
			)
		}
	}

	for _, t := range diff.ToApply {
		err := m.applyTarget(ctx, t.Name, t.Address)
		if err != nil {
			logger.Error(err, "error applying target object",
				"namespace", m.targetSource.ObjectMeta.Namespace,
				"name", t.Name,
			)
		} else {
			logger.Info("applied target object",
				"namespace", m.targetSource.ObjectMeta.Namespace,
				"name", t.Name,
			)
		}

	}

	logger.Info("end of snapshot processing")
}

func (m *TargetManager) applyTarget(ctx context.Context, name string, address string) error {
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.targetSource.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, m.client, target, func() error {
		labels := map[string]string{
			"gnmic.io/source": m.targetSource.Name,
		}

		maps.Copy(labels, m.targetSource.Spec.TargetLabels)

		target.Labels = labels

		target.Spec = gnmicv1alpha1.TargetSpec{
			Address: address,
			Profile: m.targetSource.Spec.TargetProfile,
		}

		return controllerutil.SetControllerReference(m.targetSource, target, m.scheme)
	})

	return err
}

func (m *TargetManager) deleteTarget(ctx context.Context, name string) error {
	existing := &gnmicv1alpha1.Target{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: m.targetSource.Namespace,
	}, existing)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	err = m.client.Delete(ctx, existing)
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}
