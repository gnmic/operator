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

// TargetSourceSpec defines the desired state of TargetSource
// +kubebuilder:validation:Required
type TargetSourceSpec struct {
	Provider *ProviderSpec `json:"provider"`

	// +kubebuilder:validation:Optional
	TargetLabels map[string]string `json:"targetLabels,omitempty"`

	// +kubebuilder:validation:MinLength=1
	TargetProfile string `json:"targetProfile"`
}

// +kubebuilder:validation:ExactlyOneOf=http
type ProviderSpec struct {
	HTTP *HTTPConfig `json:"http,omitempty"`
}

// +kubebuilder:validation:AtLeastOneOf=url;acceptPush
type HTTPConfig struct {
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`
	// +kubebuilder:validation:Optional
	Authorization *AuthorizationSpec `json:"authorization,omitempty"`
	// TODO: increase default value
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Optional
	PollInterval *metav1.Duration `json:"interval,omitempty"`
	// +kubebuilder:default="10s"
	// +kubebuilder:validation:Optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +kubebuilder:validation:Optional
	TLS *ClientTLSConfig `json:"tls,omitempty"`
	// +kubebuilder:validation:Optional
	Pagination *PaginationSpec `json:"pagination,omitempty"`
	// +kubebuilder:validation:Optional
	ResponseMapping *ResponseMappingSpec `json:"mapping,omitempty"`
	// +kubebuilder:default=false
	// +kubebuilder:validation:Optional
	AcceptPush bool `json:"acceptPush,omitempty"`
}

type ClientTLSConfig struct {
	InsecureSkipVerify bool                     `json:"insecureSkipVerify,omitempty"`
	CASecretRef        *corev1.SecretKeySelector `json:"caSecretRef,omitempty"`
}

// +kubebuilder:validation:ExactlyOneOf=basic;jwt;token
type AuthorizationSpec struct {
	Basic *BasicAuthSpec `json:"basic,omitempty"`
	Token *TokenAuthSpec `json:"token,omitempty"`
	JWT   *JWTAuthSpec   `json:"jwt,omitempty"`
}

// Enforce EITHER inline creds OR secret ref
// +kubebuilder:validation:XValidation:rule="(has(self.credentialsSecretRef) && !has(self.username) && !has(self.password)) || (!has(self.credentialsSecretRef) && has(self.username) && has(self.password))",message="either credentialsSecretRef OR both username and password must be set, but not a mix"
type BasicAuthSpec struct {
	Username             string                    `json:"username,omitempty"`
	Password             string                    `json:"password,omitempty"`
	CredentialsSecretRef *corev1.SecretKeySelector `json:"credentialsSecretRef,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.token) != has(self.tokenSecretRef)",message="either token or tokenSecretRef must be set, but not both"
type TokenAuthSpec struct {
	// +kubebuilder:validation:MinLength=1
	Scheme         string                    `json:"scheme"`
	Token          string                    `json:"token,omitempty"`
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!((has(self.token) || has(self.tokenSecretRef)) && (has(self.signingKeySecretRef) || has(self.claims)))",message="static JWT token and generated JWT configuration cannot be combined"
// +kubebuilder:validation:XValidation:rule="!((has(self.token) || has(self.tokenSecretRef)) && (has(self.signingKeySecretRef) || has(self.claims)))",message="static JWT token and generated JWT configuration cannot be combined"
// +kubebuilder:validation:XValidation:rule="!has(self.signingKeySecretRef) || self.algorithm != \"\"",message="algorithm must be specified when generating a JWT"
type JWTAuthSpec struct {
	// Static pre-generated JWT
	Token          string                    `json:"token,omitempty"`
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`
	// Optional: generate JWT dynamically
	Claims              map[string]string         `json:"claims,omitempty"`
	SigningKeySecretRef *corev1.SecretKeySelector `json:"signingKeySecretRef,omitempty"`
	// HS256, RS256, ES256, etc.
	Algorithm string           `json:"algorithm,omitempty"`
	TTL       *metav1.Duration `json:"ttl,omitempty"`
}

type PaginationSpec struct {
	// Example: "results"
	ItemsField string `json:"itemsField,omitempty"`
	// Example: "next"
	NextField string `json:"nextField,omitempty"`
}

// JSONPath-style expressions
type ResponseMappingSpec struct {
	Name    string            `json:"name"`
	Address string            `json:"address"`
	Port    string            `json:"port,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
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
