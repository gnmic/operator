package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
)

func TestBuildHTTPClientCases(t *testing.T) {
	caPEM, err := genSelfSignedCertPEM()
	if err != nil {
		t.Fatalf("failed to generate CA PEM: %v", err)
	}

	tests := []struct {
		name       string
		spec       gnmicv1alpha1.HTTPConfig
		fetcher    core.ResourceFetcher
		expectsErr bool
	}{
		{
			name: "valid_CABundle",
			spec: gnmicv1alpha1.HTTPConfig{
				TLS:     &gnmicv1alpha1.ClientTLSConfig{CABundleRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "test-ca"}, Key: "ca.crt"}},
				Timeout: &metav1.Duration{Duration: 10 * time.Second},
			},
			fetcher:    fakeResourceFetcher{configuration: caPEM},
			expectsErr: false,
		},
		{
			name: "invalid_CABundle_PEM",
			spec: gnmicv1alpha1.HTTPConfig{
				TLS:     &gnmicv1alpha1.ClientTLSConfig{CABundleRef: &corev1.ConfigMapKeySelector{}},
				Timeout: &metav1.Duration{Duration: 10 * time.Second},
			},
			fetcher:    fakeResourceFetcher{configuration: "not-pem"},
			expectsErr: true,
		},
		{
			name:       "CABundle_without_fetcher",
			spec:       gnmicv1alpha1.HTTPConfig{TLS: &gnmicv1alpha1.ClientTLSConfig{CABundleRef: &corev1.ConfigMapKeySelector{}}, Timeout: &metav1.Duration{Duration: 10 * time.Second}},
			fetcher:    nil,
			expectsErr: true,
		},
		{
			name:       "timeout_missing",
			spec:       gnmicv1alpha1.HTTPConfig{},
			fetcher:    nil,
			expectsErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loader := makeLoader(tc.spec, tc.fetcher)
			client, err := loader.buildHTTPClient(context.Background())
			if tc.expectsErr {
				if err == nil {
					t.Fatalf("%s: expected error, got nil", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			if client == nil {
				t.Fatalf("%s: expected client, got nil", tc.name)
			}
			if tc.name == "valid_CABundle" {
				transport, _ := client.Transport.(*http.Transport)
				if transport == nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
					t.Fatalf("%s: expected TLS RootCAs to be set", tc.name)
				}
			}
		})
	}
}

func TestFetchPageErrorsAndJSON(t *testing.T) {
	// method missing
	loader := &Loader{loaderCfg: core.CommonLoaderConfig{TargetsourceNN: types.NamespacedName{Namespace: "default", Name: "test"}}, spec: gnmicv1alpha1.HTTPConfig{Timeout: &metav1.Duration{Duration: 10 * time.Second}}}
	if _, err := loader.fetchPage(context.Background(), nil, "http://example.com", logr.Discard()); err == nil {
		t.Fatalf("expected method configuration error")
	}

	// non-200 and invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer server.Close()

	loader = makeLoader(gnmicv1alpha1.HTTPConfig{Method: http.MethodGet, Timeout: &metav1.Duration{Duration: 10 * time.Second}}, nil)
	client := mustBuildClient(t, loader)
	if _, err := loader.fetchPage(context.Background(), client, server.URL, logr.Discard()); err == nil {
		t.Fatalf("expected status code error")
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json"))
	})
	if _, err := loader.fetchPage(context.Background(), client, server.URL, logr.Discard()); err == nil {
		t.Fatalf("expected JSON decode error")
	}
}

func TestFetchPagePOSTAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// validate method and headers/body
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-Custom") != "value" {
			t.Fatalf("missing header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body decode failed: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"name": "target1"})
	}))
	defer server.Close()

	spec := gnmicv1alpha1.HTTPConfig{URL: server.URL, Method: http.MethodPost, Headers: map[string]string{"X-Custom": "value"}, Body: `{"query":"status"}`, Timeout: &metav1.Duration{Duration: 10 * time.Second}}
	loader := makeLoader(spec, nil)
	client := mustBuildClient(t, loader)
	raw, err := loader.fetchPage(context.Background(), client, server.URL, logr.Discard())
	if err != nil {
		t.Fatalf("fetchPage failed: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok || resp["name"] != "target1" {
		t.Fatalf("unexpected response: %#v", raw)
	}
}
