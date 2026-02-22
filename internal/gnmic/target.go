package gnmic

import (
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// buildTargetConfig creates a gNMIc TargetConfig from a Target and TargetProfile
// clientTLS contains paths to client certificates for mTLS with targets (from cluster.Spec.ClientTLS)
func buildTargetConfig(target *gnmicv1alpha1.Target, profile *gnmicv1alpha1.TargetProfileSpec, creds *Credentials, clientTLS *ClientTLSPaths) *gapi.TargetConfig {
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

	// no client TLS configuration at the cluster level or target profile level
	if clientTLS == nil && profile.TLS == nil {
		config.Insecure = ptr.To(true)
		return config
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
		return config
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
		return config
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
	return config
}

func durationOrDefault(duration *metav1.Duration, defaultDuration time.Duration) time.Duration {
	if duration != nil {
		return duration.Duration
	}
	return defaultDuration
}
