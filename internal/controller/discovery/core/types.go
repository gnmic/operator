package core

import (
	"context"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

// DiscoveryRegistryValue represents the controller-owned runtime state
// with its configuration for a single TargetSource
type DiscoveryRegistryValue struct {
	// Channel is the outbound communication channel used by discovery
	// components (loaders, webhooks, etc.) to emit discovery messages
	Channel chan<- []DiscoveryMessage
	// Stop cancels the discovery context associated with this registry entry
	Stop context.CancelFunc

	LoaderConfig *LoaderConfig
}

type LoaderConfig struct {
	TargetsourceNN types.NamespacedName
	Spec           *gnmicv1alpha1.TargetSourceSpec
	ChunkSize      int
	AcceptPush     bool
}

// EventAction represents the type of a discovery event
type EventAction int

const (
	// EventDelete indicates that a target should be removed
	EventDelete EventAction = iota
	// EventApply indicates that a target should be applied (created or updated)
	EventApply
)

// DiscoveredTarget represents a target discovered from an external source
// before it is materialized as a Kubernetes Target CR
type DiscoveredTarget struct {
	Name    string
	Address string
	Labels  map[string]string
}

type DiscoveryEvent struct {
	Target DiscoveredTarget
	Event  EventAction
}

type DiscoverySnapshot struct {
	SnapshotID  string
	ChunkIndex  int
	TotalChunks int
	Targets     []DiscoveredTarget
}
