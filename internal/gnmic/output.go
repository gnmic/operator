package gnmic

import (
	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	"gopkg.in/yaml.v2"
)

const (
	// output types
	PrometheusOutputType      = "prometheus"
	PrometheusWriteOutputType = "prometheus_write"
	KafkaOutputType           = "kafka"
	InfluxDBOutputType        = "influxdb"
	TCPOutputType             = "tcp"
	UDPOutputType             = "udp"
	FileOutputType            = "file"
	NATSOutputType            = "nats"
	JetstreamOutputType       = "jetstream"
	// OTLP
)

const (
	PrometheusDefaultPort = 9804
	PrometheusDefaultPath = "/metrics"
	PrmetheusPortPoolSize = 1009 // lucky prime!
)

// buildOutputConfig creates a gNMIc output config map from an OutputSpec
func buildOutputConfig(spec *gnmicv1alpha1.OutputSpec, processors []string) (map[string]any, error) {
	config := make(map[string]any)

	// parse the config YAML/JSON
	if spec.Config.Raw != nil {
		if err := yaml.Unmarshal(spec.Config.Raw, &config); err != nil {
			return nil, err
		}
	}

	// set the type
	config["type"] = spec.Type

	// set event-processors if provided
	if len(processors) > 0 {
		config["event-processors"] = processors
	}

	// apply default values
	switch spec.Type {
	case "prometheus":
		if config["path"] == nil {
			config["path"] = PrometheusDefaultPath
		}
	}

	return config, nil
}
