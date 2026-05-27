package core

import (
	"context"
)

// StatusUpdater defines the interface for TargetLoaders and MessageProcessor to update the status of the TargetSource
type StatusUpdater interface {
	SetPending(context.Context) error
	SetFetching(context.Context) error
	SetSuccessfulSync(context.Context, int32) error
	SetSyncWithErrors(context.Context, int32, error) error
	SetFetchFailed(context.Context, error) error
}
