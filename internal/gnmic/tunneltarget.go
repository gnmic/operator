package gnmic

import (
	"time"

	gnmicv1alpha1 "github.com/gnmic/gnmic-operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	"k8s.io/utils/ptr"
)

// buildTunnelTargetMatch creates a TunnelTargetMatch from a TunnelTargetPolicy and TargetProfile
// clientTLS contains paths to client certificates for mTLS with targets (from cluster.Spec.ClientTLS)
// TODO: finish mapping fields from profile to config, reuse the same function as for Targets
func buildTunnelTargetMatch(
	policySpec *gnmicv1alpha1.TunnelTargetPolicySpec,
	profile *gnmicv1alpha1.TargetProfileSpec,
	creds *Credentials,
	clientTLS *ClientTLSPaths,
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

		// no client TLS configuration at the cluster level or target profile level
		if clientTLS == nil && profile.TLS == nil {
			config.Insecure = ptr.To(true)
			match.Config = config
			return match
		}
		// TLS not enabled at the cluster level but enabled at the target profile level
		if clientTLS == nil && profile.TLS != nil {
			config.SkipVerify = ptr.To(true)
			if profile.TLS.MaxVersion != "" {
				config.TLSMaxVersion = profile.TLS.MaxVersion
			}
			if profile.TLS.MinVersion != "" {
				config.TLSMinVersion = profile.TLS.MinVersion
			}
			if len(profile.TLS.CipherSuites) > 0 {
				config.CipherSuites = profile.TLS.CipherSuites
			}
			match.Config = config
			return match
		}

		// use client TLS configuration from cluster (for mTLS with targets)
		if clientTLS.CertFile != "" {
			config.TLSCert = ptr.To(clientTLS.CertFile)
		}
		if clientTLS.KeyFile != "" {
			config.TLSKey = ptr.To(clientTLS.KeyFile)
		}
		if clientTLS.CAFile != "" {
			config.TLSCA = ptr.To(clientTLS.CAFile)
			config.SkipVerify = ptr.To(false)
		} else {
			// TLS is enabled but without CA verification (TrustBundleRef not supported yet)
			config.SkipVerify = ptr.To(true)
		}
		if profile.TLS == nil {
			match.Config = config
			return match
		}
		if profile.TLS.ServerName != "" {
			config.TLSServerName = profile.TLS.ServerName
		}
		if profile.TLS.MaxVersion != "" {
			config.TLSMaxVersion = profile.TLS.MaxVersion
		}
		if profile.TLS.MinVersion != "" {
			config.TLSMinVersion = profile.TLS.MinVersion
		}
		if len(profile.TLS.CipherSuites) > 0 {
			config.CipherSuites = profile.TLS.CipherSuites
		}
		match.Config = config
	}

	return match
}
