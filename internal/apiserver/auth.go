package apiserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	corev1 "k8s.io/api/core/v1"
)

const (
	apiAuthSecretName = "gnmic-api-auth"
	apiAuthSecretKey  = "bearer-token"
)

func (a *APIServer) verifyAuthentication(ctx *gin.Context, targetSourceSpec *gnmicv1alpha1.TargetSourceSpec, registry core.DiscoveryRegistryValue) bool {
	if targetSourceSpec.Provider.HTTP.Push.Auth == nil {
		return false
	}
	if targetSourceSpec.Provider.HTTP.Push.Auth.Bearer != nil {
		return a.verifyBearerToken(ctx, registry)
	}
	if targetSourceSpec.Provider.HTTP.Push.Signature != nil {
		return a.verifySignature(ctx)
	}
	return false
}

// verifySignature verifies Signature
func (a *APIServer) verifySignature(ctx *gin.Context) bool {
	return false
}

// verifyBearerToken verifies bearer token from authorization header with value stored in kubernetes secret.
func (a *APIServer) verifyBearerToken(ctx *gin.Context, registryValue core.DiscoveryRegistryValue) bool {
	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
		return false
	}

	tokenSecret, err := getBearerToken(registryValue.CommonLoaderConfig.ResourceFetcher)
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

// getBearerToken returns bearer token stored as kubernetes secret.
func getBearerToken(resourceFetcher core.ResourceFetcher) ([]byte, error) {
	namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	selector := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: apiAuthSecretName},
		Key:                  apiAuthSecretKey,
	}

	token, err := resourceFetcher.GetSecretKey(ctx, namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s key %q: %w", namespace, apiAuthSecretName, apiAuthSecretKey, err)
	}
	return []byte(token), nil
}
