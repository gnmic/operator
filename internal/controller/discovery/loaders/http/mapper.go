package http

import (
	"strconv"

	"github.com/gnmic/operator/internal/controller/discovery/core"
)

// valueGetter defines the contract for extracting values from a response item
type valueGetter interface {
	GetName() (string, error)
	GetIP() (string, error)
	GetPort() int32
	GetLabels() map[string]string
	GetTargetProfile() string
}

// getGetter selects the extraction strategy based on the spec
// If no ResponseMapping is defined -> use direct mapping
func (l *Loader) getGetter(item map[string]interface{}) valueGetter {
	if l.spec.ResponseMapping == nil {
		return &directGetter{
			item: item,
		}
	}

	return &jsonPathGetter{
		item: item,
		spec: l.spec.ResponseMapping,
	}
}

// mapItem is the mapping entrypoint used by the loader
// It uses the selected valueGetter and produces a DiscoveredTarget
func (l *Loader) mapItem(item map[string]interface{}) (core.DiscoveredTarget, error) {
	getter := l.getGetter(item)

	name, err := getter.GetName()
	if err != nil {
		return core.DiscoveredTarget{}, err
	}

	ip, err := getter.GetIP()
	if err != nil {
		return core.DiscoveredTarget{}, err
	}

	port := getter.GetPort()
	labels := getter.GetLabels()
	targetProfile := getter.GetTargetProfile()

	return core.DiscoveredTarget{
		Name:          name,
		IP:            ip,
		Port:          port,
		Labels:        labels,
		TargetProfile: targetProfile,
	}, nil
}

// extractPort attempts to normalize different JSON types into int32
//
// Supports:
// - float64 (default JSON number type)
// - string ("1234")
//
// Returns 0 if conversion fails (treated as "no port specified").
func extractPort(val interface{}) int32 {
	switch v := val.(type) {
	case float64:
		return int32(v)
	case string:
		p, err := strconv.Atoi(v)
		if err != nil {
			return 0
		}
		return int32(p)
	default:
		return 0
	}
}
