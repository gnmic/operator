package gnmic

import (
	"time"

	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// buildTargetConfig creates a gNMIc TargetConfig from a Target and TargetProfile
func buildTargetConfig(target *gnmicv1alpha1.Target, profile *gnmicv1alpha1.TargetProfileSpec, creds *Credentials) *gapi.TargetConfig {
	config := &gapi.TargetConfig{
		Name:       target.Namespace + Delimiter + target.Name,
		Address:    target.Spec.Address,
		Timeout:    durationOrDefault(&profile.Timeout, 10*time.Second),
		RetryTimer: durationOrDefault(&profile.RetryTimer, 2*time.Second),
		Encoding:   ptr.To(profile.Encoding),
	}

	// set credentials if provided
	if creds != nil {
		if creds.Username != "" {
			config.Username = ptr.To(creds.Username)
		}
		if creds.Password != "" {
			config.Password = ptr.To(creds.Password)
		}
		if creds.Token != "" {
			config.Token = ptr.To(creds.Token)
		}
	}

	// TLS configuration
	if profile.TLS != nil {
		if profile.TLS.TrustBundleRef == "" {
			config.SkipVerify = ptr.To(true)
		}
	} else {
		config.Insecure = ptr.To(true)
	}

	return config
}

func durationOrDefault(duration *metav1.Duration, defaultDuration time.Duration) time.Duration {
	if duration != nil {
		return duration.Duration
	}
	return defaultDuration
}
