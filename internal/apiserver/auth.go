package apiserver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// authenticate checks every method the TargetSource configures. When both
// bearer and signature are set, both must pass. No auth configured is a
// rejection: the schema requires auth when the webhook is enabled, and this
// is the backstop if that rule is ever bypassed.
func (a *APIServer) authenticate(ctx context.Context, req *http.Request, ts *gnmicv1alpha1.TargetSource, body []byte) error {
	auth := ts.Spec.Webhook.Auth
	if auth == nil || (auth.Bearer == nil && auth.Signature == nil) {
		return errors.New("webhook has no authentication configured")
	}
	if auth.Bearer != nil {
		if err := a.verifyBearer(ctx, req, ts.Namespace, auth.Bearer); err != nil {
			return err
		}
	}
	if auth.Signature != nil {
		if err := a.verifySignature(ctx, req, ts.Namespace, auth.Signature, body); err != nil {
			return err
		}
	}
	return nil
}

func (a *APIServer) verifyBearer(ctx context.Context, req *http.Request, namespace string, spec *gnmicv1alpha1.WebhookBearerAuth) error {
	const prefix = "Bearer "
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	if !strings.HasPrefix(header, prefix) {
		return errors.New("missing bearer token")
	}
	expected, err := a.secretValue(ctx, namespace, spec.SecretRef)
	if err != nil {
		return err
	}
	presented := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if subtle.ConstantTimeCompare([]byte(presented), []byte(strings.TrimSpace(expected))) != 1 {
		return errors.New("bearer token mismatch")
	}
	return nil
}

func (a *APIServer) verifySignature(ctx context.Context, req *http.Request, namespace string, spec *gnmicv1alpha1.WebhookSignatureAuth, body []byte) error {
	headerName := spec.Header
	if headerName == "" {
		headerName = "X-Hook-Signature"
	}
	presented := strings.TrimSpace(req.Header.Get(headerName))
	if presented == "" {
		return fmt.Errorf("missing %s header", headerName)
	}
	secret, err := a.secretValue(ctx, namespace, spec.SecretRef)
	if err != nil {
		return err
	}
	var mac hash.Hash
	switch spec.Algorithm {
	case "", "sha256":
		mac = hmac.New(sha256.New, []byte(secret))
		presented = strings.TrimPrefix(presented, "sha256=")
	case "sha512":
		mac = hmac.New(sha512.New, []byte(secret))
		presented = strings.TrimPrefix(presented, "sha512=")
	default:
		return fmt.Errorf("unsupported signature algorithm %q", spec.Algorithm)
	}
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(presented)
	if err != nil {
		return errors.New("signature is not hex")
	}
	if !hmac.Equal(want, got) {
		return errors.New("signature mismatch")
	}
	return nil
}

func (a *APIServer) secretValue(ctx context.Context, namespace string, ref gnmicv1alpha1.SecretKeyReference) (string, error) {
	var s corev1.Secret
	if err := a.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &s); err != nil {
		return "", fmt.Errorf("reading Secret %s/%s: %w", namespace, ref.Name, err)
	}
	v, ok := s.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("Secret %s/%s has no key %q", namespace, ref.Name, ref.Key)
	}
	return string(v), nil
}
