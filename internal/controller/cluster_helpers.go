package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"k8s.io/utils/ptr"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// maxServiceNameLen is the DNS-1035 label limit Kubernetes applies to a
// Service name.
const maxServiceNameLen = 63

// desiredReplicas is the replica count the Cluster asks for. The CRD defaults
// the field to 1, so nil is unreachable through the API; this keeps every
// reader agreeing on what nil means instead of dereferencing it.
func desiredReplicas(cluster *gnmicv1alpha1.Cluster) int32 {
	return ptr.Deref(cluster.Spec.Replicas, 1)
}

// PrometheusServiceName names the Service exposing one Prometheus output of
// one pipeline.
//
// Three user-chosen names joined with fixed separators exceed the 63-character
// Service limit easily, and the Create then failed with nothing pointing at
// the length. Over the limit the name is cut and suffixed with a hash of the
// full name, so it stays unique and stable across reconciles. A name that fits
// is returned as is, so nothing that exists today is renamed.
//
// test/integration/harness.PromServiceName mirrors this; change both.
func PrometheusServiceName(cluster, pipeline, output string) string {
	name := fmt.Sprintf("%s%s-prom-%s-%s", resourcePrefix, cluster, pipeline, output)
	if len(name) <= maxServiceNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:8]
	// A label may not end in "-", and the cut can land on one.
	head := strings.TrimRight(name[:maxServiceNameLen-len(suffix)-1], "-")
	return head + "-" + suffix
}
