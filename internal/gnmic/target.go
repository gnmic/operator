package gnmic

import (
	"maps"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// buildTargetConfig creates a gNMIc TargetConfig from a Target and TargetProfile.
// clientTLS contains paths to client certificates for mTLS with targets (from
// cluster.Spec.ClientTLS).
func buildTargetConfig(target *gnmicv1alpha1.Target, profile *gnmicv1alpha1.TargetProfileSpec, creds *Credentials, clientTLS *ClientTLSPaths) *gapi.TargetConfig {
	config := &gapi.TargetConfig{
		Name:       target.Namespace + Delimiter + target.Name,
		Address:    target.Spec.Address,
		Timeout:    durationOrDefault(&profile.Timeout, 10*time.Second),
		RetryTimer: durationOrDefault(&profile.RetryTimer, 2*time.Second),
		Encoding:   ptr.To(profile.Encoding),
	}

	applyProfileConnection(config, profile)
	applyCredentials(config, creds)
	applyTargetTLS(config, profile.TLS, clientTLS)

	return config
}

// applyProfileConnection maps the TargetProfile fields that describe how to reach a
// target, independently of TLS.
//
// These used to sit after the TLS decision, every branch of which returned early, so
// the keepalives only reached a collector when cluster clientTLS and profile TLS
// happened to both be set — never in the plaintext default, which is the common case.
// proxy, gzipCompression and labels were never mapped at all, despite having a home in
// TargetConfig.
func applyProfileConnection(config *gapi.TargetConfig, profile *gnmicv1alpha1.TargetProfileSpec) {
	if profile.Proxy != "" {
		config.Proxy = profile.Proxy
	}
	if profile.GzipCompression {
		config.Gzip = ptr.To(true)
	}
	if len(profile.Labels) > 0 {
		// gNMIc's event-tags are attached to every event produced from the target,
		// which is what the CRD field documents: "labels to add to the target's
		// updates". Cloned so the plan never aliases the informer cache's map.
		config.EventTags = maps.Clone(profile.Labels)
	}
	if profile.TCPKeepAlive != nil {
		config.TCPKeepalive = profile.TCPKeepAlive.Duration
	}
	if profile.GRCPKeepAlive != nil {
		config.GRPCKeepalive = &gapi.ClientKeepalive{
			Time:                profile.GRCPKeepAlive.Time.Duration,
			Timeout:             profile.GRCPKeepAlive.Timeout.Duration,
			PermitWithoutStream: profile.GRCPKeepAlive.PermitWithoutStream,
		}
	}
}

// applyCredentials copies whichever credentials the profile's secret supplied.
func applyCredentials(config *gapi.TargetConfig, creds *Credentials) {
	if creds == nil {
		return
	}
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

// applyTargetTLS decides how the collector talks TLS to a target.
//
//   - neither side configured: plaintext.
//   - profile TLS only: TLS with no client certificate and no CA to verify against,
//     so verification is skipped.
//   - cluster clientTLS: present the client certificate, and verify only when a CA
//     bundle came with it.
func applyTargetTLS(config *gapi.TargetConfig, profileTLS *gnmicv1alpha1.TargetTLSConfig, clientTLS *ClientTLSPaths) {
	if clientTLS == nil {
		if profileTLS == nil {
			config.Insecure = ptr.To(true)
			return
		}
		config.SkipVerify = ptr.To(true)
		applyProfileTLSOptions(config, profileTLS)
		return
	}

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
	if profileTLS != nil {
		applyProfileTLSOptions(config, profileTLS)
	}
}

// applyProfileTLSOptions copies the TLS knobs a TargetProfile can set.
//
// serverName is included here rather than only on the clientTLS path: it selects the
// SNI the collector sends, which is meaningful with or without verification, and
// leaving it out silently ignored a field the profile had set.
func applyProfileTLSOptions(config *gapi.TargetConfig, profileTLS *gnmicv1alpha1.TargetTLSConfig) {
	if profileTLS.ServerName != "" {
		config.TLSServerName = profileTLS.ServerName
	}
	if profileTLS.MaxVersion != "" {
		config.TLSMaxVersion = profileTLS.MaxVersion
	}
	if profileTLS.MinVersion != "" {
		config.TLSMinVersion = profileTLS.MinVersion
	}
	if len(profileTLS.CipherSuites) > 0 {
		config.CipherSuites = profileTLS.CipherSuites
	}
}

func durationOrDefault(duration *metav1.Duration, defaultDuration time.Duration) time.Duration {
	if duration != nil {
		return duration.Duration
	}
	return defaultDuration
}
