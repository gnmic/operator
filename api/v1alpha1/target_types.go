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

// TargetStatus defines the observed state of Target.
// A single Target may be collected by multiple Clusters (via different Pipelines),
// so the status is reported per-cluster.
type TargetStatus struct {
	// Number of clusters currently collecting this target.
	Clusters int32 `json:"clusters"`
	// Aggregate connection state across all clusters.
	// READY if all clusters report READY, DEGRADED if any do not.
	// Empty when no clusters are collecting this target.
	ConnectionState string `json:"connectionState,omitempty"`
	// Per-cluster target state, keyed by Cluster CR name.
	// A target may be collected by multiple clusters (via different pipelines).
	// +optional
	ClusterStates map[string]ClusterTargetState `json:"clusterStates,omitempty"`
}

// ClusterTargetState represents the state of a target on a specific gNMIc cluster pod.
type ClusterTargetState struct {
	// The pod within the cluster that currently owns this target.
	Pod string `json:"pod"`
	// The target's operational state (starting, running, stopping, stopped, failed).
	State string `json:"state,omitempty"`
	// The reason for failure when state is "failed".
	// +optional
	FailedReason string `json:"failedReason,omitempty"`
	// The gNMI connection state (CONNECTING, READY, TRANSIENT_FAILURE, etc.).
	ConnectionState string `json:"connectionState,omitempty"`
	// Per-subscription state (subscription name -> running/stopped).
	// +optional
	Subscriptions map[string]string `json:"subscriptions,omitempty"`
	// When this state was last updated by the gNMIc pod.
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.spec.address`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profile`
// +kubebuilder:printcolumn:name="Clusters",type=integer,JSONPath=`.status.clusters`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.connectionState`

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
