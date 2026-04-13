package targetsource

import (
	"context"
	"fmt"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NewTargetManager wires a TargetManager instance.
func NewTargetManager(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource, in <-chan []DiscoveryMessage) *TargetManager {
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

		case messages := <-m.in:
			logger.Info(
				"received discovered targets",
				"count", len(messages),
			)

			for _, msg := range messages {
				if msg.Event == CREATE {
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
				}
			}

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
