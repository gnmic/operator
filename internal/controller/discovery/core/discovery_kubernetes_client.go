package core

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// DiscoveryKubernetesClient is a client which fulfills the StatusUpdater interface
type DiscoveryKubernetesClient struct {
	client       client.Client
	scheme       *runtime.Scheme
	targetSource *gnmicv1alpha1.TargetSource
}

// Returns an instance of DiscoveryKubernetesClient
func NewDiscoveryKubernetesClient(c client.Client, s *runtime.Scheme, ts *gnmicv1alpha1.TargetSource) *DiscoveryKubernetesClient {
	return &DiscoveryKubernetesClient{
		client:       c,
		scheme:       s,
		targetSource: ts,
	}
}

// UpdateStatus takes a StatusUpdate holding Conditions and a pointer referencing the TargetsCount.
// If TargetsCount is set, the LastSync time gets set to metav1.Now().
// Replaces LastTransitionTime of each Condition with metav1.Now().
func (c *DiscoveryKubernetesClient) UpdateStatus(ctx context.Context, update StatusUpdate) error {

	return c.patchStatus(ctx, func(
		ts *gnmicv1alpha1.TargetSource,
	) {
		now := metav1.Now()

		// Update status fields: Replace all Conditions and set TargetsCount and LastSync if pointer != nil
		for i := range update.Conditions {
			update.Conditions[i].LastTransitionTime = now
		}
		ts.Status.Conditions = update.Conditions

		if update.TargetsCount != nil {
			ts.Status.TargetsCount = *update.TargetsCount
			ts.Status.LastSync = now
		}
	})
}

// patchStatus is an internal function to update the Kubernetes object
func (c *DiscoveryKubernetesClient) patchStatus(ctx context.Context, mutate func(*gnmicv1alpha1.TargetSource)) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &gnmicv1alpha1.TargetSource{}
		if err := c.client.Get(ctx, client.ObjectKeyFromObject(c.targetSource), latest); err != nil {
			return err
		}

		patch := client.MergeFrom(latest.DeepCopy())
		mutate(latest)

		return c.client.Status().Patch(ctx, latest, patch)
	})

	return err
}
