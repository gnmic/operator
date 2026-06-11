package core

// A DiscoveryMessage is an event emitted by discovery components (loaders, webhooks, etc.)
// it can be either a DiscoveryEvent (indicating a single target change)
// or a DiscoverySnapshot (indicating a full set of targets)
type DiscoveryMessage interface {
	isDiscoveryMessage()
}

// DiscoveryEvent represents a single target event (apply or delete)
func (DiscoveryEvent) isDiscoveryMessage() {}

// DiscoverySnapshot represents a complete set of discovered targets at a point in time
func (DiscoverySnapshot) isDiscoveryMessage() {}
