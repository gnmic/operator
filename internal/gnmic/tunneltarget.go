package gnmic

import (
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	"k8s.io/utils/ptr"
)

// buildTunnelTargetMatch creates a TunnelTargetMatch from a TunnelTargetPolicy and
// TargetProfile. clientTLS contains paths to client certificates for mTLS with
// targets (from cluster.Spec.ClientTLS).
//
// The profile is mapped through the same helpers as a static Target, so a tunnel
// target and a declared one built from the same TargetProfile get the same
// connection settings. The hand-inlined copy this replaces had drifted: it never
// mapped the keepalives at all.
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

	if profile == nil {
		return match
	}

	// A tunnel target's name and address are supplied by the device when it dials
	// in, so only the profile-derived settings are configured here.
	config := &gapi.TargetConfig{
		Timeout:    durationOrDefault(&profile.Timeout, 10*time.Second),
		RetryTimer: durationOrDefault(&profile.RetryTimer, 2*time.Second),
		Encoding:   ptr.To(profile.Encoding),
	}

	applyProfileConnection(config, profile)
	applyCredentials(config, creds)
	applyTargetTLS(config, profile.TLS, clientTLS)

	match.Config = config
	return match
}
