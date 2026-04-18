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
	APPLY  EventAction = 1
)

type EventAction int

type DiscoveryEvent struct {
	Target DiscoveredTarget
	Event  EventAction
}

type DiscoverySnapshot struct {
	Targets     []DiscoveredTarget
	SnapshotID  string
	IsLastChunk bool
}
