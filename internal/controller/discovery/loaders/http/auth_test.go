package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

func TestApplyAuthenticationCases(t *testing.T) {
	credsJSON, _ := json.Marshal(map[string]string{"username": "user", "password": "pass"})

	tests := []struct {
		name    string
		config  gnmicv1alpha1.HTTPConfig
		fetcher core.ResourceFetcher
		check   func(t *testing.T, req *http.Request, err error)
	}{
		{
			name:    "basic success",
			config:  gnmicv1alpha1.HTTPConfig{Authentication: &gnmicv1alpha1.AuthenticationSpec{Basic: &gnmicv1alpha1.BasicAuthSpec{CredentialSecretRef: &corev1.SecretKeySelector{}}}},
			fetcher: fakeResourceFetcher{secretValue: string(credsJSON)},
			check: func(t *testing.T, req *http.Request, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				user, pass, ok := req.BasicAuth()
				if !ok || user != "user" || pass != "pass" {
					t.Fatalf("basic auth not set correctly")
				}
			},
		},
		{
			name:    "basic invalid json",
			config:  gnmicv1alpha1.HTTPConfig{Authentication: &gnmicv1alpha1.AuthenticationSpec{Basic: &gnmicv1alpha1.BasicAuthSpec{CredentialSecretRef: &corev1.SecretKeySelector{}}}},
			fetcher: fakeResourceFetcher{secretValue: "invalid-json"},
			check: func(t *testing.T, req *http.Request, err error) {
				if err == nil {
					t.Fatalf("expected error for invalid json")
				}
			},
		},
		{
			name:    "token success",
			config:  gnmicv1alpha1.HTTPConfig{Authentication: &gnmicv1alpha1.AuthenticationSpec{Token: &gnmicv1alpha1.TokenAuthSpec{Scheme: "Bearer", TokenSecretRef: &corev1.SecretKeySelector{}}}},
			fetcher: fakeResourceFetcher{secretValue: "token-value"},
			check: func(t *testing.T, req *http.Request, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := req.Header.Get("Authorization"); !strings.Contains(got, "token-value") {
					t.Fatalf("token header not set: %q", got)
				}
			},
		},
		{
			name:    "token missing secret",
			config:  gnmicv1alpha1.HTTPConfig{Authentication: &gnmicv1alpha1.AuthenticationSpec{Token: &gnmicv1alpha1.TokenAuthSpec{Scheme: "Bearer"}}},
			fetcher: nil,
			check: func(t *testing.T, req *http.Request, err error) {
				if err == nil {
					t.Fatalf("expected token secret ref error")
				}
			},
		},
		{
			name:    "no method configured",
			config:  gnmicv1alpha1.HTTPConfig{Authentication: &gnmicv1alpha1.AuthenticationSpec{}},
			fetcher: nil,
			check: func(t *testing.T, req *http.Request, err error) {
				if err == nil {
					t.Fatalf("expected unsupported auth error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := makeLoader(tt.config, tt.fetcher)
			req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
			err := loader.applyAuthentication(req)
			tt.check(t, req, err)
		})
	}
}
