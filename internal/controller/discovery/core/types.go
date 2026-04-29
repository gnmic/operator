package core

type LoaderConfig struct {
	ChunkSize int
}

// DiscoveredTarget represents a target discovered from an external source
// before it is materialized as a Kubernetes Target CR
type DiscoveredTarget struct {
	Name    string
	Address string
	Labels  map[string]string
}

type EventAction int

const (
	DELETE EventAction = 0
	CREATE EventAction = 1
	UPDATE EventAction = 2
)

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
