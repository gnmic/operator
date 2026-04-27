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

const (
	DELETE EventAction = 0
	APPLY  EventAction = 1
)

type EventAction int

type DiscoveryEvent struct {
	Target DiscoveredTarget
	Event  EventAction
}

func (e EventAction) ToString() string {
	switch e {
	case DELETE:
		return "DELETE"
	case APPLY:
		return "APPLY"
	default:
		return "UNKNOWN"
	}
}

type DiscoverySnapshot struct {
	SnapshotID  string
	ChunkIndex  int
	TotalChunks int
	Targets     []DiscoveredTarget
}
