package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// ---------------------------------------------------------------- #16

func okBuild(calls *int) func() (*tls.Config, error) {
	return func() (*tls.Config, error) { *calls++; return &tls.Config{}, nil }
}

// A hit must not run the build callback: assembling a cert pool is the expensive
// part, and the first version of this cache did it before consulting the entry.
func TestClientCacheDoesNotBuildOnHit(t *testing.T) {
	c := newClientCache()
	calls := 0

	first, err := c.get("ns/c1", "rev-a", clientPooled, okBuild(&calls))
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.get("ns/c1", "rev-a", clientPooled, okBuild(&calls))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("a second call with the same revision returned a different client")
	}
	if calls != 1 {
		t.Errorf("build ran %d times, want once", calls)
	}

	other, err := c.get("ns/c2", "rev-a", clientPooled, okBuild(&calls))
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Error("two clusters share one client")
	}
}

// A changed trust pool has to rebuild, or a rotated CA would never apply.
func TestClientCacheRebuildsWhenRevisionChanges(t *testing.T) {
	c := newClientCache()
	calls := 0

	first, _ := c.get("ns/c1", "rev-a", clientPooled, okBuild(&calls))
	rotated, _ := c.get("ns/c1", "rev-b", clientPooled, okBuild(&calls))
	if rotated == first {
		t.Fatal("a changed revision reused the old client")
	}
	if calls != 2 {
		t.Errorf("build ran %d times, want twice", calls)
	}
	if len(c.entries) != 1 {
		t.Errorf("entries = %d, want the old one replaced", len(c.entries))
	}
}

func TestClientCacheBuildError(t *testing.T) {
	c := newClientCache()
	_, err := c.get("ns/c1", "rev", clientPooled, func() (*tls.Config, error) {
		return nil, errBuild
	})
	if err == nil {
		t.Fatal("expected the build error to surface")
	}
	if len(c.entries) != 0 {
		t.Error("a failed build was cached")
	}
}

var errBuild = errors.New("bad CA")

func TestClientCacheEvict(t *testing.T) {
	c := newClientCache()
	calls := 0
	_, _ = c.get("ns/c1", "rev", clientPooled, okBuild(&calls))
	c.evict("ns/c1")
	if len(c.entries) != 0 {
		t.Fatal("evict left the entry behind")
	}
	c.evict("ns/missing")

	var nilCache *clientCache
	got, err := nilCache.get("k", "rev", clientPooled, okBuild(&calls))
	if err != nil || got == nil {
		t.Fatalf("nil cache: client=%v err=%v", got, err)
	}
	nilCache.evict("k")
}

// The transport must carry an idle timeout: a hand-built one defaults to none, which
// is what let discarded transports pin connections for the life of the process.
func TestClientTransportBoundsIdleConnections(t *testing.T) {
	client, transport := newTLSClient(&tls.Config{}, clientPooled)
	if transport.IdleConnTimeout == 0 {
		t.Error("transport has no idle timeout")
	}
	if client.Timeout != applyRequestTimeout {
		t.Errorf("client timeout = %v", client.Timeout)
	}
	if transport.DisableKeepAlives {
		t.Error("the apply path wants keep-alive; without it every apply re-handshakes")
	}

	// The streaming client is cached like any other, but must never hand a pooled
	// connection to a stream that lasts minutes: doing so dropped the SSE stream,
	// which invalidated the ApplyCache, which re-applied, which reloaded the
	// target's subscription.
	streaming, streamingTransport := newTLSClient(&tls.Config{}, clientStreaming)
	if streaming.Timeout != 0 {
		t.Errorf("streaming client timeout = %v, want none", streaming.Timeout)
	}
	if !streamingTransport.DisableKeepAlives {
		t.Error("the streaming transport reuses pooled connections")
	}
}

// ---------------------------------------------------------------- TLS material

// The CA revision must change with the bytes and only with the bytes, so an
// unchanged file never invalidates a cached client.
func TestTLSMaterialCARevisionTracksContent(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caPath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &TLSMaterial{caPath: caPath}

	m.reloadCA(context.Background())
	pem, rev1 := m.CA()
	if string(pem) != "first" || rev1 == "" {
		t.Fatalf("pem=%q rev=%q", pem, rev1)
	}

	// Re-reading identical content must not move the revision.
	m.reloadCA(context.Background())
	if _, rev := m.CA(); rev != rev1 {
		t.Errorf("unchanged CA changed the revision: %q -> %q", rev1, rev)
	}

	if err := os.WriteFile(caPath, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.reloadCA(context.Background())
	pem, rev2 := m.CA()
	if string(pem) != "second" {
		t.Errorf("pem = %q, want the new content", pem)
	}
	if rev2 == rev1 {
		t.Error("a changed CA kept the same revision; clients would never rebuild")
	}
}

// An operator with no TLS material must not fail: nil accessors all the way down.
func TestTLSMaterialAbsentIsUsable(t *testing.T) {
	m := &TLSMaterial{caPath: filepath.Join(t.TempDir(), "missing.crt")}
	m.reloadCA(context.Background())
	if pem, rev := m.CA(); pem != nil || rev == "" {
		t.Errorf("absent CA: pem=%v rev=%q (a revision is still expected)", pem, rev)
	}
	if m.ClientCertificate() != nil {
		t.Error("no keypair was configured, so there should be no callback")
	}

	var nilMaterial *TLSMaterial
	if pem, rev := nilMaterial.CA(); pem != nil || rev != "" {
		t.Errorf("nil material returned %v/%q", pem, rev)
	}
	if nilMaterial.ClientCertificate() != nil {
		t.Error("nil material returned a certificate callback")
	}
}

// The watcher runs for the lifetime of the process, on every replica.
func TestTLSMaterialIsNotLeaderElected(t *testing.T) {
	m := &TLSMaterial{}
	if m.NeedLeaderElection() {
		t.Error("a standby replica still needs its own material current")
	}
}

// Start must return when its context is cancelled rather than blocking shutdown.
func TestTLSMaterialStartStops(t *testing.T) {
	m := &TLSMaterial{caPath: filepath.Join(t.TempDir(), "ca.crt")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after its context was cancelled")
	}
}

// ---------------------------------------------------------------- #17

func tunnelCluster(svc *gnmicv1alpha1.ServiceConfig) *gnmicv1alpha1.Cluster {
	return &gnmicv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default"},
		Spec: gnmicv1alpha1.ClusterSpec{
			Image:      "img",
			Replicas:   ptr.To(int32(1)),
			GRPCTunnel: &gnmicv1alpha1.GRPCTunnelConfig{Port: 57400, Service: svc},
		},
	}
}

// Annotations are how a cloud load balancer is configured, and they were not compared
// at all — editing them on a live Cluster did nothing.
func TestTunnelServiceAnnotationsAndLabelsAreUpdated(t *testing.T) {
	scheme := secretWatchScheme(t)
	cluster := tunnelCluster(&gnmicv1alpha1.ServiceConfig{
		Type:        corev1.ServiceTypeLoadBalancer,
		Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-internal": "true"},
		Labels:      map[string]string{"tier": "edge"},
	})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}
	ctx := context.Background()
	svcNN := types.NamespacedName{Name: "gnmic-c1-grpc-tunnel", Namespace: "default"}

	if err := r.reconcileTunnelService(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	var created corev1.Service
	if err := cl.Get(ctx, svcNN, &created); err != nil {
		t.Fatal(err)
	}
	if created.Annotations["service.beta.kubernetes.io/aws-load-balancer-internal"] != "true" {
		t.Fatalf("annotations not set on create: %v", created.Annotations)
	}

	// Something the cluster filled in that the operator must not clobber.
	created.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
	if err := cl.Update(ctx, &created); err != nil {
		t.Fatal(err)
	}

	// Now change the annotations and labels on the Cluster.
	cluster.Spec.GRPCTunnel.Service.Annotations = map[string]string{
		"service.beta.kubernetes.io/aws-load-balancer-internal": "false",
		"added": "yes",
	}
	cluster.Spec.GRPCTunnel.Service.Labels = map[string]string{"tier": "core"}
	if err := r.reconcileTunnelService(ctx, cluster); err != nil {
		t.Fatal(err)
	}

	var updated corev1.Service
	if err := cl.Get(ctx, svcNN, &updated); err != nil {
		t.Fatal(err)
	}
	if got := updated.Annotations["service.beta.kubernetes.io/aws-load-balancer-internal"]; got != "false" {
		t.Errorf("annotation not updated: %q", got)
	}
	if updated.Annotations["added"] != "yes" {
		t.Errorf("new annotation missing: %v", updated.Annotations)
	}
	if updated.Labels["tier"] != "core" {
		t.Errorf("label not updated: %v", updated.Labels)
	}
	// operator labels still win
	if updated.Labels[LabelClusterName] != "c1" {
		t.Errorf("operator label lost: %v", updated.Labels)
	}
	// and the field the cluster owns survived
	if updated.Spec.ExternalTrafficPolicy != corev1.ServiceExternalTrafficPolicyLocal {
		t.Error("externalTrafficPolicy was clobbered by a whole-Spec assignment")
	}
}

// An unchanged Cluster must not rewrite the Service on every reconcile.
func TestTunnelServiceUnchangedDoesNotWrite(t *testing.T) {
	scheme := secretWatchScheme(t)
	cluster := tunnelCluster(&gnmicv1alpha1.ServiceConfig{Type: corev1.ServiceTypeClusterIP})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}
	ctx := context.Background()
	svcNN := types.NamespacedName{Name: "gnmic-c1-grpc-tunnel", Namespace: "default"}

	if err := r.reconcileTunnelService(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	var first corev1.Service
	_ = cl.Get(ctx, svcNN, &first)

	if err := r.reconcileTunnelService(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	var second corev1.Service
	_ = cl.Get(ctx, svcNN, &second)

	if first.ResourceVersion != second.ResourceVersion {
		t.Fatalf("reconcile rewrote an unchanged service: %s -> %s",
			first.ResourceVersion, second.ResourceVersion)
	}
}

// ---------------------------------------------------------------- issuer CA watch

func issuerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := secretWatchScheme(t)
	if err := certmanagerv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

// Rotating an issuing CA used to reach the operator only when something unrelated
// happened to wake this controller, because nothing mapped the Issuer's backing
// Secret to the Clusters that trust it.
func TestSecretBackingAnIssuerWakesTheClustersTrustingIt(t *testing.T) {
	scheme := issuerScheme(t)
	objs := []client.Object{
		&certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-issuer", Namespace: "default"},
			Spec: certmanagerv1.IssuerSpec{IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{SecretName: "ca-secret"},
			}},
		},
		&certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{Name: "other-issuer", Namespace: "default"},
			Spec: certmanagerv1.IssuerSpec{IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{SecretName: "unrelated-secret"},
			}},
		},
		// names the issuer through api.tls
		&gnmicv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "via-api", Namespace: "default"},
			Spec: gnmicv1alpha1.ClusterSpec{Image: "i", Replicas: ptr.To(int32(1)),
				API: &gnmicv1alpha1.APIConfig{RestPort: 7890,
					TLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "ca-issuer"}}},
		},
		// and through clientTLS
		&gnmicv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "via-client", Namespace: "default"},
			Spec: gnmicv1alpha1.ClusterSpec{Image: "i", Replicas: ptr.To(int32(1)),
				ClientTLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "ca-issuer"}},
		},
		// names a different issuer
		&gnmicv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"},
			Spec: gnmicv1alpha1.ClusterSpec{Image: "i", Replicas: ptr.To(int32(1)),
				API: &gnmicv1alpha1.APIConfig{RestPort: 7890,
					TLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "other-issuer"}}},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	got := clusterNames(t, r, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ca-secret", Namespace: "default"},
	})
	want := []string{"via-api", "via-client"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("clusters = %v, want %v", got, want)
	}

	// A Secret backing no Issuer wakes nobody.
	if got := clusterNames(t, r, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nothing-uses-me", Namespace: "default"},
	}); len(got) != 0 {
		t.Errorf("unrelated secret woke %v", got)
	}
}

// writeKeypair writes a self-signed cert/key pair and returns their paths.
func writeKeypair(t *testing.T, dir, cn string) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// The client certificate must be resolved per handshake, not captured into the
// tls.Config. That is what lets a rotation apply without rebuilding any client, and
// it is the whole reason the reconcile path no longer reads the files.
func TestClientCertificateIsResolvedPerHandshake(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeKeypair(t, dir, "first")
	t.Setenv("GNMIC_TLS_CERT", certPath)
	t.Setenv("GNMIC_TLS_KEY", keyPath)
	t.Setenv("GNMIC_TLS_CA", filepath.Join(dir, "ca.crt"))

	m := NewTLSMaterial()
	getCert := m.ClientCertificate()
	if getCert == nil {
		t.Fatal("no certificate callback despite a keypair on disk")
	}

	cfg, err := buildCollectorTLSConfig(m, testCAPEM(t), &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "iss"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetClientCertificate == nil {
		t.Error("tls.Config captured no per-handshake callback")
	}
	if len(cfg.Certificates) != 0 {
		t.Error("certificate was captured by value; a rotation would need a rebuild")
	}

	before, err := getCert(nil)
	if err != nil || before == nil {
		t.Fatalf("cert=%v err=%v", before, err)
	}

	// Rotate on disk. The same callback, and so the same tls.Config and the same
	// client, must now serve the new certificate.
	writeKeypair(t, dir, "second")
	if err := m.certWatcher.ReadCertificate(); err != nil {
		t.Fatal(err)
	}
	after, err := getCert(nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before.Certificate[0], after.Certificate[0]) {
		t.Fatal("the callback still serves the old certificate after a rotation")
	}
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	certPath, _ := writeKeypair(t, dir, "ca")
	b, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
