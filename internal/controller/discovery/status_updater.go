package discovery

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

const (
	ConditionReady       = "Ready"
	ConditionReconciling = "Reconciling"
	ConditionDegraded    = "Degraded"
	ConditionStalled     = "Stalled"

	ReasonWaitingForSync = "WaitingForSync"
	ReasonSyncStarted    = "SyncStarted"
	ReasonSyncSucceeded  = "SyncSucceeded"
	ReasonSyncCompleted  = "SyncCompleted"
	ReasonSyncWithErrors = "SyncSucceededWithErrors"
	ReasonSyncFailed     = "SyncFailed"
)

type TargetSourceStatusUpdater struct {
	client       client.Client
	targetSource *gnmicv1alpha1.TargetSource
}

func NewTargetSourceStatusUpdater(c client.Client, ts *gnmicv1alpha1.TargetSource) *TargetSourceStatusUpdater {
	return &TargetSourceStatusUpdater{
		client:       c,
		targetSource: ts,
	}
}

func (u *TargetSourceStatusUpdater) SetPending(ctx context.Context) error {

	return u.patchStatus(ctx, func(
		ts *gnmicv1alpha1.TargetSource,
	) {
		now := metav1.Now()

		// Ready=True
		meta.SetStatusCondition(&ts.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonWaitingForSync,
			Message:            "Waiting for the TargetLoader to start the sync",
			LastTransitionTime: now,
		})

		// Remove other status conditions
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReconciling,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionStalled,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionDegraded,
		)
	})
}

func (u *TargetSourceStatusUpdater) SetFetching(ctx context.Context) error {

	return u.patchStatus(ctx, func(
		ts *gnmicv1alpha1.TargetSource,
	) {
		now := metav1.Now()

		// Reconciling=True
		meta.SetStatusCondition(&ts.Status.Conditions, metav1.Condition{
			Type:               ConditionReconciling,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonSyncStarted,
			Message:            "Started fetching targets",
			LastTransitionTime: now,
		})

		// Remove other status conditions
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReady,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionStalled,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionDegraded,
		)
	})
}

func (u *TargetSourceStatusUpdater) SetFetchFailed(ctx context.Context, err error) error {

	return u.patchStatus(ctx, func(
		ts *gnmicv1alpha1.TargetSource,
	) {
		now := metav1.Now()

		// Reconciling=True
		meta.SetStatusCondition(&ts.Status.Conditions, metav1.Condition{
			Type:               ConditionStalled,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonSyncFailed,
			Message:            err.Error(),
			LastTransitionTime: now,
		})

		// Remove other status conditions
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReady,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReconciling,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionDegraded,
		)
	})
}

func (u *TargetSourceStatusUpdater) SetSuccessfulSync(ctx context.Context, targetsCount int32) error {

	return u.patchStatus(ctx, func(
		ts *gnmicv1alpha1.TargetSource,
	) {
		now := metav1.Now()

		// Ready=True
		meta.SetStatusCondition(&ts.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonSyncSucceeded,
			Message:            "Targets synchronized successfully",
			LastTransitionTime: now,
		})

		// Remove other status conditions
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReconciling,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionStalled,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionDegraded,
		)

		// Update status fields
		ts.Status.TargetsCount = targetsCount
		ts.Status.LastSync = now
	})
}

func (u *TargetSourceStatusUpdater) SetSyncWithErrors(ctx context.Context, targetsCount int32, err error) error {

	return u.patchStatus(ctx, func(
		ts *gnmicv1alpha1.TargetSource,
	) {
		now := metav1.Now()

		// Ready=True
		meta.SetStatusCondition(&ts.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonSyncSucceeded,
			Message:            "Targets synchronized",
			LastTransitionTime: now,
		})

		// Degraded=True
		meta.SetStatusCondition(&ts.Status.Conditions, metav1.Condition{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonSyncWithErrors,
			Message:            err.Error(),
			LastTransitionTime: now,
		})

		// Remove other status conditions
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReady,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionReconciling,
		)
		meta.RemoveStatusCondition(
			&ts.Status.Conditions,
			ConditionStalled,
		)

		// Update status fields
		ts.Status.TargetsCount = targetsCount
		ts.Status.LastSync = now
	})
}

func (u *TargetSourceStatusUpdater) patchStatus(ctx context.Context, mutate func(*gnmicv1alpha1.TargetSource)) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &gnmicv1alpha1.TargetSource{}
		if err := u.client.Get(ctx, client.ObjectKeyFromObject(u.targetSource), latest); err != nil {
			return err
		}

		patch := client.MergeFrom(latest.DeepCopy())
		mutate(latest)

		return u.client.Status().Patch(ctx, latest, patch)
	})

	return err
}
