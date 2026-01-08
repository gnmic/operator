package gnmic

import (
	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	"gopkg.in/yaml.v2"
)

// buildOutputConfig creates a gNMIc output config map from an OutputSpec
func buildOutputConfig(spec *gnmicv1alpha1.OutputSpec) (map[string]any, error) {
	config := make(map[string]any)

	// parse the config YAML/JSON
	if spec.Config.Raw != nil {
		if err := yaml.Unmarshal(spec.Config.Raw, &config); err != nil {
			return nil, err
		}
	}

	// set the type
	config["type"] = spec.Type

	return config, nil
}
