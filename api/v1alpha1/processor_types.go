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

// ProcessorSpec defines the desired state of Processor
type ProcessorSpec struct {
	// The type of the processor
	// +kubebuilder:validation:Enum=event-add-tag;event-allow;event-combine;event-convert;event-data-convert;event-date-string;event-delete;event-drop;event-duration-convert;event-extract-tags;event-group-by;event-ieee-float32;event-jq;event-merge;event-override-ts;event-plugin;event-rate-limit;event-starlark;event-strings;event-time-epoch;event-to-tag;event-trigger;event-value-tag;event-write;
	Type string `json:"type"`
	// Config is the processor-specific config object.
	// Use x-kubernetes-preserve-unknown-fields so each processor type can carry its own schema.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Config apiextensionsv1.JSON `json:"config,omitempty"`
}

// ProcessorStatus defines the observed state of Processor
type ProcessorStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// Processor is the Schema for the processors API
type Processor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProcessorSpec   `json:"spec,omitempty"`
	Status ProcessorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ProcessorList contains a list of Processor
type ProcessorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Processor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Processor{}, &ProcessorList{})
}
