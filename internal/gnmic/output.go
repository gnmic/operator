package gnmic

import (
	"fmt"
	"strconv"
	"strings"

	gnmicv1alpha1 "github.com/gnmic/gnmic-operator/api/v1alpha1"
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
	// TODO: OTLP
)

const (
	PrometheusDefaultPort = 9804
	PrometheusDefaultPath = "/metrics"
	PrmetheusPortPoolSize = 1009 // lucky prime!
)

// OutputTypesWithServiceRef lists output types that support serviceRef/serviceSelector
var OutputTypesWithServiceRef = map[string]bool{
	NATSOutputType:            true,
	JetstreamOutputType:       true,
	KafkaOutputType:           true,
	PrometheusWriteOutputType: true,
	InfluxDBOutputType:        true,
}

// TODO: move  this under PiplelineData struct
type outputConfigOptions struct {
	// Processors to reference under the output config
	Processors []string
	// Resolved addresses to inject into the output config
	// list of resolved addresses (e.g: "nats://service:4222")
	ResolvedAddresses []string
	// TLS options to inject into the output config
	TLS *TLSOptions
}

type TLSOptions struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	SkipVerify bool
	ClientAuth string
}

// buildOutputConfig creates a gNMIc output config map from an OutputSpec
// resolvedAddresses contains addresses resolved from serviceRef/serviceSelector
func buildOutputConfig(spec *gnmicv1alpha1.OutputSpec, options *outputConfigOptions) (map[string]any, error) {
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
	if len(options.Processors) > 0 {
		config["event-processors"] = options.Processors
	}

	// inject resolved addresses for supported output types
	if len(options.ResolvedAddresses) > 0 {
		if OutputTypesWithServiceRef[spec.Type] {
			switch spec.Type {
			case NATSOutputType, JetstreamOutputType:
				// NATS supports comma-separated addresses
				config["address"] = strings.Join(options.ResolvedAddresses, ",")
			case KafkaOutputType:
				// Kafka uses comma-separated broker list
				config["address"] = strings.Join(options.ResolvedAddresses, ",")
			case PrometheusWriteOutputType:
				config["url"] = strings.Join(options.ResolvedAddresses, ",")
			case InfluxDBOutputType:
				config["url"] = strings.Join(options.ResolvedAddresses, ",")
			}
		}
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

// FormatServiceAddress formats a service address with the appropriate scheme for the output type
func FormatServiceAddress(spec *gnmicv1alpha1.OutputSpec, host string, port int32) string {
	switch spec.Type {
	case NATSOutputType, JetstreamOutputType:
		return fmt.Sprintf("nats://%s:%d", host, port)
	case KafkaOutputType:
		// Kafka doesn't use a scheme prefix
		return fmt.Sprintf("%s:%d", host, port)
	case PrometheusWriteOutputType:
		// find out of TLS is enabled in the raw config
		if spec.Config.Raw != nil {
			var config map[string]any
			if err := yaml.Unmarshal(spec.Config.Raw, &config); err != nil {
				return fmt.Sprintf("http://%s:%d", host, port)
			}
			if config["tls"] != nil {
				return fmt.Sprintf("https://%s:%d", host, port)
			}
		}
		return fmt.Sprintf("http://%s:%d", host, port)
	case InfluxDBOutputType:
		if spec.Config.Raw != nil {
			var config map[string]any
			if err := yaml.Unmarshal(spec.Config.Raw, &config); err != nil {
				return fmt.Sprintf("http://%s:%d", host, port)
			}
			if config["tls"] != nil {
				return fmt.Sprintf("https://%s:%d", host, port)
			}
		}
		return fmt.Sprintf("http://%s:%d", host, port)
	default:
		return fmt.Sprintf("%s:%d", host, port)
	}
}

// ParseServicePort parses a port string (name or number) and returns the port number
// from the provided port list
func ParseServicePort(portStr string, ports []ServicePort) (int32, error) {
	if portStr == "" && len(ports) > 0 {
		// Default to first port
		return ports[0].Port, nil
	}

	// Try parsing as number first
	if portNum, err := strconv.ParseInt(portStr, 10, 32); err == nil {
		return int32(portNum), nil
	}

	// Search by name
	for _, p := range ports {
		if p.Name == portStr {
			return p.Port, nil
		}
	}

	return 0, fmt.Errorf("port %q not found in service", portStr)
}

// ServicePort represents a simplified service port
type ServicePort struct {
	Name string
	Port int32
}
