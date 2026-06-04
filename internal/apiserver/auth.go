package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

// kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=supersecret

//verifyAuthentication 
func (a *APIServer) verifyAuthentication(ctx *gin.Context, registry core.DiscoveryRegistryValue, logger logr.Logger) bool {
	if registry.CommonLoaderConfig.PushConfig.Auth != nil {
		return a.verifyBearerToken(ctx, registry, logger)
	}
	if registry.CommonLoaderConfig.PushConfig.Signature != nil {
		return a.verifySignature(ctx, registry, logger)
	}
	if registry.CommonLoaderConfig.PushConfig.Auth == nil {
		return true
	}
	return false
}

// verifySignature verifies Signature
func (a *APIServer) verifySignature(ctx *gin.Context, registry core.DiscoveryRegistryValue, logger logr.Logger) bool {
	return false
}

// verifyBearerToken verifies bearer token from authorization header with value stored in kubernetes secret.
func (a *APIServer) verifyBearerToken(ctx *gin.Context, registry core.DiscoveryRegistryValue, logger logr.Logger) bool {
	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		err := fmt.Errorf("POST request has missing or invalid authorization header")
		logger.Error(err, "verifyBearerToken failed")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false
	}

	bearerSecret, err := getSecret(registry.CommonLoaderConfig)
	if err != nil {
		logger.Error(err, "error calling getSecret")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false
	}

	bearerHeader := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if bearerHeader != bearerSecret {
		err := fmt.Errorf("POST request bearer is not equal to equal to bearer stored in Kubernetes secret")
		logger.Error(err, "bearer token mismatch")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false
	}
	return true
}

// getSecret returns Kubernetes Opaque secret as string
func getSecret(clc *core.CommonLoaderConfig) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	key := clc.PushConfig.Auth.Bearer.TokenSecretRef.Key
	name := clc.PushConfig.Auth.Bearer.TokenSecretRef.Name
	namespace := clc.TargetsourceNN.Namespace

	selector := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
	secret, err := clc.ResourceFetcher.GetSecretKey(ctx, namespace, selector)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s key %q: %w", namespace, name, key, err)
	}
	return secret, nil
}
