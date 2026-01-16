package gnmic

import (
	"fmt"

	gnmicv1alpha1 "github.com/gnmic/gnmic-operator/api/v1alpha1"
	"gopkg.in/yaml.v2"
)

// buildProcessorConfig creates a gNMIc processor config map from a ProcessorSpec
func buildProcessorConfig(spec *gnmicv1alpha1.ProcessorSpec) (map[string]any, error) {
	config := make(map[string]any)

	// parse the config YAML/JSON
	if spec.Config.Raw != nil {
		if err := yaml.Unmarshal(spec.Config.Raw, &config); err != nil {
			return nil, err
		}
	}
	result := map[string]any{
		spec.Type: config,
	}
	v := convert(result)
	switch v := v.(type) {
	case map[string]any:
		return v, nil
	}
	return nil, fmt.Errorf("invalid processor config type: %T", v)
}

// normalize map[any]any to map[string]any recursively
func convert(v any) any {
	switch v := v.(type) {
	case map[string]any:
		newMap := make(map[string]any)
		for k, v := range v {
			newMap[k] = convert(v)
		}
		return newMap
	case map[any]any:
		newMap := make(map[string]any)
		for k, v := range v {
			key, ok := k.(string)
			if !ok {
				continue
			}
			newMap[key] = convert(v)
		}
		return newMap
	case []any:
		newSlice := make([]any, len(v))
		for i, v := range v {
			newSlice[i] = convert(v)
		}
		return newSlice
	default:
		return v
	}
}
