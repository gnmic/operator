package apiserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	apiAuthSecretName = "gnmic-api-auth"
	apiAuthSecretKey  = "bearer-token"
)

func (a *APIServer) InitializeAuthToken(ctx context.Context) error {
	if a.bearerToken != "" {
		return nil
	}

	bearerToken, err := ensureBearerToken(ctx, a.clusterReconciler, "")
	if err != nil {
		return err
	}
	a.bearerToken = bearerToken
	return nil
}

func ensureBearerToken(ctx context.Context, clusterReconciler *controller.ClusterReconciler, providedToken string) (string, error) {
	if strings.TrimSpace(providedToken) != "" {
		return strings.TrimSpace(providedToken), nil
	}

	namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if namespace == "" {
		namespace = "gnmic-system"
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	secretKey := types.NamespacedName{Name: apiAuthSecretName, Namespace: namespace}
	secret := &corev1.Secret{}
	if err := clusterReconciler.Get(ctx, secretKey, secret); err == nil {
		token := strings.TrimSpace(string(secret.Data[apiAuthSecretKey]))
		if token == "" {
			return "", fmt.Errorf("secret %s/%s exists but %q is empty", namespace, apiAuthSecretName, apiAuthSecretKey)
		}
		return token, nil
	} else if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("failed to get secret %s/%s: %w", namespace, apiAuthSecretName, err)
	}

	token, err := generateBearerToken()
	if err != nil {
		return "", err
	}

	toCreate := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiAuthSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			apiAuthSecretKey: token,
		},
	}

	if err := clusterReconciler.Create(ctx, toCreate); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("failed to create secret %s/%s: %w", namespace, apiAuthSecretName, err)
		}

		if err := clusterReconciler.Get(ctx, secretKey, secret); err != nil {
			return "", fmt.Errorf("failed to get existing secret %s/%s after create race: %w", namespace, apiAuthSecretName, err)
		}
		token = strings.TrimSpace(string(secret.Data[apiAuthSecretKey]))
		if token == "" {
			return "", fmt.Errorf("secret %s/%s exists but %q is empty", namespace, apiAuthSecretName, apiAuthSecretKey)
		}
	}

	return token, nil
}

func generateBearerToken() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate bearer token: %w", err)
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

func (a *APIServer) checkBearerToken(ctx *gin.Context) bool {
	if a.bearerToken == "" {
		ctx.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "api authentication is not initialized"})
		return false
	}

	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
		return false
	}

	token := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if token == "" {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return false
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(a.bearerToken)) != 1 {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
		return false
	}

	return true
}
