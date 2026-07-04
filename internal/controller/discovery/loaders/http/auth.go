package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
)

// fetchSecret uses the configured ResourceFetcher to resolve secret values.
func (l *Loader) fetchSecret(ctx context.Context, sel *corev1.SecretKeySelector) (string, error) {
	if l.loaderCfg.ResourceFetcher == nil {
		return "", nil
	}
	return l.loaderCfg.ResourceFetcher.GetSecretKey(ctx, l.loaderCfg.TargetsourceNN.Namespace, sel)
}

func (l *Loader) applyAuthentication(req *http.Request) error {
	auth := l.spec.Authentication
	if auth == nil {
		return nil
	}

	if auth.Basic != nil {
		return l.applyBasicAuth(req, auth.Basic.CredentialSecretRef)
	}

	if auth.Token != nil {
		return l.applyTokenAuth(req, auth.Token.Scheme, auth.Token.TokenSecretRef)
	}

	return fmt.Errorf("no supported authentication method configured")
}

// applyBasicAuth applies Basic authentication using the provided secret selector.
// Returns an error when credentials are missing or cannot be parsed.
func (l *Loader) applyBasicAuth(req *http.Request, sel *corev1.SecretKeySelector) error {
	if sel == nil {
		return fmt.Errorf("Basic auth enabled but no valid credentials provided")
	}

	val, err := l.fetchSecret(req.Context(), sel)
	if err != nil {
		return err
	}

	var cm map[string]string
	if err := json.Unmarshal([]byte(val), &cm); err != nil {
		return err
	}

	username := cm["username"]
	password := cm["password"]
	if username == "" && password == "" {
		return fmt.Errorf("Basic auth enabled but no valid credentials provided")
	}

	req.SetBasicAuth(username, password)
	return nil
}

// applyTokenAuth applies token-based authentication using the provided secret selector
// Returns an error when no valid token is found
func (l *Loader) applyTokenAuth(req *http.Request, scheme string, sel *corev1.SecretKeySelector) error {
	if sel == nil {
		return fmt.Errorf("Token auth enabled but no valid token secret reference provided")
	}

	token, err := l.fetchSecret(req.Context(), sel)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("%s %s", scheme, token))
	return nil
}
