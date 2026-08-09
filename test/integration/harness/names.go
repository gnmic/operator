//go:build integration

package harness

import "fmt"

// Labels the operator puts on the objects it owns. Mirrors
// internal/controller/const.go; kept here so suites do not import internal
// packages, and so a rename in the operator breaks in exactly one place.
const (
	LabelClusterName           = "operator.gnmic.dev/cluster"
	LabelOutputName            = "operator.gnmic.dev/output"
	LabelPipelineName          = "operator.gnmic.dev/pipeline"
	LabelPodName               = "operator.gnmic.dev/pod-name"
	LabelServiceType           = "operator.gnmic.dev/service-type"
	LabelOutputType            = "operator.gnmic.dev/output-type"
	LabelCertType              = "operator.gnmic.dev/cert-type"
	LabelTargetSourceFinalizer = "operator.gnmic.dev/targetsource-finalizer"

	ValueName                  = "gnmic"
	ValueManagedBy             = "gnmic-operator"
	ValueServiceTypeHeadless   = "rest-api"
	ValueServiceTypeTunnel     = "tunnel"
	ValueServiceTypePrometheus = "prometheus-output"
	ValueCertTypeClient        = "client"
	ValueCertTypeTunnel        = "tunnel"
)

const resourcePrefix = "gnmic-"

// The operator's naming scheme for the objects it builds. Suites call these
// rather than formatting names inline.

func StatefulSetName(cluster string) string     { return resourcePrefix + cluster }
func HeadlessServiceName(cluster string) string { return resourcePrefix + cluster }
func ConfigMapName(cluster string) string       { return resourcePrefix + cluster + "-config" }
func TunnelServiceName(cluster string) string   { return resourcePrefix + cluster + "-grpc-tunnel" }
func ClientCertName(cluster string) string      { return resourcePrefix + cluster + "-client-tls" }

func PromServiceName(cluster, pipeline, output string) string {
	return fmt.Sprintf("%s%s-prom-%s-%s", resourcePrefix, cluster, pipeline, output)
}

func PodName(cluster string, ordinal int) string {
	return fmt.Sprintf("%s%s-%d", resourcePrefix, cluster, ordinal)
}
