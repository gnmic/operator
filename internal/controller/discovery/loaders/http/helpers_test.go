package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

// fakeResourceFetcher is a lightweight test double.
type fakeResourceFetcher struct {
	secretValue   string
	configuration string
	secretErr     error
	configMapErr  error
}

func (f fakeResourceFetcher) GetSecretKey(_ context.Context, _ string, _ *corev1.SecretKeySelector) (string, error) {
	return f.secretValue, f.secretErr
}

func (f fakeResourceFetcher) GetConfigMapKey(_ context.Context, _ string, _ *corev1.ConfigMapKeySelector) (string, error) {
	return f.configuration, f.configMapErr
}

func makeLoader(spec gnmicv1alpha1.HTTPConfig, fetcher core.ResourceFetcher) *Loader {
	if spec.Method == "" {
		spec.Method = http.MethodGet
	}
	if spec.Interval == nil {
		spec.Interval = &metav1.Duration{Duration: 6 * time.Hour}
	}
	return &Loader{
		loaderCfg: core.CommonLoaderConfig{
			TargetsourceNN:  types.NamespacedName{Namespace: "default", Name: "test"},
			ChunkSize:       10,
			ResourceFetcher: fetcher,
		},
		spec: spec,
	}
}

func mustBuildClient(t *testing.T, loader *Loader) *http.Client {
	t.Helper()
	client, err := loader.buildHTTPClient(context.Background())
	if err != nil {
		t.Fatalf("buildHTTPClient failed: %v", err)
	}
	return client
}

func startLoaderRun(loader *Loader) (context.Context, context.CancelFunc, chan []core.DiscoveryMessage, chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan []core.DiscoveryMessage, 1)
	done := make(chan error, 1)
	go func() { done <- loader.Run(ctx, out, gnmicv1alpha1.TargetSourceSpec{}) }()
	return ctx, cancel, out, done
}

// genSelfSignedCertPEM generates a self-signed certificate PEM used in tests.
func genSelfSignedCertPEM() (string, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-ca",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
