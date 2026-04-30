package core

type DiscoveryRegistryValue struct {
	Channel        chan<- []DiscoveryMessage
	WebhookEnabled bool
}

type LoaderConfig struct {
	ChunkSize int
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
