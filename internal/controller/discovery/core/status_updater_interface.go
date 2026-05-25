package core

import (
	"context"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

type StatusUpdate struct {
	SyncStatus   gnmicv1alpha1.TargetSourceSyncStatus
	TargetsCount int32
	Err          error
}

// StatusUpdater defines the interface for TargetLoaders and MessageProcessor to update the status of the TargetSource
type StatusUpdater interface {
	UpdateStatus(context.Context, StatusUpdate) error
}
