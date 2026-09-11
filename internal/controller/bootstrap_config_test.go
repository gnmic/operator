package controller

import (
	"testing"

	"gopkg.in/yaml.v2"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// renderConfig builds the collector bootstrap config for a Cluster and decodes it.
func renderConfig(t *testing.T, cluster *gnmicv1alpha1.Cluster) map[string]any {
	t.Helper()
	r := NewClusterReconcilerForTest()
	content, err := r.buildConfigContent(cluster)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		t.Fatalf("bootstrap config is not YAML: %v\n%s", err, content)
	}
	return cfg
}

func section(t *testing.T, cfg map[string]any, name string) map[any]any {
	t.Helper()
	v, ok := cfg[name]
	if !ok {
		t.Fatalf("bootstrap config has no %q section: %v", name, cfg)
	}
	m, ok := v.(map[any]any)
	if !ok {
		t.Fatalf("%q is %T, want a mapping", name, v)
	}
	return m
}

// gnmiPort used to add only a container port and a Service port. Nothing told the
// collector to listen, so the documented gNMI proxy/cache was a port that
// answered nothing.
func TestBootstrapConfigRendersGNMIServerForGNMIPort(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	cluster.Name = "c1"
	cluster.Spec.API = &gnmicv1alpha1.APIConfig{RestPort: 7890, GNMIPort: 9393}

	cfg := renderConfig(t, cluster)
	gs := section(t, cfg, "gnmi-server")
	if gs["address"] != ":9393" {
		t.Errorf("gnmi-server.address = %v, want :9393", gs["address"])
	}
	if gs["enable-metrics"] != true {
		t.Errorf("gnmi-server.enable-metrics = %v, want true", gs["enable-metrics"])
	}
	// The collector only builds a cache when the section names one, and the
	// server serves from that cache; without it the listener has nothing to answer.
	cache, ok := gs["cache"].(map[any]any)
	if !ok || cache["type"] != "oc" {
		t.Errorf("gnmi-server.cache = %v, want {type: oc}", gs["cache"])
	}
	if _, hasTLS := gs["tls"]; hasTLS {
		t.Errorf("gnmi-server.tls rendered without api.tls: %v", gs["tls"])
	}

	// The REST API section is untouched by the addition.
	api := section(t, cfg, "api-server")
	if api["address"] != ":7890" {
		t.Errorf("api-server.address = %v, want :7890", api["address"])
	}
}

func TestBootstrapConfigHasNoGNMIServerWithoutPort(t *testing.T) {
	for name, api := range map[string]*gnmicv1alpha1.APIConfig{
		"nil api":   nil,
		"no port":   {RestPort: 7890},
		"zero port": {RestPort: 7890, GNMIPort: 0},
	} {
		cluster := &gnmicv1alpha1.Cluster{}
		cluster.Name = "c1"
		cluster.Spec.API = api
		if _, ok := renderConfig(t, cluster)["gnmi-server"]; ok {
			t.Errorf("%s: gnmi-server rendered without a gnmiPort", name)
		}
	}
}

func TestBootstrapConfigGNMIServerTLSFollowsAPITLS(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{}
	cluster.Name = "c1"
	cluster.Spec.API = &gnmicv1alpha1.APIConfig{
		RestPort: 7890,
		GNMIPort: 9393,
		TLS:      &gnmicv1alpha1.ClusterTLSConfig{IssuerRef: "ca-issuer"},
	}

	cfg := renderConfig(t, cluster)
	tls, ok := section(t, cfg, "gnmi-server")["tls"].(map[any]any)
	if !ok {
		t.Fatalf("gnmi-server.tls missing with api.tls set: %v", cfg["gnmi-server"])
	}
	if tls["cert-file"] != gnmic.CertFilePath || tls["key-file"] != gnmic.KeyFilePath {
		t.Errorf("gnmi-server.tls = %v, want the API server certificate", tls)
	}
	if _, clientAuth := tls["client-auth"]; clientAuth {
		t.Errorf("gnmi-server must not require client certificates: %v", tls)
	}
	// The REST API still does; the two listeners differ exactly here.
	apiTLS, _ := section(t, cfg, "api-server")["tls"].(map[any]any)
	if apiTLS["client-auth"] != "require-verify" {
		t.Errorf("api-server.tls.client-auth = %v, want require-verify", apiTLS["client-auth"])
	}
}
