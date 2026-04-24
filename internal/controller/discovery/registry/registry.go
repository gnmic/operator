package registry

import (
	"fmt"
	"sync"
)

// Registry is a thread-safe key -> channel registry
// K must be comparable so it can be used as a map key
type Registry[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]chan<- V
}

func NewRegistry[K comparable, V any]() *Registry[K, V] {
	return &Registry[K, V]{m: make(map[K]chan<- V)}
}

func (r *Registry[K, V]) Register(key K, ch chan<- V) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[key]; exists {
		return fmt.Errorf("already registered: %s", key)
	}
	r.m[key] = ch
	return nil
}

func (r *Registry[K, V]) Unregister(key K) {
	r.mu.Lock()
	delete(r.m, key)
	r.mu.Unlock()
}

func (r *Registry[K, V]) Get(key K) (chan<- V, bool) {
	r.mu.RLock()
	ch, ok := r.m[key]
	r.mu.RUnlock()
	return ch, ok
}
