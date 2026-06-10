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

	// Optional port to use for discovered targets if not specified by the provider
	// +kubebuilder:validation:Optional
	TargetPort int32 `json:"targetPort,omitempty"`

	// Optional labels to apply to all targets discovered by this TargetSource
	// +kubebuilder:validation:Optional
	TargetLabels map[string]string `json:"targetLabels,omitempty"`

	// Optional TargetProfile to use for targets discovered by this TargetSource if not specified by the provider
	// +kubebuilder:validation:Optional
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
// +kubebuilder:validation:AtLeastOneOf:=url;push
type HTTPConfig struct {
	// URL of the HTTP endpoint to pull targets from
	// If defined, the loader will periodically poll this endpoint for targets
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`

	// HTTP method used for the request.
	//
	// Defaults to GET if not specified.
	//
	// Supported values:
	// - GET  (default, no request body)
	// - POST (supports request body)
	//
	// +kubebuilder:validation:Enum=GET;POST
	// +kubebuilder:default="GET"
	// +kubebuilder:validation:Optional
	Method string `json:"method,omitempty"`

	// Optional HTTP headers to include in the request.
	//
	// These map directly to HTTP headers (key-value pairs).
	//
	// Example:
	//   headers:
	//     Content-Type: application/json
	//     X-Custom-Header: value
	//
	// Precedence:
	// - Authentication configuration overrides any conflicting headers e.g. Authorization
	//
	// +kubebuilder:validation:Optional
	Headers map[string]string `json:"headers,omitempty"`

	// Optional raw request body.
	//
	// Typically used with POST requests and contains JSON payload.
	//
	// Example:
	//   body: |
	//     {
	//       "limit": 100,
	//       "status": "active"
	//     }
	//
	// Notes:
	// - Ignored for GET requests
	// - User must set appropriate Content-Type header if needed
	//
	// +kubebuilder:validation:Optional
	Body string `json:"body,omitempty"`

	// Optional authentication configuration for accessing the HTTP endpoint
	// +kubebuilder:validation:Optional
	Authentication *AuthenticationSpec `json:"authentication,omitempty"`

	// Optional interval for polling the HTTP endpoint for targets
	// TODO: document about default value
	// +kubebuilder:default="30m"
	// +kubebuilder:validation:Optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Optional timeout for HTTP requests to the endpoint
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Optional TLS configuration for connecting to the HTTP endpoint
	// If it is an HTTP endpoint, this will be ignored
	// +kubebuilder:validation:Optional
	TLS *ClientTLSConfig `json:"tls,omitempty"`

	// Optional pagination configuration for parsing responses from the HTTP endpoint
	// +kubebuilder:validation:Optional
	Pagination *PaginationSpec `json:"pagination,omitempty"`

	// Optional mapping configuration for parsing responses from the HTTP endpoint
	// +kubebuilder:validation:Optional
	ResponseMapping *ResponseMappingSpec `json:"mapping,omitempty"`

	// Optional configuration to enable push
	// +kubebuilder:validation:Optional
	Push *PushSpec `json:"push,omitempty"`
}

type ClientTLSConfig struct {
	// Skip TLS verification of the Provider's certificate.
	// +kubebuilder:default:=false
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// Reference to a ConfigMap containing a bundle of PEM-encoded CAs to use when
	// verifying the certificate chain presented by the Provider when using HTTPS.
	// Mutually exclusive with CABundle.
	// +kubebuilder:validation:Optional
	CABundleRef *corev1.ConfigMapKeySelector `json:"caBundleRef,omitempty"`
}

// AuthenticationSpec defines the configuration for authentication
// +kubebuilder:validation:ExactlyOneOf=basic;token
type AuthenticationSpec struct {
	// Basic authentication configuration
	Basic *BasicAuthSpec `json:"basic,omitempty"`
	// Token-based authentication configuration
	Token *TokenAuthSpec `json:"token,omitempty"`
}

// BasicAuthSpec defines the configuration for basic authentication
type BasicAuthSpec struct {
	// Reference to a Secret containing "username" and "password" keys to use for
	// basic authentication when connecting to the Provider.
	// +kubebuilder:validation:Required
	CredentialSecretRef *corev1.SecretKeySelector `json:"credentialSecretRef"`
}

// TokenAuthSpec defines the configuration for token-based authentication
type TokenAuthSpec struct {
	// Scheme for the token, e.g. "Bearer"
	// +kubebuilder:validation:MinLength=1
	Scheme string `json:"scheme"`
	// Reference to a Secret containing a key with the token value to use for
	// authentication when connecting to the Provider.
	// Mutually exclusive with Token.
	// +kubebuilder:validation:Required
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`
}

// PaginationSpec defines how pagination is handled for HTTP APIs.
//
// The pagination mechanism is fully server-driven. The loader will repeatedly:
//  1. Extract the "next" reference from the response
//  2. Use it to construct the next request
//  3. Continue until no next reference is returned
//
// Supported pagination styles:
//  1. Cursor-based:
//     - Response returns a token (e.g. "next_page_token")
//     - Client sends it back via a query parameter (e.g. "page_token")
//  2. URL-based (nextLink):
//     - Response returns a full URL
//     - Client follows it directly without modification
//  3. Expression-based extraction:
//     - The next reference is extracted using a CEL expression
//     - This allows access to nested fields or special keys
//     (e.g. "@odata.nextLink")
//
// Behavior:
//   - If the extracted value is a full URL, it will be used as-is
//   - Otherwise, it is treated as a token and appended using RequestParam
//   - The token is treated as opaque and must not be interpreted
//
// Example:
//
//	pagination:
//	  nextField: "self.next_page_token"
//	  requestParam: "page_token"
//
//	pagination:
//	  nextField: "self['@odata.nextLink']"
type PaginationSpec struct {
	// CEL expression used to extract the next page reference from the response.
	//
	// The expression is evaluated with:
	//   self -> full JSON response
	//
	// It must evaluate to either:
	//   - string (full URL OR token), or
	//   - null (indicates end of pagination)
	//
	// Examples:
	//   "self.next"
	//   "self.next_page_token"
	//   "self['@odata.nextLink']"
	//
	// +kubebuilder:validation:Optional
	NextField string `json:"nextField,omitempty"`

	// Query parameter name used when the extracted value is a token.
	//
	// Required for token-based pagination.
	// Ignored when NextField resolves to a full URL.
	//
	// Example:
	//   requestParam: "page_token"
	//
	// +kubebuilder:validation:Optional
	RequestParam string `json:"requestParam,omitempty"`
}

// ResponseMappingSpec controls how targets are extracted from an HTTP JSON response.
//
// This allows you to map fields from a JSON API into targets using either:
//   - simple direct field access (e.g. item["name"])
//   - or CEL expressions for more advanced logic
//
// General behavior:
//
//  1. Selecting targets:
//     - `targetsField` is a CEL expression that selects the list of targets
//     - It runs once on the full response (`self`) and MUST return a list
//     - If not set, the response itself must be a JSON array
//
//  2. Extracting fields:
//     - Each field (name, address, port, labels, etc.) is handled independently
//     - If a CEL expression is provided → it is evaluated
//     - If not provided → the value is read directly from the target object
//
//  3. Available variables in CEL:
//     - item -> the current target object
//     - self -> the full HTTP response JSON
//
// Example:
//
//	Response:
//	{
//	  "results": [
//	    { "name": "device1", "ip": "10.0.0.1", "env": "prod" }
//	  ],
//	  "meta": { "region": "eu-west" }
//	}
//
//	Mapping:
//	targetsField: "self.results"
//
//	name: ""            # direct → item["name"]
//	address: "item.ip"  # CEL
//
//	labels:
//	  env:    "item.env"
//	  region: "self.meta.region"
type ResponseMappingSpec struct {
	// CEL expression that selects the list of target objects from the response.
	//
	// This is evaluated once using:
	//   self -> full JSON response
	//
	// Example:
	//   targetsField: "self.results"
	//
	// If not set, the response itself must be a JSON array with the targets.
	//
	// +kubebuilder:validation:Optional
	TargetsField string `json:"targetsField,omitempty"`

	// CEL expression for the target name.
	//
	// If not set, defaults to:
	//   item["name"]
	//
	// Example:
	//   "item.hostname"
	//
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`

	// CEL expression for the target address.
	//
	// If not set, defaults to:
	//   item["address"]
	//
	// Example:
	//   "item.ip"
	//
	// +kubebuilder:validation:Optional
	Address string `json:"address,omitempty"`

	// CEL expression for the target port.
	//
	// If not set, defaults to:
	//   item["port"]
	//
	// Example:
	//   "item.port"
	//
	// +kubebuilder:validation:Optional
	Port string `json:"port,omitempty"`

	// CEL expression that returns a map of labels.
	// The expression must evaluate to an object (map).
	//
	// Example:
	//
	//   labels: |
	//     {
	//       "env": item.environment,
	//       "region": self.meta.region,
	//       item.dynamicKey: "value"
	//     }
	//
	// If not set, defaults to:
	//   item["labels"]
	//
	// The resulting map will be converted into labels.
	// The extracted labels will be merged with the static TargetLabels defined in the TargetSourceSpec,
	// with values from the response taking precedence in case of conflicts.
	//
	// +kubebuilder:validation:Optional
	Labels string `json:"labels,omitempty"`

	// CEL expression for the target profile.
	//
	// If not set, defaults to:
	//   item["targetProfile"]
	//
	// Example:
	//   "item.type == 'edge' ? 'edge-profile' : 'default'"
	//
	// +kubebuilder:validation:Optional
	TargetProfile string `json:"targetProfile,omitempty"`
}

// PushSpec defines the settings for event-based update mechanism (i.e. webhooks sent from the server)
type PushSpec struct {
	// Enables the applyTargets endpoint; when disabled, incoming requests are rejected.
	// +kubebuilder:validation:Required
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Enables Authentication; when empty, authentication is not checked
	// +kubebuilder:validation:Optional
	Auth *PushAuthSpec `json:"auth,omitempty"`

	// Enables Signature verification; when empty, signature is not checked
	// +kubebuilder:validation:Optional
	Signature *PushSignatureSpec `json:"signature,omitempty"`
}

// +kubebuilder:validation:Optional
type PushAuthSpec struct {
	// Spec for Bearer Token authentication
	Bearer *PushBearerAuthSpec `json:"bearer,omitempty"`
}

// +kubebuilder:validation:Required
type PushBearerAuthSpec struct {
	// Reference to a secret containing a token that is checked to the Bearer token found in the
	// authorization header of the incoming HTTP request.
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`
}

// +kubebuilder:validation:Required
type PushSignatureSpec struct {
	// Reference to a secret containing a signature key. HMAC calculated with secret and HTTP body
	// to calculate X-Hook-Signature. Compared with X-Hook-Signature in HTTP header
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty"`

	// Enum to select hashing algorithm during HMAC calculation
	// +kubebuilder:default="sha512"
	// +kubebuilder:validation:Enum=sha256;sha512
	Algorithm string `json:"algorithm"`
}


// TargetSourceStatus defines the observed state of TargetSource
type TargetSourceStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	TargetsCount       int32              `json:"targetsCount,omitempty"`
	LastSync           metav1.Time        `json:"lastSync,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
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
