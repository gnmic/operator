package gnmic

import (
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func TestGNMIServerTLSConfig(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	if GNMIServerTLSConfig(cluster) != nil {
		t.Fatal("expected nil when api.tls is unset")
	}

	cluster.Spec.API = &gnmicv1alpha1.APIConfig{GNMIPort: 9393, TLS: &gnmicv1alpha1.ClusterTLSConfig{}}
	if cfg := GNMIServerTLSConfig(cluster); cfg == nil || cfg.CertFile != "" || cfg.ClientAuth != "" {
		t.Fatalf("api.tls without an issuer should yield an empty TLS block, got %+v", cfg)
	}

	cluster.Spec.API.TLS.IssuerRef = "issuer"
	cfg := GNMIServerTLSConfig(cluster)
	if cfg == nil || cfg.CertFile != CertFilePath || cfg.KeyFile != KeyFilePath {
		t.Fatalf("expected the API server certificate, got %+v", cfg)
	}
	// The REST API requires a client certificate signed by the controller CA;
	// copying that here would lock every gNMI client out.
	if cfg.ClientAuth != "" || cfg.CAFile != "" {
		t.Fatalf("gNMI server must not require controller-CA client certificates, got %+v", cfg)
	}
	rest := TLSConfigForClusterPod(cluster)
	if rest.ClientAuth == "" {
		t.Fatal("test premise: the REST API config does require client auth")
	}
}
