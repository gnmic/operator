package core

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeReady       = "Ready"
	ConditionTypeReconciling = "Reconciling"
	ConditionTypeDegraded    = "Degraded"
	ConditionTypeStalled     = "Stalled"

	ReasonWaitingForSync Reason = "WaitingForSync"
	ReasonSyncStarted    Reason = "SyncStarted"
	ReasonSyncSucceeded  Reason = "SyncSucceeded"
	ReasonSyncCompleted  Reason = "SyncCompleted"
	ReasonSyncWithErrors Reason = "SyncSucceededWithErrors"
	ReasonSyncFailed     Reason = "SyncFailed"
)

type Reason string

type StatusUpdate struct {
	Conditions   []metav1.Condition
	TargetsCount *int32
}

// StatusUpdater defines the interface for TargetLoaders and MessageProcessor to update the status of the TargetSource
type StatusUpdater interface {
	UpdateStatus(context.Context, StatusUpdate) error
}
