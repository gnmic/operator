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

// SubscriptionSpec defines the desired state of Subscription
type SubscriptionSpec struct {
	// The gNMI prefix to subscribe to
	Prefix string `json:"prefix,omitempty"`
	// The gNMI target to subscribe to
	Target string `json:"target,omitempty"`
	// The gNMI paths to subscribe to
	Paths []string `json:"paths,omitempty"`
	// The gNMI SubscriptionList mode (ONCE, STREAM/SAMPLE, STREAM/ON_CHANGE, STREAM/TARGET_DEFINED or POLL)
	// +kubebuilder:validation:Enum=ONCE;STREAM;STREAM/SAMPLE;STREAM/ON_CHANGE;STREAM/TARGET_DEFINED;POLL
	Mode string `json:"mode,omitempty"`
	// The gNMI Subscription sample interval
	SampleInterval metav1.Duration `json:"sampleInterval,omitempty"`
	// The gNMI Subscription heartbeat interval
	HeartbeatInterval metav1.Duration `json:"heartbeatInterval,omitempty"`
	// Whether to only send updates or all data
	UpdatesOnly bool `json:"updatesOnly,omitempty"`
	// The gNMI Subscription stream subscriptions
	StreamSubscriptions []string `json:"streamSubscriptions,omitempty"`
	// The gNMI Subscription depth (Depth extension)
	Depth uint32 `json:"depth,omitempty"`
	// The gNMI Subscription encoding (JSON, BYTES, PROTO, ASCII, JSON_IETF)
	// +kubebuilder:validation:Enum=JSON;BYTES;PROTO;ASCII;JSON_IETF
	Encoding string `json:"encoding,omitempty"`
	// The gNMI Subscription QoS (0-9)
	Qos *uint32 `json:"qos,omitempty"`
	// Whether to suppress redundant updates
	SuppressRedundant bool `json:"suppressRedundant,omitempty"`
	// The gNMI Subscription history configuration
	History *SubscriptionHistoryConfig `json:"history,omitempty"`
}

type SubscriptionHistoryConfig struct {
	// The gNMI Subscription history snapshot time
	Snapshot metav1.Time `json:"snapshot,omitempty"`
	// The gNMI Subscription history start time
	Start metav1.Time `json:"start,omitempty"`
	// The gNMI Subscription history end time
	End metav1.Time `json:"end,omitempty"`
}

// SubscriptionStatus defines the observed state of Subscription
type SubscriptionStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="SampleInterval",type=string,JSONPath=`.spec.sampleInterval`
// +kubebuilder:printcolumn:name="Encoding",type=string,JSONPath=`.spec.encoding`

// Subscription is the Schema for the subscriptions API
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubscriptionSpec   `json:"spec,omitempty"`
	Status SubscriptionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Subscription{}, &SubscriptionList{})
}
