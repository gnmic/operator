package core

import (
	"context"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// Loader defines a pluggable TargetSource loader interface
// Loaders observe external Sources of Truth and emit target snapshots through a channel
type Loader interface {
	// Name returns the unique loader identifier e.g. "http_pull"
	Name() string

	// Start begins discovery and pushes target snapshots or events into the out channel
	// The loader must stop cleanly when ctx is cancelled
	Start(
		ctx context.Context,
		targetsourceName string,
		spec gnmicv1alpha1.TargetSourceSpec,
		out chan<- []DiscoveryMessage,
	) error
}
