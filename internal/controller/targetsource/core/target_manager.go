package core

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

// Run is a long‑running loop that receives target event messages
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
				switch msg.Event {
				case DELETE:
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

				case CREATE:
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

				case UPDATE:
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

				default:
					logger.Error(nil, "unknown discovery event received")
				}
			}
		}
	}
}
