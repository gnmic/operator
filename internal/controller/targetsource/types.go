package targetsource

import "sigs.k8s.io/controller-runtime/pkg/client"

// DiscoveredTarget represents a target discovered from an external source
// before it is materialized as a Kubernetes Target CR
type DiscoveredTarget struct {
	Name    string
	Address string
	Labels  map[string]string
}

// TargetManager consumes discovered targets and applies them to Kubernetes.
type TargetManager struct {
	client       client.Client
	targetsource string
	in           <-chan []DiscoveredTarget
}
