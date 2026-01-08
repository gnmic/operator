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

// TunnelTargetPolicySpec defines the desired state of TunnelTargetPolicy
type TunnelTargetPolicySpec struct {
	// The match criteria for the target
	// Not required, if not set, the policy will match all targets.
	Match *tunnelTargetMatch `json:"match,omitempty"`
	// The target profile to use for the matching targets
	// kubebuilder:validation:Required
	Profile string `json:"profile"`
}

type tunnelTargetMatch struct {
	// A regex to match the target type received in the gRPC tunnel Register Target RPC.
	Type string `json:"type"`
	// A regex to match the target ID received in the gRPC tunnel Register Target RPC.
	ID string `json:"id"`
}

// TunnelTargetPolicyStatus defines the observed state of TunnelTargetPolicy.
type TunnelTargetPolicyStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// TunnelTargetPolicy is the Schema for the tunneltargetpolicies API
type TunnelTargetPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TunnelTargetPolicy
	// +required
	Spec TunnelTargetPolicySpec `json:"spec"`

	// status defines the observed state of TunnelTargetPolicy
	// +optional
	Status TunnelTargetPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TunnelTargetPolicyList contains a list of TunnelTargetPolicy
type TunnelTargetPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TunnelTargetPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TunnelTargetPolicy{}, &TunnelTargetPolicyList{})
}
