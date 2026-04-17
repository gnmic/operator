package core

// DiscoveredTarget represents a target discovered from an external source
// before it is materialized as a Kubernetes Target CR
type DiscoveredTarget struct {
	Name    string
	Address string
	Labels  map[string]string
}

const (
	DELETE EventAction = 0
	CREATE EventAction = 1
	UPDATE EventAction = 2
)

type EventAction int

type DiscoveryEvent struct {
	Target DiscoveredTarget
	Event  EventAction
}

type DiscoverySnapshot struct {
	Target      []DiscoveredTarget
	Event       EventAction
	SnapshotID  string
	IsLastChunk bool
}
