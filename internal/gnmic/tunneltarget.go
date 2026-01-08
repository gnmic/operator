package gnmic

import (
	"time"

	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	"k8s.io/utils/ptr"
)

// buildTunnelTargetMatch creates a TunnelTargetMatch from a TunnelTargetPolicy and TargetProfile
// TODO: finish mapping fields from profile to config, reuse the same function as for Targets
func buildTunnelTargetMatch(
	policySpec *gnmicv1alpha1.TunnelTargetPolicySpec,
	profile *gnmicv1alpha1.TargetProfileSpec,
	creds *Credentials,
) *TunnelTargetMatch {
	match := &TunnelTargetMatch{}

	// set match criteria from the policy
	if policySpec.Match != nil {
		match.Type = policySpec.Match.Type
		match.ID = policySpec.Match.ID
	}

	// build target config from profile
	if profile != nil {
		config := &gapi.TargetConfig{
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

		match.Config = config
	}

	return match
}
