package http

import (
	"fmt"
)

// directGetter extracts values via direct map access
// Example input:
//
//	{
//	  "name": "router1",
//	  "ip": "10.0.0.1",
//	  "port": 57400,
//	  "labels": { ... },
//	  "targetProfile": "profile1"
//	}
type directGetter struct {
	item map[string]interface{}
}

// GetName extracts the "name" field directly
func (g *directGetter) GetName() (string, error) {
	val, ok := g.item["name"].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("name must be a non-empty string")
	}
	return val, nil
}

// GetIP extracts the "ip" field directly.
func (g *directGetter) GetIP() (string, error) {
	val, ok := g.item["ip"].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("ip must be a non-empty string")
	}
	return val, nil
}

// GetPort extracts and normalizes the "port" field
//
// Behavior:
// - supports int, float64, string
// - returns 0 if value is missing or invalid
func (g *directGetter) GetPort() int32 {
	if val, ok := g.item["port"]; ok {
		return extractPort(val)
	}
	return 0
}

// GetLabels extracts labels from the "labels" field
// Expected format:
//
//	"labels": {
//	  "key": "value"
//	}
//
// Non-string values are converted to string
func (g *directGetter) GetLabels() map[string]string {
	labels := make(map[string]string)

	if val, ok := g.item["labels"]; ok {
		if m, ok := val.(map[string]interface{}); ok {
			for k, v := range m {
				labels[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	return labels
}

// GetTargetProfile extracts the "targetProfile" field directly
//
// Behavior:
// - returns "" if value is missing or invalid
func (g *directGetter) GetTargetProfile() string {
	if val, ok := g.item["targetProfile"].(string); ok {
		return val
	}
	return ""
}
