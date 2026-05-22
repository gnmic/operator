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

func (l *Loader) applyAuthorization(req *http.Request) error {
	auth := l.spec.Authorization
	if auth == nil {
		return nil
	}
	// Basic auth
	if auth.Basic != nil {
		// Secret-based credentials
		if auth.Basic.CredentialsSecretRef != nil {
			val, err := l.fetchSecret(req.Context(), auth.Basic.CredentialsSecretRef)
			if err != nil {
				return err
			}
			var cm map[string]string
			if err := json.Unmarshal([]byte(val), &cm); err == nil {
				username := cm["username"]
				password := cm["password"]
				if username != "" || password != "" {
					req.SetBasicAuth(username, password)
					return nil
				}
			}
			return err
		}
		return fmt.Errorf("Basic auth enabled but no valid credentials provided")
	}

	// Token-based auth: prefer secret ref if present
	if auth.Token != nil {
		if auth.Token.TokenSecretRef != nil {
			token, err := l.fetchSecret(req.Context(), auth.Token.TokenSecretRef)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", fmt.Sprintf("%s %s", auth.Token.Scheme, token))
			return nil
		}
		return fmt.Errorf("Token auth enabled but no valid token secret reference provided")
	}

	// No supported auth method configured
	return fmt.Errorf("no supported authentication method configured")
}
