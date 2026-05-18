package http

import (
	"fmt"

	"github.com/PaesslerAG/jsonpath"
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// jsonPathGetter extracts values using JSONPath expressions defined in the CR
// Example mapping:
//
//	name: "$.hostname"
//	ip: "$.ip"
//	port: "$.port"
//	labels:
//	  rack: "$.meta.rack"
type jsonPathGetter struct {
	item map[string]interface{}
	spec *gnmicv1alpha1.ResponseMappingSpec
}

// helper function to execute JSONPath queries
func (g *jsonPathGetter) get(expr string) (interface{}, error) {
	return jsonpath.Get(expr, g.item)
}

// GetName extracts the target name using JSONPath
func (g *jsonPathGetter) GetName() (string, error) {
	val, err := g.get(g.spec.Name)
	if err != nil {
		return "", fmt.Errorf("name mapping failed: %w", err)
	}

	str, ok := val.(string)
	if !ok || str == "" {
		return "", fmt.Errorf("name must be a non-empty string")
	}

	return str, nil
}

// GetIP extracts the IP using JSONPath
func (g *jsonPathGetter) GetIP() (string, error) {
	val, err := g.get(g.spec.IP)
	if err != nil {
		return "", fmt.Errorf("IP mapping failed: %w", err)
	}

	str, ok := val.(string)
	if !ok || str == "" {
		return "", fmt.Errorf("IP must be a non-empty string")
	}

	return str, nil
}

// GetPort extracts the port using JSONPath
//
// Behavior:
// - returns 0 if no port mapping defined
// - returns 0 if extraction fails or value invalid
func (g *jsonPathGetter) GetPort() int32 {
	if g.spec.Port == "" {
		return 0
	}

	val, err := g.get(g.spec.Port)
	if err != nil {
		return 0
	}

	return extractPort(val)
}

// GetLabels extracts labels using JSONPath expressions defined per label key
func (g *jsonPathGetter) GetLabels() map[string]string {
	labels := make(map[string]string)

	for key, expr := range g.spec.Labels {
		if val, err := g.get(expr); err == nil {
			labels[key] = fmt.Sprintf("%v", val)
		}
	}

	return labels
}

// GetTargetProfile extracts the target profile using JSONPath
//
// Behavior:
// - returns "" if no target profile mapping defined
// - returns "" if extraction fails or value invalid
func (g *jsonPathGetter) GetTargetProfile() string {
	val, err := g.get(g.spec.TargetProfile)
	if err != nil {
		return ""
	}

	str, ok := val.(string)
	if !ok {
		return ""
	}

	return str
}
