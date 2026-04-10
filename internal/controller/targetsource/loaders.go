package targetsource

import (
	"context"
	"fmt"
	"sync"
)

// Loader defines a pluggable TargetSource loader interface
// Loaders observe external Sources of Truth and emit target snapshots through a channel
type Loader interface {
	// Name returns the unique loader identifier e.g. "http_pull"
	Name() string

	// Start begins discovery and pushes target snapshots into the out channel
	// The loader must stop cleanly when ctx is cancelled
	Start(
		ctx context.Context,
		targetsourceName string,
		out chan<- []DiscoveredTarget,
	) error
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]func() Loader)
)

// Register registers a loader implementation
// It panics on duplicate registrations to fail fast during startup rather than at runtime
func Register(name string, factory func() Loader) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("targetsource loader %q already registered", name))
	}
	registry[name] = factory
}

// NewLoader creates a loader by name
func NewLoader(name string) (Loader, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown targetsource loader: %q", name)
	}
	return factory(), nil
}
