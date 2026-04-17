package discovery

import (
	"context"
	"fmt"

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
		WithValues("targetSource", m.targetSource)

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
						target := &gnmicv1alpha1.Target{
							ObjectMeta: metav1.ObjectMeta{
								Name:      msg.Target.Name,
								Namespace: m.targetSource.ObjectMeta.Namespace,
								Labels: map[string]string{
									"gnmic.io/source": m.targetSource.ObjectMeta.Name,
								},
							},
							Spec: gnmicv1alpha1.TargetSpec{
								Address: msg.Target.Address,
								Profile: "default",
							},
						}
						err := controllerutil.SetControllerReference(m.targetSource, target, m.scheme)
						if err != nil {
							logger.Error(err, "error setting the owner reference")
						}

						err = m.client.Create(ctx, target)
						if err != nil {
							logger.Error(err, "error creating target object")
						}
						logger.Info(fmt.Sprintf("created new target object %s/%s", target.ObjectMeta.Namespace, target.ObjectMeta.Name))

					case core.UPDATE:
						logger.Info("Would update target", "name", msg.Target.Name, "address", msg.Target.Address, "labels", msg.Target.Labels)
						existing := &gnmicv1alpha1.Target{}
						newSpec := gnmicv1alpha1.TargetSpec{
							Address: msg.Target.Address,
							Profile: "default",
						}

						err := m.client.Get(ctx, types.NamespacedName{
							Name:      msg.Target.Name,
							Namespace: m.targetSource.Namespace,
						}, existing)
						if err != nil {
							logger.Error(err, "error fetching existing target object")
						}

						existing.Spec = newSpec

						err = m.client.Update(ctx, existing)
						if err != nil {
							logger.Error(err, "error updating object")
						}
						logger.Info(fmt.Sprintf("updated existing target object %s/%s", existing.ObjectMeta.Namespace, existing.ObjectMeta.Name))

					case core.DELETE:
						logger.Info("Would delete target", "name", msg.Target.Name)
						existing := &gnmicv1alpha1.Target{}
						err := m.client.Get(ctx, types.NamespacedName{
							Name:      msg.Target.Name,
							Namespace: m.targetSource.Namespace,
						}, existing)
						if err != nil {
							logger.Error(err, "error fetching existing target object")
						}

						err = m.client.Delete(ctx, existing)
						if err != nil {
							logger.Error(err, "error deleting the object")
						}
						logger.Info(fmt.Sprintf("deleted target object %s/%s", m.targetSource.Namespace, msg.Target.Name))
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
