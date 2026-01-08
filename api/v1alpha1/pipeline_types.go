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

// PipelineSpec defines the desired state of Pipeline
type PipelineSpec struct {
	// The cluster to assign the pipeline to
	ClusterRef string `json:"clusterRef"`
	// Whether the pipeline is enabled
	Enabled bool `json:"enabled,omitempty"`

	// The selector for the targets
	TargetSelectors []metav1.LabelSelector `json:"targetSelectors,omitempty"`
	// The targets to assign to the pipeline
	TargetRefs []string `json:"targetRefs,omitempty"`

	// The selector for the gRPC tunnel target policies
	TunnelTargetPolicySelectors []metav1.LabelSelector `json:"tunnelTargetPolicySelectors,omitempty"`
	// The gRPC tunnel target policies to assign to the pipeline
	TunnelTargetPolicyRefs []string `json:"tunnelTargetPolicyRefs,omitempty"`

	// The selector for the subscriptions
	SubscriptionSelectors []metav1.LabelSelector `json:"subscriptionSelectors,omitempty"`
	// The subscriptions to assign to the pipeline
	SubscriptionRefs []string `json:"subscriptionRefs,omitempty"`

	// The selector for the outputs
	Outputs OutputSelector `json:"outputs,omitempty"`

	// The selector for the inputs
	Inputs InputSelector `json:"inputs,omitempty"`

	// The labels to add to the exported data
	Labels map[string]string `json:"labels,omitempty"`
}

type OutputSelector struct {
	// The selector for the outputs
	OutputSelectors []metav1.LabelSelector `json:"outputSelectors,omitempty"`
	// The outputs to assign to the pipeline
	OutputRefs []string `json:"outputRefs,omitempty"`

	// The selector for the processors
	ProcessorSelectors []metav1.LabelSelector `json:"processorSelectors,omitempty"`
	// The processors to assign to the pipeline
	ProcessorRefs []string `json:"processorRefs,omitempty"`
}

type InputSelector struct {
	// The selector for the inputs
	InputSelectors []metav1.LabelSelector `json:"inputSelectors,omitempty"`
	// The inputs to assign to the pipeline
	InputRefs []string `json:"inputRefs,omitempty"`

	// The selector for the processors
	ProcessorSelectors []metav1.LabelSelector `json:"processorSelectors,omitempty"`
	// The processors to assign to the pipeline
	ProcessorRefs []string `json:"processorRefs,omitempty"`
}

// PipelineStatus defines the observed state of Pipeline
type PipelineStatus struct {
	Status                    string             `json:"status"`
	TargetsCount              int32              `json:"targetsCount"`
	SubscriptionsCount        int32              `json:"subscriptionsCount"`
	InputsCount               int32              `json:"inputsCount"`
	OutputsCount              int32              `json:"outputsCount"`
	TunnelTargetPoliciesCount int32              `json:"tunnelTargetPoliciesCount"`
	Conditions                []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
//+kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
//+kubebuilder:printcolumn:name="Targets",type=integer,JSONPath=`.status.targetsCount`
//+kubebuilder:printcolumn:name="Tunnel_Target_Policies",type=integer,JSONPath=`.status.tunnelTargetPoliciesCount`
//+kubebuilder:printcolumn:name="Subscriptions",type=integer,JSONPath=`.status.subscriptionsCount`
//+kubebuilder:printcolumn:name="Outputs",type=integer,JSONPath=`.status.outputsCount`
//+kubebuilder:printcolumn:name="Inputs",type=integer,JSONPath=`.status.inputsCount`
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`

// Pipeline is the Schema for the pipelines API
type Pipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PipelineSpec   `json:"spec,omitempty"`
	Status PipelineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PipelineList contains a list of Pipeline
type PipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Pipeline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Pipeline{}, &PipelineList{})
}
