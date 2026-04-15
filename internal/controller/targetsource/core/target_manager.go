package core

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NewTargetManager wires a TargetManager instance.
func NewTargetManager(c client.Client, sourceName string, in <-chan []DiscoveryMessage) *TargetManager {
	return &TargetManager{
		client:       c,
		targetsource: sourceName,
		in:           in,
	}
}

// Run is a long‑running loop that receives target snapshots
// and reconciles Target CRs accordingly
func (m *TargetManager) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).
		WithValues("targetSource", m.targetsource)

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
