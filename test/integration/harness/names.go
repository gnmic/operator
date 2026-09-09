//go:build integration

package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

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
func APICertName(cluster string, ordinal int) string {
	return fmt.Sprintf("%s%s-%d-tls", resourcePrefix, cluster, ordinal)
}
func ControllerCAConfigMap(cluster string) string {
	return resourcePrefix + cluster + "-controller-ca"
}

// PromServiceName mirrors controller.PrometheusServiceName, including the
// cut-and-hash applied when the joined name exceeds the 63-character Service
// limit.
func PromServiceName(cluster, pipeline, output string) string {
	const maxLen = 63
	name := fmt.Sprintf("%s%s-prom-%s-%s", resourcePrefix, cluster, pipeline, output)
	if len(name) <= maxLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:8]
	return strings.TrimRight(name[:maxLen-len(suffix)-1], "-") + "-" + suffix
}

func PodName(cluster string, ordinal int) string {
	return fmt.Sprintf("%s%s-%d", resourcePrefix, cluster, ordinal)
}
