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
	// Provider defines the source of targets for this TargetSource
	// Only one provider can be specified per TargetSource
	// +kubebuilder:validation:Required
	Provider *ProviderSpec `json:"provider"`

	// TODO: implement in message processor
	// Optional port to use for discovered targets if not specified by the provider
	// +kubebuilder:validation:Optional
	TargetPort int32 `json:"targetPort,omitempty"`

	// Optional labels to apply to all targets discovered by this TargetSource
	// +kubebuilder:validation:Optional
	TargetLabels map[string]string `json:"targetLabels,omitempty"`

	// The TargetProfile to use for targets discovered by this TargetSource
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TargetProfile string `json:"targetProfile"`
}

// ProviderSpec defines the source of targets for a TargetSource
// Only one provider can be specified per TargetSource
// +kubebuilder:validation:ExactlyOneOf=http
type ProviderSpec struct {
	// HTTP defines the configuration for a HTTP provider
	HTTP *HTTPConfig `json:"http,omitempty"`
}

// HTTPConfig defines the configuration for the HTTP provider
// +kubebuilder:validation:AtLeastOneOf=url;acceptPush
type HTTPConfig struct {
	// URL of the HTTP endpoint to pull targets from
	// If defined, the loader will periodically poll this endpoint for targets
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`

	// If true, the loader will accept pushed target updates to the controller endpoint
	// The endpoint will be /{namespace}/{targetsource}/<todo>
	// +kubebuilder:default=false
	// +kubebuilder:validation:Optional
	AcceptPush bool `json:"acceptPush,omitempty"`

	// Optional authorization configuration for accessing the HTTP endpoint
	// +kubebuilder:validation:Optional
	Authorization *AuthorizationSpec `json:"authorization,omitempty"`

	// Optional interval for polling the HTTP endpoint for targets
	// TODO: increase default value
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Optional
	PollInterval *metav1.Duration `json:"interval,omitempty"`

	// Optional timeout for HTTP requests to the endpoint
	// +kubebuilder:default="10s"
	// +kubebuilder:validation:Optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Optional TLS configuration for connecting to the HTTP endpoint
	// If it is an HTTP endpoint, this will be ignored
	// +kubebuilder:validation:Optional
	TLS *ClientTLSConfig `json:"tls,omitempty"`

	// Field name in the JSON response that contains the list of items (targets).
	// Must refer to a top-level key in the response object.
	// If not specified, the entire response is expected to be a list of items.
	// Example: "results"
	// +kubebuilder:validation:Optional
	ItemsField string `json:"itemsField,omitempty"`

	// Optional pagination configuration for parsing responses from the HTTP endpoint
	// +kubebuilder:validation:Optional
	Pagination *PaginationSpec `json:"pagination,omitempty"`

	// Optional mapping configuration for parsing responses from the HTTP endpoint
	// +kubebuilder:validation:Optional
	ResponseMapping *ResponseMappingSpec `json:"mapping,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!(has(self.caBundle) && has(self.caBundleSecretRef))",message="caBundle and caBundleSecretRef are mutually exclusive"
type ClientTLSConfig struct {
	// Skip TLS verification of the Provider's certificate.
	// +kubebuilder:default:=false
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// Base64-encoded bundle of PEM CAs which will be used to validate the certificate
	// chain presented by the Provider. Only used if using HTTPS to connect to Provider and
	// ignored for HTTP connections.
	// Mutually exclusive with CABundleSecretRef.
	// +optional
	CABundle []byte `json:"caBundle,omitempty"`

	// Reference to a Secret containing a bundle of PEM-encoded CAs to use when
	// verifying the certificate chain presented by the Provider when using HTTPS.
	// Mutually exclusive with CABundle.
	CABundleSecretRef *corev1.SecretKeySelector `json:"caBundleSecretRef,omitempty"`
}

// AuthorizationSpec defines the configuration for authentication
// +kubebuilder:validation:ExactlyOneOf=basic;token
type AuthorizationSpec struct {
	// Basic authentication configuration
	Basic *BasicAuthSpec `json:"basic,omitempty"`
	// Token-based authentication configuration
	Token *TokenAuthSpec `json:"token,omitempty"`
	// JWT   *JWTAuthSpec   `json:"jwt,omitempty"`
	// MTLS
}

// BasicAuthSpec defines the configuration for basic authentication
// Enforce EITHER inline creds OR secret ref
// +kubebuilder:validation:XValidation:rule="(has(self.credentialsSecretRef) && !has(self.username) && !has(self.password)) || (!has(self.credentialsSecretRef) && has(self.username) && has(self.password))",message="either credentialsSecretRef OR both username and password must be set, but not a mix"
type BasicAuthSpec struct {
	// Username for basic auth
	// Mutually exclusive with CredentialsSecretRef.
	Username string `json:"username,omitempty"`
	// Password for basic auth
	// Mutually exclusive with CredentialsSecretRef.
	Password string `json:"password,omitempty"`

	// Reference to a Secret containing "username" and "password" keys to use for
	// basic authentication when connecting to the Provider.
	// Mutually exclusive with Username and Password.
	CredentialsSecretRef *corev1.SecretKeySelector `json:"credentialsSecretRef,omitempty"`
}

// TokenAuthSpec defines the configuration for token-based authentication
// +kubebuilder:validation:XValidation:rule="has(self.token) != has(self.tokenSecretRef)",message="either token or tokenSecretRef must be set, but not both"
type TokenAuthSpec struct {
	// Scheme for the token, e.g. "Bearer"
	// +kubebuilder:validation:MinLength=1
	Scheme string `json:"scheme"`
	// Token value for authentication
	// Mutually exclusive with TokenSecretRef.
	Token string `json:"token,omitempty"`
	// Reference to a Secret containing a key with the token value to use for
	// authentication when connecting to the Provider.
	// Mutually exclusive with Token.
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`
}

// +kubebuilder(disabled):validation:XValidation:rule="!((has(self.token) || has(self.tokenSecretRef)) && (has(self.key) || has(self.signingKeySecretRef) || has(self.claims)))",message="static JWT token and generated JWT configuration cannot be combined"
// +kubebuilder(disabled):validation:XValidation:rule="!has(self.signingKeySecretRef) || self.algorithm != \"\"",message="algorithm must be specified when generating a JWT"
// type JWTAuthSpec struct {
// 	// Static pre-generated JWT
// 	Token          string                    `json:"token,omitempty"`
// 	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`
// 	// Optional: generate JWT dynamically
// 	Claims              map[string]string         `json:"claims,omitempty"`
// 	Key                 string                    `json:"key,omitempty"`
// 	SigningKeySecretRef *corev1.SecretKeySelector `json:"signingKeySecretRef,omitempty"`
// 	// HS256, RS256, ES256, etc.
// 	Algorithm string           `json:"algorithm,omitempty"`
// 	TTL       *metav1.Duration `json:"ttl,omitempty"`
// }

// PaginationSpec defines the configuration for paginating through responses from providers
type PaginationSpec struct {
	// Field name in the JSON response that contains the next page reference.
	// The value can be either:
	// - a full URL (used directly for the next request), or
	// - a pagination token (appended as a query parameter using this field name as the key).
	//
	// Must refer to a top-level key in the response object.
	// Example: "next" or "nextToken"
	NextField string `json:"nextField,omitempty"`
}

// JSONPath-style expressions to extract target fields from the response
// and map them to the corresponding Target fields.
type ResponseMappingSpec struct {
	// JSONPath expression to extract the target name from the response
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// JSONPath expression to extract the target IP from the response
	// +kubebuilder:validation:Required
	IP string `json:"ip"`

	// JSONPath expression to extract the target port from the response
	// +kubebuilder:validation:Optional
	Port string `json:"port,omitempty"`

	// JSONPath expression to extract the target labels from the response
	// The extracted labels will be merged with the static TargetLabels defined in the TargetSourceSpec,
	// with values from the response taking precedence in case of conflicts.
	// +kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`

	// JSONPath expression to extract the target profile from the response
	// +kubebuilder:validation:Optional
	TargetProfile string `json:"targetProfile,omitempty"`
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
