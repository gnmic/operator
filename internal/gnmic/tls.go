package gnmic

import (
	"os"

	gnmicv1alpha1 "github.com/gnmic/gnmic-operator/api/v1alpha1"
)

const (
	clientAuthRequireVerify = "require-verify"

	// Paths for gNMIc pods TLS (server certificates for REST/gNMI API)
	CertFilesBasePath = "/etc/gnmic/tls"
	CertFilePath      = CertFilesBasePath + "/tls.crt"
	KeyFilePath       = CertFilesBasePath + "/tls.key"
	CAFilePath        = CertFilesBasePath + "/ca.crt"

	// Paths for gNMIc pods tunnel TLS (server certificates for gRPC tunnel)
	TunnelCertFilesBasePath = "/etc/gnmic/tunnel-tls"
	TunnelCertFilePath      = TunnelCertFilesBasePath + "/tls.crt"
	TunnelKeyFilePath       = TunnelCertFilesBasePath + "/tls.key"
	TunnelCAFilePath        = TunnelCertFilesBasePath + "/ca.crt" // TODO: consider pointing to a directory instead of a file instead of a file

	// Paths for gNMIc pods client TLS (client certificates for connecting to targets)
	ClientTLSCertFilesBasePath = "/etc/gnmic/client-tls"
	ClientTLSCertFilePath      = ClientTLSCertFilesBasePath + "/tls.crt"
	ClientTLSKeyFilePath       = ClientTLSCertFilesBasePath + "/tls.key"
	ClientTLSCAFilePath        = ClientTLSCertFilesBasePath + "/ca.crt"

	// Path where client CA bundle is mounted in gNMIc pods (for verifying target server certs)
	ClientCABundleMountPath = "/etc/gnmic/client-ca"
	ClientCABundleFilePath  = ClientCABundleMountPath + "/ca.crt"

	// Path where tunnel CA bundle is mounted in gNMIc pods (for verifying tunnel client certs)
	TunnelCABundleMountPath = "/etc/gnmic/tunnel-ca"
	TunnelCABundleFilePath  = TunnelCABundleMountPath + "/ca.crt"

	// Path where controller's CA is mounted in gNMIc pods (for verifying controller client certs)
	ControllerCAMountPath = "/etc/gnmic/controller-ca"
	ControllerCAFilePath  = ControllerCAMountPath + "/ca.crt"

	// Default paths for controller client certificates
	// These can be overridden via environment variables
	DefaultControllerCertPath = "/etc/gnmic-operator/certs/tls.crt"
	DefaultControllerKeyPath  = "/etc/gnmic-operator/certs/tls.key"
	DefaultControllerCAPath   = "/etc/gnmic-operator/ca/ca.crt"
)

// GetControllerCertPath returns the path to the controller's client certificate
func GetControllerCertPath() string {
	if path := os.Getenv("GNMIC_TLS_CERT"); path != "" {
		return path
	}
	return DefaultControllerCertPath
}

// GetControllerKeyPath returns the path to the controller's client key
func GetControllerKeyPath() string {
	if path := os.Getenv("GNMIC_TLS_KEY"); path != "" {
		return path
	}
	return DefaultControllerKeyPath
}

// GetControllerCAPath returns the path to the CA certificate for verifying gNMIc pods
func GetControllerCAPath() string {
	if path := os.Getenv("GNMIC_TLS_CA"); path != "" {
		return path
	}
	return DefaultControllerCAPath
}

type TLSConfig struct {
	CAFile     string `json:"ca-file,omitempty" yaml:"ca-file,omitempty"`
	CertFile   string `json:"cert-file,omitempty" yaml:"cert-file,omitempty"`
	KeyFile    string `json:"key-file,omitempty" yaml:"key-file,omitempty"`
	SkipVerify bool   `json:"skip-verify,omitempty" yaml:"skip-verify,omitempty"`
	ClientAuth string `json:"client-auth,omitempty" yaml:"client-auth,omitempty"`
}

func TLSConfigForClusterPod(cluster *gnmicv1alpha1.Cluster) *TLSConfig {
	if cluster.Spec.API == nil || cluster.Spec.API.TLS == nil {
		return nil
	}
	tlsConfig := &TLSConfig{}
	if cluster.Spec.API.TLS.IssuerRef != "" {
		// server certificate for the gNMIc API
		tlsConfig.CertFile = CertFilePath
		tlsConfig.KeyFile = KeyFilePath
		// CA for verifying controller client certificates (mTLS)
		// the controller's CA is synced to the cluster namespace and mounted here
		tlsConfig.CAFile = ControllerCAFilePath
		tlsConfig.ClientAuth = clientAuthRequireVerify
	}
	return tlsConfig
}

// TunnelServerTLSConfig returns the TLS configuration for the gRPC tunnel server
func TunnelServerTLSConfig(cluster *gnmicv1alpha1.Cluster) *TLSConfig {
	if cluster.Spec.GRPCTunnel == nil || cluster.Spec.GRPCTunnel.TLS == nil {
		return nil
	}

	tlsConfig := &TLSConfig{}

	// if issuerRef is configured, use the generated certificates
	if cluster.Spec.GRPCTunnel.TLS.IssuerRef != "" {
		tlsConfig.CertFile = TunnelCertFilePath
		tlsConfig.KeyFile = TunnelKeyFilePath
	}

	// if bundleRef is configured, use it for client verification
	if cluster.Spec.GRPCTunnel.TLS.BundleRef != "" {
		tlsConfig.CAFile = TunnelCABundleFilePath
		tlsConfig.ClientAuth = clientAuthRequireVerify // TODO: consider making this configurable
	}

	return tlsConfig
}

// ClientTLSPaths holds the paths to client TLS certificates for connecting to targets
type ClientTLSPaths struct {
	CertFile string `json:"tls-cert,omitempty" yaml:"tls-cert,omitempty"`
	KeyFile  string `json:"tls-key,omitempty" yaml:"tls-key,omitempty"`
	CAFile   string `json:"tls-ca,omitempty" yaml:"tls-ca,omitempty"`
}

// ClientTLSConfigForCluster returns the client TLS paths for connecting to targets
// Returns nil if ClientTLS is not configured on the cluster
func ClientTLSConfigForCluster(cluster *gnmicv1alpha1.Cluster) *ClientTLSPaths {
	if cluster.Spec.ClientTLS == nil {
		return nil
	}

	tlsPaths := &ClientTLSPaths{}

	// if issuerRef is configured, use the generated client certificates
	if cluster.Spec.ClientTLS.IssuerRef != "" {
		tlsPaths.CertFile = ClientTLSCertFilePath
		tlsPaths.KeyFile = ClientTLSKeyFilePath
	}

	// if bundleRef is configured, use it as CA for verifying target server certificates
	if cluster.Spec.ClientTLS.BundleRef != "" {
		tlsPaths.CAFile = ClientCABundleFilePath
	}

	return tlsPaths
}
