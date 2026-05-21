package core

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
)

// Loader defines a pluggable TargetSource loader interface
// Loaders observe external Sources of Truth and emit target snapshots through a channel
type Loader interface {
	// Name returns the unique loader identifier e.g. "pull"
	Name() string

	// Run begins discovery and pushes target snapshots or events into the out channel
	// The loader must stop cleanly when ctx is canceled
	Run(ctx context.Context, out chan<- []DiscoveryMessage) error

	// SecretRefs returns a list of secrets which should be watched by the TargetSource controller
	SecretRefs() []types.NamespacedName
}
