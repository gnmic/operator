package discovery

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	http "github.com/gnmic/operator/internal/controller/discovery/loaders/http"
)

// NewLoader creates a loader by name
func NewLoader(ctx context.Context, c client.Client, cfg *core.CommonLoaderConfig, spec gnmicv1alpha1.TargetSourceSpec) (core.Loader, error) {

	switch {
	case spec.Provider.HTTP != nil:
		httpSpec := *spec.Provider.HTTP
		cfg.AcceptPush = httpSpec.AcceptPush

		// TODO: watch secrets -> if secret changes reconcile has to be executed
		if httpSpec.Authorization != nil {
			if err := resolveAuthorizationIntoSpec(
				ctx,
				c,
				cfg.TargetsourceNN.Namespace,
				httpSpec.Authorization,
			); err != nil {
				return nil, err
			}
		}

		return http.New(*cfg, httpSpec), nil
	default:
		return nil, fmt.Errorf("unknown targetsource provider, check TargetSource CRD for %s", cfg.TargetsourceNN)
	}

}

func resolveAuthorizationIntoSpec(
	ctx context.Context,
	c client.Client,
	namespace string,
	authSpec *gnmicv1alpha1.AuthorizationSpec,
) error {
	if authSpec == nil {
		return nil
	}
	auth := authSpec

	switch {
	case auth.Basic != nil:
		b := auth.Basic

		if b.CredentialsSecretRef != nil {
			values, err := GetSecretValues(
				ctx,
				c,
				namespace,
				b.CredentialsSecretRef.Name,
				"username",
				"password",
			)
			if err != nil {
				return err
			}
			b.Username = values["username"]
			b.Password = values["password"]
		}

	case auth.Token != nil:
		t := auth.Token
		if t.TokenSecretRef != nil {
			values, err := GetSecretValues(
				ctx,
				c,
				namespace,
				t.TokenSecretRef.Name,
				"token",
			)
			if err != nil {
				return err
			}
			t.Token = values["token"]
		}

		// case auth.JWT != nil:
		// 	jwt := auth.JWT
		// 	if jwt.TokenSecretRef != nil {
		// 		values, err := GetSecretValues(
		// 			ctx,
		// 			c,
		// 			namespaceName,
		// 			jwt.TokenSecretRef.Name,
		// 			"token",
		// 		)
		// 		if err != nil {
		// 			return err
		// 		}
		// 		jwt.Token = values[jwt.TokenSecretRef.Key]
		// 	}
		// 	if jwt.SigningKeySecretRef != nil {
		// 		values, err := GetSecretValues(
		// 			ctx,
		// 			c,
		// 			namespaceName,
		// 			jwt.SigningKeySecretRef.Name,
		// 			"key",
		// 		)
		// 		if err != nil {
		// 			return err
		// 		}
		// 		jwt.Key = values[jwt.SigningKeySecretRef.Key]

		// 	}
	}

	return nil
}
