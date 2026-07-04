package gnmic

import (
	"os"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func TestGetControllerTLSPaths(t *testing.T) {
	t.Setenv("GNMIC_TLS_CERT", "/custom/cert.crt")
	t.Setenv("GNMIC_TLS_KEY", "/custom/key.key")
	t.Setenv("GNMIC_TLS_CA", "/custom/ca.crt")

	if got := GetControllerCertPath(); got != "/custom/cert.crt" {
		t.Fatalf("cert path = %q", got)
	}
	if got := GetControllerKeyPath(); got != "/custom/key.key" {
		t.Fatalf("key path = %q", got)
	}
	if got := GetControllerCAPath(); got != "/custom/ca.crt" {
		t.Fatalf("ca path = %q", got)
	}

	os.Unsetenv("GNMIC_TLS_CERT")
	os.Unsetenv("GNMIC_TLS_KEY")
	os.Unsetenv("GNMIC_TLS_CA")

	if GetControllerCertPath() != DefaultControllerCertPath {
		t.Fatal("expected default cert path")
	}
	if GetControllerKeyPath() != DefaultControllerKeyPath {
		t.Fatal("expected default key path")
	}
	if GetControllerCAPath() != DefaultControllerCAPath {
		t.Fatal("expected default CA path")
	}
}

func TestTLSConfigForClusterPod(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	if TLSConfigForClusterPod(cluster) != nil {
		t.Fatal("expected nil when API TLS unset")
	}

	cluster.Spec.API = &gnmicv1alpha1.APIConfig{
		TLS: &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "issuer"},
	}
	cfg := TLSConfigForClusterPod(cluster)
	if cfg == nil || cfg.CertFile != CertFilePath || cfg.ClientAuth != clientAuthRequireVerify {
		t.Fatalf("unexpected API TLS config: %+v", cfg)
	}
}

func TestTunnelServerTLSConfig(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	if TunnelServerTLSConfig(cluster) != nil {
		t.Fatal("expected nil when tunnel TLS unset")
	}

	cluster.Spec.GRPCTunnel = &gnmicv1alpha1.GRPCTunnelConfig{
		TLS: &gnmicv1alpha1.ClusterTLSConfig{
			IssuerRef: "issuer",
			BundleRef: "bundle",
		},
	}
	cfg := TunnelServerTLSConfig(cluster)
	if cfg == nil || cfg.CertFile != TunnelCertFilePath || cfg.CAFile != TunnelCABundleFilePath {
		t.Fatalf("unexpected tunnel TLS config: %+v", cfg)
	}
}

func TestClientTLSConfigForCluster(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	if ClientTLSConfigForCluster(cluster) != nil {
		t.Fatal("expected nil when client TLS unset")
	}

	cluster.Spec.ClientTLS = &gnmicv1alpha1.ClusterTLSConfig{
		IssuerRef: "issuer",
		BundleRef: "bundle",
	}
	paths := ClientTLSConfigForCluster(cluster)
	if paths == nil || paths.CertFile != ClientTLSCertFilePath || paths.CAFile != ClientCABundleFilePath {
		t.Fatalf("unexpected client TLS paths: %+v", paths)
	}
}
