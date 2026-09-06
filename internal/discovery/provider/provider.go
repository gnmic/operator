// Package provider reads devices from external systems.
//
// A provider is a pure function of its spec plus the remote system: it does
// not talk to Kubernetes, does not create resources, and does not update
// status. Fetch runs, returns the complete set of devices the source knows
// about, and is done. Everything after that, naming, pruning, status, is the
// controller's job.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// Device is one device as the source describes it, before any naming,
// defaulting or sanitising.
type Device struct {
	Name    string
	Address string
	Port    int32
	Profile string
	Labels  map[string]string
}

// Failure is one device the provider could not turn into a Device, with a
// short reason. The controller counts and samples these in status so a typo
// in a mapping is visible rather than a quietly shorter list.
type Failure struct {
	Name   string
	Reason string
}

// Result is what one Fetch returns.
type Result struct {
	// Devices is the full set the source returned.
	Devices []Device
	// Invalid lists items that could not be read as a device.
	Invalid []Failure
	// Truncated reports that the source could not be read completely, for
	// example when a paginated API hit MaxPages. A truncated result never
	// prunes, and is reported as such. A provider must not return a
	// partial set with Truncated false and a nil error.
	Truncated bool
}

// Resolver reads Secrets and ConfigMaps for a provider. Providers never hold
// a Kubernetes client. A missing object or key must be returned wrapped with
// Spec, since retrying cannot fix it.
type Resolver interface {
	SecretData(ctx context.Context, namespace, name string) (map[string][]byte, error)
	SecretKey(ctx context.Context, namespace, name, key string) ([]byte, error)
	ConfigMapData(ctx context.Context, namespace, name string) (map[string][]byte, error)
	ConfigMapKey(ctx context.Context, namespace, name, key string) ([]byte, error)
}

// Request is everything a provider is allowed to see.
type Request struct {
	// Source is the validated source block.
	Source gnmicv1alpha1.SourceSpec
	// Namespace is the TargetSource namespace. All Secret and ConfigMap
	// references resolve here.
	Namespace string
	// Objects resolves Secret and ConfigMap references.
	Objects Resolver
}

// Provider reads devices from one kind of external system.
type Provider interface {
	// Type is the discriminator value this provider serves.
	Type() gnmicv1alpha1.SourceType
	// Fetch returns every device the source currently knows about. It must
	// respect the context deadline.
	Fetch(ctx context.Context, req Request) (Result, error)
}

// SpecError marks a failure a retry cannot fix: the spec, or an object it
// references, has to change first. The controller reports it as Stalled and
// backs off to the cap instead of retrying at the normal interval.
type SpecError struct {
	Err error
}

func (e *SpecError) Error() string { return e.Err.Error() }
func (e *SpecError) Unwrap() error { return e.Err }

// Spec wraps err as a SpecError. A nil err stays nil.
func Spec(err error) error {
	if err == nil {
		return nil
	}
	return &SpecError{Err: err}
}

// Specf formats a SpecError.
func Specf(format string, args ...any) error {
	return &SpecError{Err: fmt.Errorf(format, args...)}
}

// IsSpecError reports whether err, or anything it wraps, is a SpecError.
func IsSpecError(err error) bool {
	var se *SpecError
	return errors.As(err, &se)
}

var (
	registryMu sync.RWMutex
	registry   = map[gnmicv1alpha1.SourceType]Provider{}
)

// Register adds a provider for its Type. Registering the same type twice is a
// programming error and panics at init.
func Register(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[p.Type()]; dup {
		panic(fmt.Sprintf("provider: duplicate registration for %q", p.Type()))
	}
	registry[p.Type()] = p
}

// Lookup returns the provider for a source type.
func Lookup(t gnmicv1alpha1.SourceType) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[t]
	return p, ok
}

// Types lists the registered source types, sorted.
func Types() []gnmicv1alpha1.SourceType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]gnmicv1alpha1.SourceType, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
