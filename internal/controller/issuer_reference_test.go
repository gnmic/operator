package controller

import (
	"maps"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

const (
	stepIssuerName  = "step-ca"
	stepIssuerKind  = "StepIssuer"
	stepIssuerGroup = "certmanager.step.sm"
)

// A Cluster may name an issuer outside the cert-manager.io Issuer kind, such as
// a StepIssuer. The kind and group reach the Certificate, and an unset
// pair renders the reference exactly as it did before the fields existed.
func TestCertificateIssuerReferenceCarriesKindAndGroup(t *testing.T) {
	t.Parallel()

	r := NewClusterReconcilerForTest()
	cluster := &gnmicv1alpha1.Cluster{}
	cluster.Name, cluster.Namespace = "c1", "netmon"
	cluster.Spec.ClientTLS = &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: stepIssuerName}

	got := r.buildClientTLSCertificate(cluster, "gnmic-c1-client-tls").Spec.IssuerRef
	want := cmmeta.IssuerReference{Name: stepIssuerName, Kind: certmanagerv1.IssuerKind}
	if got != want {
		t.Errorf("default issuerRef = %+v, want %+v", got, want)
	}

	cluster.Spec.ClientTLS.IssuerKind = stepIssuerKind
	cluster.Spec.ClientTLS.IssuerGroup = stepIssuerGroup
	external := r.buildClientTLSCertificate(cluster, "gnmic-c1-client-tls")
	want = cmmeta.IssuerReference{Name: stepIssuerName, Kind: stepIssuerKind, Group: stepIssuerGroup}
	if external.Spec.IssuerRef != want {
		t.Errorf("issuerRef = %+v, want %+v", external.Spec.IssuerRef, want)
	}

	// The name alone no longer identifies the issuer: a Certificate from the
	// default kind must be re-issued when the Cluster moves to another kind.
	previous := external.DeepCopy()
	previous.Spec.IssuerRef = cmmeta.IssuerReference{Name: stepIssuerName, Kind: certmanagerv1.IssuerKind}
	if !r.certificateNeedsUpdate(previous, external) {
		t.Error("a certificate from a different issuer kind should be updated")
	}
	if r.certificateNeedsUpdate(external, external.DeepCopy()) {
		t.Error("an identical certificate should not be updated")
	}
}

func TestCSIVolumeCarriesIssuerKindAndGroup(t *testing.T) {
	t.Parallel()

	r := NewClusterReconcilerForTest()
	cluster := &gnmicv1alpha1.Cluster{}
	cluster.Name, cluster.Namespace = "c1", "netmon"
	cluster.Spec.Image = "gnmic"
	cluster.Spec.ClientTLS = &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: stepIssuerName, UseCSIDriver: true}

	got := clientCSIAttributes(t, r, cluster)
	want := map[string]string{
		csiIssuerNameAttr:                 stepIssuerName,
		csiIssuerKindAttr:                 certmanagerv1.IssuerKind,
		"csi.cert-manager.io/common-name": "c1.netmon",
		"csi.cert-manager.io/dns-names":   "c1.netmon",
	}
	if !maps.Equal(got, want) {
		t.Errorf("default issuer attributes = %v, want %v", got, want)
	}

	cluster.Spec.ClientTLS.IssuerKind = stepIssuerKind
	cluster.Spec.ClientTLS.IssuerGroup = stepIssuerGroup
	got = clientCSIAttributes(t, r, cluster)
	want[csiIssuerKindAttr] = stepIssuerKind
	want[csiIssuerGroupAttr] = stepIssuerGroup
	if !maps.Equal(got, want) {
		t.Errorf("issuer attributes = %v, want %v", got, want)
	}
}

func clientCSIAttributes(t *testing.T, r *ClusterReconciler, cluster *gnmicv1alpha1.Cluster) map[string]string {
	t.Helper()

	sts, _, err := r.buildStatefulSet(cluster)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Name == "client-tls-certs" && v.CSI != nil {
			return v.CSI.VolumeAttributes
		}
	}
	t.Fatal("no client-tls-certs CSI volume")

	return nil
}
