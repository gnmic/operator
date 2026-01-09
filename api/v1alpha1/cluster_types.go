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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// The gNMIc image to use
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// The number of replicas to run
	// +kubebuilder:validation:Required
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas"`

	// The REST/gNMI server configuration
	API *APIConfig `json:"api,omitempty"`

	// The TLS configuration for the gNMI client certificates
	// If not set, the gNMI client certificates are not enabled.
	ClientTLS *ClusterTLSConfig `json:"clientTLS,omitempty"`

	// The gRPC tunnel server endpoint configuration
	// If not set, the gRPC tunnel server is not enabled on this cluster.
	GRPCTunnel *GRPCTunnelConfig `json:"grpcTunnel,omitempty"`

	// The resources requests and limits for the gNMIc pods
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Environment variables to set in the gNMIc pods
	Env []corev1.EnvVar `json:"env,omitempty"`
}

type APIConfig struct {
	// The port for the REST API
	// +kubebuilder:default=7890
	RestPort int32 `json:"restPort"`
	// The port for the gNMI Server
	// exposed by the gNMIc pods.
	// If not set, the gNMI server is not enabled.
	GNMIPort int32 `json:"gnmiPort,omitempty"`
	// The TLS configuration for the REST and gNMI servers
	// If not set, the TLS is not enabled.
	TLS *ClusterTLSConfig `json:"tls,omitempty"`
}

type GRPCTunnelConfig struct {
	// The port for the gRPC tunnel
	Port int32 `json:"port"`
	// The TLS configuration for the gRPC tunnel
	TLS *ClusterTLSConfig `json:"tls,omitempty"`
	// The service configuration for the gRPC tunnel that will be exposed to the clients
	Service *ServiceConfig `json:"service,omitempty"`
}

type ServiceConfig struct {
	// Type specifies the Kubernetes service type (ClusterIP, NodePort, LoadBalancer)
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default=LoadBalancer
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// Annotations to add to the service
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// TLS configuration for the gNMIc pods API or gNMI client certificates
type ClusterTLSConfig struct {
	// A CertManager Issuer user to sign the gNMIc pods API or gNMI client certificates.
	IssuerRef string `json:"issuerRef,omitempty"`
	// Additional trusted CA bundle to mount to the gNMIc pods API or gNMI client certificates.
	BundleRef string `json:"bundleRef,omitempty"`
	// If true the operator will use CertManager CSI driver to request and mount the pods API or gNMI client certificates.
	UseCSIDriver bool `json:"useCSIDriver,omitempty"`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// The number of ready replicas
	ReadyReplicas int32 `json:"readyReplicas"`
	// The number of pipelines referencing this cluster
	PipelinesCount int32 `json:"pipelinesCount"`
	// The number of targets referenced by the pipelines
	TargetsCount int32 `json:"targetsCount"`
	// The number of subscriptions referenced by the pipelines
	SubscriptionsCount int32 `json:"subscriptionsCount"`
	// The number of inputs referenced by the pipelines
	InputsCount int32 `json:"inputsCount"`
	// The number of outputs referenced by the pipelines
	OutputsCount int32 `json:"outputsCount"`
	// The conditions of the cluster
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Pipelines",type=integer,JSONPath=`.status.pipelinesCount`
// +kubebuilder:printcolumn:name="Targets",type=integer,JSONPath=`.status.targetsCount`
// +kubebuilder:printcolumn:name="Subs",type=integer,JSONPath=`.status.subscriptionsCount`
// +kubebuilder:printcolumn:name="Inputs",type=integer,JSONPath=`.status.inputsCount`
// +kubebuilder:printcolumn:name="Outputs",type=integer,JSONPath=`.status.outputsCount`

// Cluster is the Schema for the clusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
