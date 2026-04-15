package discovery

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

// TargetManager consumes discovered targets and applies them to Kubernetes.
type TargetManager struct {
	client       client.Client
	scheme       *runtime.Scheme
	targetSource *gnmicv1alpha1.TargetSource
	in           <-chan []core.DiscoveryMessage
}

// NewTargetManager wires a TargetManager instance.
func NewTargetManager(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []core.DiscoveryMessage) *TargetManager {
	return &TargetManager{
		client:       c,
		scheme:       s,
		targetSource: ts,
		in:           in,
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

		case targets := <-m.in:
			logger.Info(
				"received discovered targets",
				"count", len(targets),
			)

			// List existing Target CRs owned by this TargetSource
			// var existing gnmicv1alpha1.TargetList
			// if err := m.client.List(
			// 	ctx,
			// 	&existing,
			// 	client.MatchingLabels{
			// 		"gnmic.dev/targetsource": m.targetsource,
			// 	},
			// ); err != nil {
			// 	return err
			// }

			// TODO: Target Lifecycle Management
			// 1. Compare and determine which Targets to create/update/delete
			// 2. Create/update/delete Target CRs accordingly
			// 3. Update TargetSource status with sync results
		}
	}
}
