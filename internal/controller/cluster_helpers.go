package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
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

// podNameEnv is the downward-API variable the TLS volume mounts key on.
var podNameEnv = corev1.EnvVar{
	Name: "POD_NAME",
	ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
	},
}

// ensurePodNameEnv returns env with exactly one POD_NAME, set from the
// downward API.
//
// Both TLS paths need it for their subPathExpr, and the user may also have put
// one in spec.env. The API TLS path used to append unconditionally, so a
// user-supplied POD_NAME produced a duplicate entry. It is replaced rather than
// kept: the volume mount only works when POD_NAME is the pod's own name, so
// when TLS is on the operator owns the variable.
func ensurePodNameEnv(env []corev1.EnvVar) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == podNameEnv.Name {
			env[i] = podNameEnv
			return env
		}
	}
	return append(env, podNameEnv)
}
