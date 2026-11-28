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
	LabelValueCertTypeAPI    = "api"

	// AnnotationBootstrapConfig is a checksum of the collector bootstrap file.
	// The file is mounted with a subPath, which Kubernetes does not refresh, so
	// a change to this annotation is what rolls the pods onto the new file.
	AnnotationBootstrapConfig = "operator.gnmic.dev/bootstrap-config"
)

const (
	// Config
	gNMIcConfigPath = "/etc/gnmic/config.yaml"
	gNMIcConfigFile = "config.yaml"
)
