package targetsource

// DiscoveredTarget represents a target discovered from an external source
// before it is materialized as a Kubernetes Target CR.
type DiscoveredTarget struct {
	Name    string
	Address string
	Labels  map[string]string
}
