package discovery

import (
	"fmt"
	"sync"
)

// Registry is a thread-safe key -> channel registry
// K must be comparable so it can be used as a map key
// DO NOT USE a pointer type as K
type Registry[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

func NewRegistry[K comparable, V any]() *Registry[K, V] {
	return &Registry[K, V]{m: make(map[K]V)}
}

func (r *Registry[K, V]) Register(key K, value V) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[key]; exists {
		return fmt.Errorf("already registered: %v", key)
	}
	r.m[key] = value
	return nil
}

func (r *Registry[K, V]) Unregister(key K) {
	r.mu.Lock()
	delete(r.m, key)
	r.mu.Unlock()
}

func (r *Registry[K, V]) Get(key K) (V, bool) {
	r.mu.RLock()
	value, ok := r.m[key]
	r.mu.RUnlock()
	return value, ok
}
