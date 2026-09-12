package controller

import (
	"slices"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// The certificate a pod presents on its REST and gNMI ports must cover the
// headless Service name as well as the pod's own names: the headless name
// resolves to an arbitrary pod, and a verifying client dialing it was rejected.
func TestAPICertificateCoversHeadlessServiceName(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	cluster.Name, cluster.Namespace = "c1", "telemetry"
	cluster.Spec.API = &gnmicv1alpha1.APIConfig{TLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "ca"}}
	r := NewClusterReconcilerForTest()

	cert := r.buildCertificate(cluster, "gnmic-c1-0-tls", "gnmic-c1-0", "gnmic-c1")
	for _, want := range []string{
		"gnmic-c1-0", "gnmic-c1-0.gnmic-c1.telemetry.svc", // per-pod, as before
		"gnmic-c1", "gnmic-c1.telemetry", "gnmic-c1.telemetry.svc", // headless Service
	} {
		if !slices.Contains(cert.Spec.DNSNames, want) {
			t.Errorf("DNSNames lack %q: %v", want, cert.Spec.DNSNames)
		}
	}
	if cert.Spec.CommonName != "gnmic-c1-0" {
		t.Errorf("CommonName = %q, want the pod name", cert.Spec.CommonName)
	}

	// A certificate issued before this change differs in DNSNames, so the
	// reconciler re-issues it on upgrade rather than leaving the old SAN set.
	old := cert.DeepCopy()
	old.Spec.DNSNames = old.Spec.DNSNames[:5]
	if !r.certificateNeedsUpdate(old, cert) {
		t.Error("a certificate without the headless names should be updated")
	}
	if r.certificateNeedsUpdate(cert, cert.DeepCopy()) {
		t.Error("an identical certificate should not be updated")
	}
}
