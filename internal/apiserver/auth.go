package apiserver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

// verifyAuthentication checks for Bearer Token and/or Signature
func (a *APIServer) verifyAuthentication(ctx *gin.Context, registry core.DiscoveryRegistryValue, logger logr.Logger) (bool, error) {
	if registry.CommonLoaderConfig.PushConfig.Auth != nil {
		if authenticated, err := a.verifyBearerToken(ctx, registry, logger); authenticated == false {
			return false, err
		}
	}
	if registry.CommonLoaderConfig.PushConfig.Signature != nil {
		if signatureMatch, err :=  a.verifySignature(ctx, registry, logger); signatureMatch == false {
			return false, err
		}
	}
	return true, nil
}

// verifySignature verifies x-hook-signature from POST header with hmac from body and a kubernetes secret.
func (a *APIServer) verifySignature(ctx *gin.Context, registry core.DiscoveryRegistryValue, logger logr.Logger) (bool, error) {
	signatureHeader := ctx.GetHeader("x-hook-signature")
	clc := registry.CommonLoaderConfig
	secret, err := getSecret(clc, clc.PushConfig.Signature.SecretRef.Key, clc.PushConfig.Signature.SecretRef.Name)
	
	if err != nil {
		logger.Error(err, "error calling getSecret")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false, err
	}
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		logger.Error(err, "failed to read request body")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid request body"})
		return false, err
	}
	ctx.Request.Body = io.NopCloser(bytes.NewReader(body))

	var mac hash.Hash
	if registry.CommonLoaderConfig.PushConfig.Signature.Algorithm == "sha256" {
		mac = hmac.New(sha256.New, []byte(secret))
		signatureHeader = strings.TrimSpace(strings.TrimPrefix(signatureHeader, "sha256="))
	} else {
		mac = hmac.New(sha512.New, []byte(secret))
		signatureHeader = strings.TrimSpace(strings.TrimPrefix(signatureHeader, "sha512="))
	}
	mac.Write(body)
	signatureCalculated := mac.Sum(nil)
	signatureProvided, err := hex.DecodeString(signatureHeader)
	if err != nil {
		logger.Error(err, "error decoding signatureHeader")
	}

	if hmac.Equal(signatureCalculated, signatureProvided) {
		return true, nil
	}
	err = fmt.Errorf("POST request signature does not align with signature calulcated from body and Kubernetes secret")
	logger.Error(err, "verifySignature failed")
	ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
	return false, err
}

// verifyBearerToken verifies bearer token from authorization header with value stored in kubernetes secret.
func (a *APIServer) verifyBearerToken(ctx *gin.Context, registry core.DiscoveryRegistryValue, logger logr.Logger) (bool, error) {
	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		err := fmt.Errorf("POST request has missing or invalid authorization header")
		logger.Error(err, "verifyBearerToken failed")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false, err
	}

	clc := registry.CommonLoaderConfig
	bearerSecret, err := getSecret(clc, clc.PushConfig.Auth.Bearer.TokenSecretRef.Key, clc.PushConfig.Auth.Bearer.TokenSecretRef.Name)
	if err != nil {
		logger.Error(err, "error calling getSecret")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false, err
	}

	bearerHeader := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if bearerHeader != bearerSecret {
		err := fmt.Errorf("POST request bearer is not equal to bearer stored in Kubernetes secret")
		logger.Error(err, "bearer token mismatch")
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err})
		return false, err
	}
	return true, nil
}

// getSecret returns Kubernetes Opaque secret as string
func getSecret(clc *core.CommonLoaderConfig, key string, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	selector := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
	secret, err := clc.ResourceFetcher.GetSecretKey(ctx, clc.TargetsourceNN.Namespace, selector)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s key %q: %w", clc.TargetsourceNN.Namespace, name, key, err)
	}
	return secret, nil
}
