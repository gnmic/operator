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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OutputSpec defines the desired state of Output
type OutputSpec struct {
	// The output type
	// +kubebuilder:validation:Enum=file;kafka;prometheus;prometheus_write;nats;jetstream;influxdb;tcp;udp
	Type string `json:"type"`
	// The output-specific config object.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Config apiextensionsv1.JSON `json:"config,omitempty"`
	// The service configuration for outputs that expose an endpoint (e.g., prometheus).
	// If not specified, a ClusterIP service will be created by default.
	// +optional
	Service *OutputServiceSpec `json:"service,omitempty"`
	// ServiceRef references a Kubernetes Service to use as the output address.
	// Supported for: nats, jetstream, kafka outputs.
	// The service address will be resolved and injected into the output config.
	// +optional
	ServiceRef *ServiceReference `json:"serviceRef,omitempty"`
	// ServiceSelector selects Kubernetes Services by labels to use as output addresses.
	// Supported for: nats, jetstream, kafka outputs.
	// All matching service addresses will be resolved and injected into the output config.
	// +optional
	ServiceSelector *ServiceSelector `json:"serviceSelector,omitempty"`
}

// ServiceReference references a specific Kubernetes Service
type ServiceReference struct {
	// Name of the Service
	Name string `json:"name"`
	// Namespace of the Service. Defaults to the Output's namespace if not specified.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Port is the name or number of the port to use.
	// If not specified, the first port of the service is used.
	// +optional
	Port string `json:"port,omitempty"`
}

// ServiceSelector selects Services by labels
type ServiceSelector struct {
	// MatchLabels is a map of {key,value} pairs to match services
	MatchLabels map[string]string `json:"matchLabels"`
	// Namespace to search for services. Defaults to the Output's namespace if not specified.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Port is the name or number of the port to use.
	// If not specified, the first port of the service is used.
	// +optional
	Port string `json:"port,omitempty"`
}

// OutputServiceSpec defines the service configuration for outputs that expose an endpoint
type OutputServiceSpec struct {
	// Type specifies the Kubernetes service type (ClusterIP, NodePort, LoadBalancer)
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default=ClusterIP
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// Annotations to add to the service
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels to add to the service
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// OutputStatus defines the observed state of Output
type OutputStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// Output is the Schema for the outputs API
type Output struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OutputSpec   `json:"spec,omitempty"`
	Status OutputStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OutputList contains a list of Output
type OutputList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Output `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Output{}, &OutputList{})
}
