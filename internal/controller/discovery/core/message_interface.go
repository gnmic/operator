package core

type DiscoveryMessage interface {
	isDiscoveryMessage()
}

func (DiscoveryEvent) isDiscoveryMessage()    {}
func (DiscoverySnapshot) isDiscoveryMessage() {}
