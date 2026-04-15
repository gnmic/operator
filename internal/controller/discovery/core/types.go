package core

// DiscoveredTarget represents a target discovered from an external source
// before it is materialized as a Kubernetes Target CR
type DiscoveredTarget struct {
	Name    string
	Address string
	Labels  map[string]string
}

const (
	DELETE DiscoveryEvent = 0
	CREATE DiscoveryEvent = 1
	UPDATE DiscoveryEvent = 2
)

type DiscoveryEvent int

type DiscoveryMessage struct {
	Target DiscoveredTarget
	Event  DiscoveryEvent
}
