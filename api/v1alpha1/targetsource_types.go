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

// TargetSourceSpec defines the desired state of TargetSource
// +kubebuilder:validation:Required
type TargetSourceSpec struct {
	Provider *ProviderSpec `json:"provider"`
	// +kubebuilder:validation:Optional
	Webhook WebhookSpec `json:"webhook,omitempty"`
	// +kubebuilder:validation:Optional
	TargetLabels map[string]string `json:"targetLabels,omitempty"`

	// +kubebuilder:validation:Optional
	RestartPolicy *RestartPolicySpec `json:"restartPolicy,omitempty"`

	// +kubebuilder:validation:MinLength=1
	TargetProfile string `json:"targetProfile"`
}

// +kubebuilder:validation:ExactlyOneOf=http;consul
type ProviderSpec struct {
	HTTP   *HTTPConfig   `json:"http,omitempty"`
	Consul *ConsulConfig `json:"consul,omitempty"`
}

type WebhookSpec struct {
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`
}

type HTTPConfig struct {
	// +kubebuilder:validation:MinLength=1
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

type ConsulConfig struct {
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url,omitempty"`
}

type RestartPolicySpec struct {
	// +kubebuilder:validation:Optional
	MaxRestarts *int `json:"maxRestarts,omitempty"`

	// +kubebuilder:validation:Optional
	BackoffSeconds *int `json:"backoffSeconds,omitempty"`
}

// TargetSourceStatus defines the observed state of TargetSource
type TargetSourceStatus struct {
	Status       string      `json:"status"`
	TargetsCount int32       `json:"targetsCount"`
	LastSync     metav1.Time `json:"lastSync"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TargetSource is the Schema for the targetsources API
type TargetSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetSourceSpec   `json:"spec,omitempty"`
	Status TargetSourceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TargetSourceList contains a list of TargetSource
type TargetSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TargetSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TargetSource{}, &TargetSourceList{})
}
