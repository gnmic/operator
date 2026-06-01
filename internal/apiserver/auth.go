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
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	apiAuthSecretName = "gnmic-api-auth"
	apiAuthSecretKey  = "bearer-token"
)

// InitializeBearerToken creates a new bearer token in form of a Kubernetes secret, only if it doesn't exist yet.
func (a *APIServer) InitializeBearerToken(ctx context.Context) error {
	if bearerTokenExists(a.clusterReconciler) {
		return nil
	}
	err := createBearerToken(ctx, a.clusterReconciler)
	if err != nil {
		return err
	}
	return nil
}

// createBearerToken creates a new Opaque kubernetes secret
func createBearerToken(ctx context.Context, clusterReconciler *controller.ClusterReconciler) error {
	logger := log.FromContext(ctx).WithValues("component", "apiserver")
	namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token, err := getStringForBearerToken()
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiAuthSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			apiAuthSecretKey: token,
		},
	}

	if err := clusterReconciler.Create(ctx, secret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create secret %s/%s: %w", namespace, apiAuthSecretName, err)
		}
	}
	logger.Info(
		"Created kubernetes auth secret",
		"secret", fmt.Sprintf("%s/%s", namespace, apiAuthSecretName),
		"key", apiAuthSecretKey,
		"namespace", namespace,
	)
	return nil
}

// getStringForBearerToken returns a base64 encoded string used for the bearer token.
func getStringForBearerToken() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate bearer token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (a *APIServer) verifyAuthentication(ctx *gin.Context, clusterReconciler *controller.ClusterReconciler, targetSource *gnmicv1alpha1.TargetSource) bool {
	var pushAuthSpec *gnmicv1alpha1.PushAuthSpec
	if targetSource.Spec.Provider != nil && targetSource.Spec.Provider.HTTP != nil && targetSource.Spec.Provider.HTTP.Push != nil {
		pushAuthSpec = targetSource.Spec.Provider.HTTP.Push.Auth
		if pushAuthSpec == nil {
			return false
		}
		if pushAuthSpec.Bearer != nil {
			return a.verifyBearerToken(ctx, clusterReconciler)
		}
		if pushAuthSpec.Signature != nil {
			return a.verifySignature(ctx, clusterReconciler)
		}
		if pushAuthSpec.NoAuthentication {
			return true
		}
	}
	return false
}

// verifyBearerToken verifies bearer token from authorization header with value stored in kubernetes secret.
func (a *APIServer) verifyBearerToken(ctx *gin.Context, clusterReconciler *controller.ClusterReconciler) bool {
	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
		return false
	}

	tokenSecret, err := getBearerToken(clusterReconciler)
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

// verifySignature verifies Signature
func (a *APIServer) verifySignature(ctx *gin.Context, clusterReconciler *controller.ClusterReconciler) bool {
	return false
}

// bearerTokenExists returns true if the bearerToken exists and false if it doesn't.
func bearerTokenExists(clusterReconciler *controller.ClusterReconciler) bool {
	_, err := getBearerToken(clusterReconciler)
	if err != nil {
		return false
	}
	return true
}

// getBearerToken returns bearer token stored as kubernetes secret.
func getBearerToken(clusterReconciler *controller.ClusterReconciler) ([]byte, error) {
	namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	var secret corev1.Secret
	if err := clusterReconciler.Get(ctx, types.NamespacedName{Name: apiAuthSecretName, Namespace: namespace}, &secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", namespace, apiAuthSecretName, err)
	}
	token, ok := secret.Data[apiAuthSecretKey]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s does not contain key %q", namespace, apiAuthSecretName, apiAuthSecretKey)
	}
	// kubectl get secret -n gnmic-system gnmic-api-auth -o jsonpath="{.data.bearer-token}" | base64 --decode
	return token, nil
}

func (a *APIServer) fetchTargetSource(ctx context.Context, key types.NamespacedName) (*gnmicv1alpha1.TargetSource, error) {
	var targetSource gnmicv1alpha1.TargetSource
	if err := a.clusterReconciler.Get(ctx, key, &targetSource); err != nil {
		return nil, err
	}
	return &targetSource, nil
}
