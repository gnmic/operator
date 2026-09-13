package controller

import (
	"context"
	"reflect"
	"slices"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

func tlsCluster(replicas int32) *gnmicv1alpha1.Cluster {
	c := &gnmicv1alpha1.Cluster{}
	c.Name, c.Namespace = "c1", "telemetry"
	c.Spec.Replicas = ptr.To(replicas)
	c.Spec.Image = "gnmic:test"
	c.Spec.API = &gnmicv1alpha1.APIConfig{RestPort: 7890, TLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "ca"}}
	c.Spec.GRPCTunnel = &gnmicv1alpha1.GRPCTunnelConfig{Port: 57401, TLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "ca"}}
	return c
}

// Server certificates are cluster-wide: one object, a wildcard for the pods,
// the Service names, and no pod-specific label that would tie it to an ordinal.
func TestServerCertificatesAreClusterScoped(t *testing.T) {
	r := NewClusterReconcilerForTest()
	cluster := tlsCluster(3)

	api := r.buildCertificate(cluster)
	if api.Name != "gnmic-c1-api-tls" || api.Spec.SecretName != "gnmic-c1-api-tls" || api.Spec.CommonName != "gnmic-c1" {
		t.Errorf("api certificate identity: name=%s secret=%s cn=%s", api.Name, api.Spec.SecretName, api.Spec.CommonName)
	}
	for _, want := range []string{
		"*.gnmic-c1.telemetry.svc." + gnmic.ClusterDomain(), // every pod
		"gnmic-c1.telemetry.svc",                            // the headless Service
	} {
		if !slices.Contains(api.Spec.DNSNames, want) {
			t.Errorf("api DNSNames lack %q: %v", want, api.Spec.DNSNames)
		}
	}
	if api.Labels[LabelCertType] != LabelValueCertTypeAPI {
		t.Errorf("api certificate lacks cert-type label: %v", api.Labels)
	}
	if _, perPod := api.Spec.SecretTemplate.Labels[LabelPodName]; perPod {
		t.Errorf("api Secret template carries a pod label: %v", api.Spec.SecretTemplate.Labels)
	}

	tunnel := r.buildTunnelCertificate(cluster)
	if tunnel.Name != "gnmic-c1-tunnel-tls" || tunnel.Spec.CommonName != "gnmic-c1" {
		t.Errorf("tunnel certificate identity: name=%s cn=%s", tunnel.Name, tunnel.Spec.CommonName)
	}
	for _, want := range []string{"*.gnmic-c1.telemetry.svc." + gnmic.ClusterDomain(), "gnmic-c1-grpc-tunnel.telemetry.svc"} {
		if !slices.Contains(tunnel.Spec.DNSNames, want) {
			t.Errorf("tunnel DNSNames lack %q: %v", want, tunnel.Spec.DNSNames)
		}
	}
	if tunnel.Labels[LabelCertType] != LabelValueCertTypeTunnel {
		t.Errorf("tunnel certificate lacks cert-type label: %v", tunnel.Labels)
	}

	// Neither name parses as a legacy per-pod certificate, so the legacy
	// cleanup can never mistake the new objects for old ones.
	if r.extractOrdinalFromCertName(api.Name, "gnmic-c1") >= 0 || r.extractOrdinalFromTunnelCertName(tunnel.Name, "gnmic-c1") >= 0 ||
		r.extractOrdinalFromCertName(tunnel.Name, "gnmic-c1") >= 0 {
		t.Error("cluster-wide certificate names must not look like per-pod ones")
	}
}

// The reason for the change: the pod template must not depend on the replica
// count, or every scale operation rolls every pod.
func TestStatefulSetTemplateIndependentOfReplicas(t *testing.T) {
	r := NewClusterReconcilerForTest()
	one, _, err := r.buildStatefulSet(tlsCluster(1))
	if err != nil {
		t.Fatal(err)
	}
	three, _, err := r.buildStatefulSet(tlsCluster(3))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one.Spec.Template.Spec, three.Spec.Template.Spec) {
		t.Errorf("pod template differs between 1 and 3 replicas:\n1: %+v\n3: %+v", one.Spec.Template.Spec.Volumes, three.Spec.Template.Spec.Volumes)
	}
	// And the volumes are plain Secret mounts of the cluster-wide certificates.
	var apiVol, tunnelVol *corev1.Volume
	for i := range one.Spec.Template.Spec.Volumes {
		switch one.Spec.Template.Spec.Volumes[i].Name {
		case "tls-certs":
			apiVol = &one.Spec.Template.Spec.Volumes[i]
		case "tunnel-tls-certs":
			tunnelVol = &one.Spec.Template.Spec.Volumes[i]
		}
	}
	if apiVol == nil || apiVol.Secret == nil || apiVol.Secret.SecretName != "gnmic-c1-api-tls" {
		t.Errorf("tls-certs volume = %+v, want Secret gnmic-c1-api-tls", apiVol)
	}
	if tunnelVol == nil || tunnelVol.Secret == nil || tunnelVol.Secret.SecretName != "gnmic-c1-tunnel-tls" {
		t.Errorf("tunnel-tls-certs volume = %+v, want Secret gnmic-c1-tunnel-tls", tunnelVol)
	}
	for _, m := range one.Spec.Template.Spec.Containers[0].VolumeMounts {
		if (m.Name == "tls-certs" || m.Name == "tunnel-tls-certs") && m.SubPathExpr != "" {
			t.Errorf("mount %s still uses subPathExpr %q", m.Name, m.SubPathExpr)
		}
	}
}

func TestStatefulSetRolledOut(t *testing.T) {
	sts := func(gen, obs int64, replicas, ready int32, cur, upd string) *appsv1.StatefulSet {
		s := &appsv1.StatefulSet{}
		s.Generation = gen
		s.Spec.Replicas = ptr.To(int32(2))
		s.Status = appsv1.StatefulSetStatus{ObservedGeneration: obs, Replicas: replicas, ReadyReplicas: ready, CurrentRevision: cur, UpdateRevision: upd}
		return s
	}
	cases := map[string]struct {
		sts  *appsv1.StatefulSet
		want bool
	}{
		"converged":            {sts(3, 3, 2, 2, "r2", "r2"), true},
		"rollout in flight":    {sts(3, 3, 2, 2, "r1", "r2"), false},
		"not all ready":        {sts(3, 3, 2, 1, "r2", "r2"), false},
		"old generation":       {sts(4, 3, 2, 2, "r2", "r2"), false},
		"fresh, no status yet": {sts(1, 0, 0, 0, "", ""), false},
		"nil":                  {nil, false},
	}
	for name, c := range cases {
		if got := statefulSetRolledOut(c.sts); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

// Upgrading from per-pod certificates: the old Certificates are removed and
// nothing else is touched -- not the cluster-wide objects, not another
// cluster's, and not the legacy Secrets (Secrets are read-only for the
// operator; they are left for manual removal).
func TestLegacyPodCertificatesRemoved(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, gnmicv1alpha1.AddToScheme, certmanagerv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	cluster := tlsCluster(1)
	clusterLabels := map[string]string{LabelClusterName: "c1"}
	cert := func(name string, labels map[string]string) *certmanagerv1.Certificate {
		return &certmanagerv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "telemetry", Labels: labels}}
	}
	secret := func(name string, labels map[string]string) *corev1.Secret {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "telemetry", Labels: labels}}
	}
	legacyLabels := map[string]string{LabelClusterName: "c1", LabelPodName: "gnmic-c1-0"}
	tunnelLabels := map[string]string{LabelClusterName: "c1", LabelCertType: LabelValueCertTypeTunnel}
	apiLabels := map[string]string{LabelClusterName: "c1", LabelCertType: LabelValueCertTypeAPI}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		// legacy per-pod objects: go
		cert("gnmic-c1-0-tls", clusterLabels), secret("gnmic-c1-0-tls", legacyLabels),
		cert("gnmic-c1-1-tunnel-tls", tunnelLabels), secret("gnmic-c1-1-tunnel-tls", map[string]string{LabelClusterName: "c1", LabelPodName: "gnmic-c1-1", LabelCertType: LabelValueCertTypeTunnel}),
		// current objects: stay
		cert("gnmic-c1-api-tls", apiLabels), secret("gnmic-c1-api-tls", apiLabels),
		cert("gnmic-c1-tunnel-tls", tunnelLabels), secret("gnmic-c1-tunnel-tls", tunnelLabels),
		cert("gnmic-c1-client-tls", map[string]string{LabelClusterName: "c1", LabelCertType: LabelValueCertTypeClient}),
		// another cluster's legacy object: not ours
		cert("gnmic-c2-0-tls", map[string]string{LabelClusterName: "c2"}),
	).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	if err := r.cleanupLegacyPodCertificates(context.Background(), cluster); err != nil {
		t.Fatal(err)
	}
	gone := func(obj interface{ GetName() string }, get func() error) {
		t.Helper()
		if err := get(); !apierrors.IsNotFound(err) {
			t.Errorf("%s should be deleted, got err=%v", obj.GetName(), err)
		}
	}
	kept := func(name string, get func() error) {
		t.Helper()
		if err := get(); err != nil {
			t.Errorf("%s should survive, got %v", name, err)
		}
	}
	key := func(name string) types.NamespacedName {
		return types.NamespacedName{Namespace: "telemetry", Name: name}
	}
	var c certmanagerv1.Certificate
	var s corev1.Secret
	gone(cert("gnmic-c1-0-tls", nil), func() error { return cl.Get(context.Background(), key("gnmic-c1-0-tls"), &c) })
	gone(cert("gnmic-c1-1-tunnel-tls", nil), func() error { return cl.Get(context.Background(), key("gnmic-c1-1-tunnel-tls"), &c) })
	for _, name := range []string{"gnmic-c1-api-tls", "gnmic-c1-tunnel-tls", "gnmic-c1-client-tls", "gnmic-c2-0-tls"} {
		kept(name, func() error { return cl.Get(context.Background(), key(name), &c) })
	}
	for _, name := range []string{"gnmic-c1-api-tls", "gnmic-c1-tunnel-tls", "gnmic-c1-0-tls", "gnmic-c1-1-tunnel-tls"} {
		kept(name, func() error { return cl.Get(context.Background(), key(name), &s) })
	}
}
