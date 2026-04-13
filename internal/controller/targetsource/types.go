package targetsource

import "sigs.k8s.io/controller-runtime/pkg/client"

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

// TargetManager consumes discovered targets and applies them to Kubernetes.
type TargetManager struct {
	client       client.Client
	targetsource string
	in           <-chan []DiscoveryMessage
}
