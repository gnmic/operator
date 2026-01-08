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

// TargetSpec defines the desired state of Target
type TargetSpec struct {
	// The address of the target
	Address string `json:"address"`
	// The profile to use for the target
	Profile string `json:"profile"`
}

type TargetTLSConfig struct {
	IssuerRef      string `json:"issuerRef,omitempty"`
	TrustBundleRef string `json:"trustBundleRef,omitempty"`
	//
	ServerName   string   `json:"serverName,omitempty"`
	MaxVersion   string   `json:"maxVersion,omitempty"`
	MinVersion   string   `json:"minVersion,omitempty"`
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

type GRPCKeepAliveConfig struct {
	Time                metav1.Duration `json:"time,omitempty"`
	Timeout             metav1.Duration `json:"timeout,omitempty"`
	PermitWithoutStream bool            `json:"permitWithoutStream,omitempty"`
}

// TargetStatus defines the observed state of Target
type TargetStatus struct {
	// The connection state of the target
	ConnectionState  string      `json:"connectionState"`
	LastConnected    metav1.Time `json:"lastConnected"`
	LastDisconnected metav1.Time `json:"lastDisconnected"`
	LastError        string      `json:"lastError"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.spec.address`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profile`

// Target is the Schema for the targets API
type Target struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetSpec   `json:"spec,omitempty"`
	Status TargetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TargetList contains a list of Target
type TargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Target `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Target{}, &TargetList{})
}
