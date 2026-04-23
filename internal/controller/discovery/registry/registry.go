package registry

import (
	"fmt"
	"sync"
)

/* USAGE

// create registry once in main.go
discoveryReg := discovery.NewRegistry[[]core.DiscoveryMessage]()

// inside targetsource controller, when starting discovery pipeline:
key := fmt.Sprintf("%s/%s", spec.Namespace, targetsourceName)
if err := discoveryReg.Register(key, out); err != nil {
  logger.Error(err, "could not register loader")
  return err
}
defer discoveryReg.Unregister(key)

// CHECK REGISTRY
ch, ok := discoveryReg.Get(ns + "/" + ts)
if !ok {
  http.Error(w, "no loader for targetsource", http.StatusNotFound)
  return
}
// then deliver payload to ch
*/

// Registry is a thread-safe map: key -> channel of T.
type Registry[T any] struct {
	mu sync.RWMutex
	m  map[string]chan<- T
}

func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{m: make(map[string]chan<- T)}
}

func (r *Registry[T]) Register(key string, ch chan<- T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[key]; exists {
		return fmt.Errorf("already registered: %s", key)
	}
	r.m[key] = ch
	return nil
}

func (r *Registry[T]) Unregister(key string) {
	r.mu.Lock()
	delete(r.m, key)
	r.mu.Unlock()
}

func (r *Registry[T]) Get(key string) (chan<- T, bool) {
	r.mu.RLock()
	ch, ok := r.m[key]
	r.mu.RUnlock()
	return ch, ok
}
