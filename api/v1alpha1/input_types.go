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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InputSpec defines the desired state of Input
type InputSpec struct {
	// The type of the input
	// supported types:
	// "kafka", "nats" or "jetstream"
	// +kubebuilder:validation:Enum=kafka;nats;jetstream
	Type string `json:"type"`
	// Config is the input-specific config object.
	// Use x-kubernetes-preserve-unknown-fields so each output type can carry its own schema.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Config apiextensionsv1.JSON `json:"config,omitempty"`
}

// InputStatus defines the observed state of Input
type InputStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`

// Input is the Schema for the inputs API
type Input struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InputSpec   `json:"spec,omitempty"`
	Status InputStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// InputList contains a list of Input
type InputList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Input `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Input{}, &InputList{})
}
