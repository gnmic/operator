package apiserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	corev1 "k8s.io/api/core/v1"
)

// kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=supersecret

const (
	apiAuthSecretName = "gnmic-api-auth"
	apiAuthSecretKey  = "bearer-token"
)

func (a *APIServer) verifyAuthentication(ctx *gin.Context, registry core.DiscoveryRegistryValue) bool {
	if registry.CommonLoaderConfig.PushConfig.Auth != nil {
		return a.verifyBearerToken(ctx, registry)
	}
	if registry.CommonLoaderConfig.PushConfig.Signature != nil {
		return a.verifySignature(ctx)
	}
	if registry.CommonLoaderConfig.PushConfig.Auth == nil {
		return true
	}
	return false
}

// verifySignature verifies Signature
func (a *APIServer) verifySignature(ctx *gin.Context) bool {
	return false
}

// verifyBearerToken verifies bearer token from authorization header with value stored in kubernetes secret.
func (a *APIServer) verifyBearerToken(ctx *gin.Context, registry core.DiscoveryRegistryValue) bool {
	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
		return false
	}

	tokenSecret, err := getSecret(registry)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, err)
		return false
	}

	tokenHeader := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if subtle.ConstantTimeCompare([]byte(tokenHeader), tokenSecret) != 1 {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
		return false
	}
	return true
}

// getSecret returns bearer token stored as kubernetes secret.
func getSecret(registry core.DiscoveryRegistryValue) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	selector := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: registry.CommonLoaderConfig.PushConfig.Auth.Bearer.TokenSecretRef.Key},
		Key:                  registry.CommonLoaderConfig.PushConfig.Auth.Bearer.TokenSecretRef.Key,
	}

	token, err := registry.CommonLoaderConfig.ResourceFetcher.GetSecretKey(ctx, registry.CommonLoaderConfig.TargetsourceNN.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s key %q: %w", registry.CommonLoaderConfig.TargetsourceNN.Namespace, registry.CommonLoaderConfig.PushConfig.Auth.Bearer.TokenSecretRef.Key, registry.CommonLoaderConfig.PushConfig.Auth.Bearer.TokenSecretRef.Key, err)
	}
	return []byte(token), nil
}
