package gnmic

import "encoding/json"

// decodeConfig reads a schemaless CRD config. apiextensionsv1.JSON is stored as
// JSON. yaml.v2 turns nested objects into map[interface{}]interface{}, which
// encoding/json cannot marshal, and that failure aborts the cluster apply
// before any pod is updated.
func decodeConfig(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if config == nil {
		return map[string]any{}, nil
	}
	return config, nil
}
