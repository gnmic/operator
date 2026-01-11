package gnmic

import (
	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	"gopkg.in/yaml.v2"
)

// buildInputConfig creates a gNMIc input config map from an InputSpec
// outputs is the list of output names this input should send data to
func buildInputConfig(spec *gnmicv1alpha1.InputSpec, outputs []string, processors []string) (map[string]any, error) {
	config := make(map[string]any)

	// parse the config YAML/JSON
	if spec.Config.Raw != nil {
		if err := yaml.Unmarshal(spec.Config.Raw, &config); err != nil {
			return nil, err
		}
	}

	// set the type
	config["type"] = spec.Type

	// set outputs if provided
	if len(outputs) > 0 {
		config["outputs"] = outputs
	}

	// set event-processors if provided
	if len(processors) > 0 {
		config["event-processors"] = processors
	}

	return config, nil
}
