/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TargetProfileSpec defines the desired state of TargetProfile
type TargetProfileSpec struct {
	// The credentials of the target
	// username, password or token keys in the secret referenced by the field
	CredentialsRef string `json:"credentialsRef,omitempty"`

	// Target TLS configuration
	TLS *TargetTLSConfig `json:"tls,omitempty"`

	// Target connection timeout
	Timeout metav1.Duration `json:"timeout,omitempty"`

	// default is 2 seconds
	// +kubebuilder:default="2s"
	// +kubebuilder:validation:XValidation:rule="self == '' || duration(self) >= duration('2s')",message="RetryTimer must be at least 2 seconds"
	RetryTimer metav1.Duration `json:"retryTimer,omitempty"`

	// The gNMI Subscription encoding (JSON, BYTES, PROTO, ASCII, JSON_IETF)
	// +kubebuilder:validation:Enum=JSON;BYTES;PROTO;ASCII;JSON_IETF
	Encoding string `json:"encoding,omitempty"`

	// The labels to add to the target's updates
	Labels map[string]string `json:"labels,omitempty"`

	// The proxy to use to connect to the target
	Proxy string `json:"proxy,omitempty"`

	// Whether to use gzip compression
	GzipCompression bool `json:"gzipCompression,omitempty"`

	// The TCP keep-alive interval
	TCPKeepAlive *metav1.Duration `json:"tcpKeepAlive,omitempty"`

	// The gRPC keep-alive configuration
	GRCPKeepAlive *GRPCKeepAliveConfig `json:"grpcKeepAlive,omitempty"`
}

type TargetTLSConfig struct {
	// TLS serverName override value
	ServerName string `json:"serverName,omitempty"`
	// TLS Maximum version: 1.1, 1.2 or 1.3
	MaxVersion string `json:"maxVersion,omitempty"`
	// TLS Minimum version: 1.1, 1.2 or 1.3
	MinVersion string `json:"minVersion,omitempty"`
	// List of supported TLS cipher suites
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

type GRPCKeepAliveConfig struct {
	// gRPC keep alive time (interval)
	Time metav1.Duration `json:"time,omitempty"`
	// gRPC keep alive timeout
	Timeout metav1.Duration `json:"timeout,omitempty"`
	// If true gRPC keepalives are sent when there is no active stream
	PermitWithoutStream bool `json:"permitWithoutStream,omitempty"`
}

// TargetProfileStatus defines the observed state of TargetProfile
type TargetProfileStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Credentials",type=string,JSONPath=`.spec.credentialsRef`

// TargetProfile is the Schema for the targetprofiles API
type TargetProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetProfileSpec   `json:"spec,omitempty"`
	Status TargetProfileStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TargetProfileList contains a list of TargetProfile
type TargetProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TargetProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TargetProfile{}, &TargetProfileList{})
}
