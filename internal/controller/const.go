package controller

const (
	// Labels
	LabelClusterName  = "operator.gnmic.dev/cluster"
	LabelOutputName   = "operator.gnmic.dev/output"
	LabelPipelineName = "operator.gnmic.dev/pipeline"
	LabelPodName      = "operator.gnmic.dev/pod-name"

	LabelValueName      = "gnmic"
	LabelValueManagedBy = "gnmic-operator"

	LabelServiceType                      = "operator.gnmic.dev/service-type"
	LabelValueServiceTypeTunnel           = "tunnel"
	LabelValueServiceTypePrometheusOutput = "prometheus-output"
	LabelValueServiceTypeHeadless         = "rest-api"

	LabelOutputType                = "operator.gnmic.dev/output-type"
	LabelValueOutputTypePrometheus = "prometheus-output"

	LabelCertType            = "operator.gnmic.dev/cert-type"
	LabelValueCertTypeClient = "client"
	LabelValueCertTypeTunnel = "tunnel"
)

const (
	// Config
	gNMIcConfigPath = "/etc/gnmic/config.yaml"
	gNMIcConfigFile = "config.yaml"
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
