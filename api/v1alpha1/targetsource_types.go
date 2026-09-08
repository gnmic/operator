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

// TargetSourceSpec describes where to load targets from and what to do with them.
//
// A TargetSource reads devices from an external system on every run, turns them
// into Target resources, and removes Targets whose device is gone. Everything
// that is not specific to one kind of source lives outside Source, because it
// means the same thing for every provider.
type TargetSourceSpec struct {
	// Source describes the external system to read devices from.
	// +kubebuilder:validation:Required
	Source SourceSpec `json:"source"`

	// Interval between two discovery runs, after a successful run.
	// A random jitter of up to 10% is added so that many TargetSources
	// created at the same time do not poll at the same time.
	// After a failure the next run is scheduled with a backoff that starts at
	// Interval, doubles on each consecutive failure, and is capped at ten
	// times Interval.
	// +kubebuilder:default="5m"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('10s')",message="interval must be at least 10s"
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Timeout for a single discovery run, including all pages. A timeout
	// longer than Interval is capped at Interval, so a short interval needs no
	// matching timeout.
	// +kubebuilder:default="1m"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s')",message="timeout must be at least 1s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Suspend stops discovery. Targets already created are left alone.
	// Use it to freeze a source without deleting anything.
	// +kubebuilder:default=false
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// Target controls how a discovered device becomes a Target resource.
	// +optional
	Target TargetTemplateSpec `json:"target,omitempty"`

	// Prune controls when Targets whose device is no longer returned by the
	// source may be deleted.
	// +optional
	Prune PruneSpec `json:"prune,omitempty"`

	// MaxTargets caps how many Targets this source may manage. A run that
	// discovers more is rejected as a whole, so a broken query cannot create
	// tens of thousands of resources. 0 means no limit.
	// +kubebuilder:default=10000
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxTargets *int32 `json:"maxTargets,omitempty"`

	// Webhook enables an endpoint the source can call to request an
	// immediate run.
	// +optional
	Webhook *WebhookSpec `json:"webhook,omitempty"`
}

// SourceType names a discovery provider.
// +kubebuilder:validation:Enum=HTTP;Static;ConfigMap;Secret
type SourceType string

const (
	// SourceTypeHTTP reads a JSON or YAML document from a remote HTTP API.
	SourceTypeHTTP SourceType = "HTTP"
	// SourceTypeStatic takes the device list from the spec itself.
	SourceTypeStatic SourceType = "Static"
	// SourceTypeConfigMap reads a document from a ConfigMap in the same namespace.
	SourceTypeConfigMap SourceType = "ConfigMap"
	// SourceTypeSecret reads a document from a Secret in the same namespace.
	SourceTypeSecret SourceType = "Secret"
)

// SourceSpec is a discriminated union: Type names the provider and exactly one
// matching field must be set.
//
// +kubebuilder:validation:XValidation:rule="has(self.http) == (self.type == 'HTTP')",message="http must be set if and only if type is HTTP"
// +kubebuilder:validation:XValidation:rule="has(self.static) == (self.type == 'Static')",message="static must be set if and only if type is Static"
// +kubebuilder:validation:XValidation:rule="has(self.configMap) == (self.type == 'ConfigMap')",message="configMap must be set if and only if type is ConfigMap"
// +kubebuilder:validation:XValidation:rule="has(self.secret) == (self.type == 'Secret')",message="secret must be set if and only if type is Secret"
type SourceSpec struct {
	// Type names the provider.
	// +kubebuilder:validation:Required
	Type SourceType `json:"type"`

	// HTTP reads devices from a remote HTTP API.
	// +optional
	HTTP *HTTPSource `json:"http,omitempty"`

	// Static lists the devices inline.
	// +optional
	Static *StaticSource `json:"static,omitempty"`

	// ConfigMap reads devices from a ConfigMap in the TargetSource namespace.
	// +optional
	ConfigMap *ObjectSource `json:"configMap,omitempty"`

	// Secret reads devices from a Secret in the TargetSource namespace.
	// +optional
	Secret *ObjectSource `json:"secret,omitempty"`
}

// EndpointSpec is shared by every provider that calls a remote HTTP API.
type EndpointSpec struct {
	// URL of the remote API. Must be http or https.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.startsWith('http://') || self.startsWith('https://')",message="url must start with http:// or https://"
	URL string `json:"url"`

	// Headers added to every request. Authentication headers set by Auth win.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`

	// Auth authenticates the request.
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`

	// TLS applies to https URLs only.
	// +optional
	TLS *ClientTLSSpec `json:"tls,omitempty"`
}

// AuthSpec picks one way of authenticating against a remote HTTP API.
// +kubebuilder:validation:ExactlyOneOf=basic;token;header
type AuthSpec struct {
	// Basic sends HTTP Basic credentials.
	// +optional
	Basic *BasicAuth `json:"basic,omitempty"`

	// Token sends "<scheme> <token>" in the Authorization header.
	// +optional
	Token *TokenAuth `json:"token,omitempty"`

	// Header sends a secret value in an arbitrary header, for APIs that use
	// neither Basic nor a bearer scheme.
	// +optional
	Header *HeaderAuth `json:"header,omitempty"`
}

// BasicAuth reads a username and a password from one Secret.
type BasicAuth struct {
	// SecretRef names a Secret holding "username" and "password" keys. The
	// key names can be overridden with UsernameKey and PasswordKey.
	// +kubebuilder:validation:Required
	SecretRef LocalSecretReference `json:"secretRef"`

	// +kubebuilder:default=username
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`

	// +kubebuilder:default=password
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
}

// TokenAuth reads a token from a Secret key.
type TokenAuth struct {
	// Scheme prefixed to the token in the Authorization header.
	// NetBox uses "Token", most others use "Bearer".
	// +kubebuilder:default=Bearer
	// +kubebuilder:validation:MinLength=1
	// +optional
	Scheme string `json:"scheme,omitempty"`

	// SecretRef names the Secret key holding the token.
	// +kubebuilder:validation:Required
	SecretRef SecretKeyReference `json:"secretRef"`
}

// HeaderAuth sends a secret value in a named header.
type HeaderAuth struct {
	// Name of the header.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// SecretRef names the Secret key holding the header value.
	// +kubebuilder:validation:Required
	SecretRef SecretKeyReference `json:"secretRef"`
}

// ClientTLSSpec configures TLS for an https endpoint.
type ClientTLSSpec struct {
	// InsecureSkipVerify disables server certificate verification.
	// +kubebuilder:default=false
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CABundleRef points at a ConfigMap key holding PEM-encoded CAs.
	// +optional
	CABundleRef *ConfigMapKeyReference `json:"caBundleRef,omitempty"`

	// ClientCertRef points at a kubernetes.io/tls Secret for mutual TLS.
	// +optional
	ClientCertRef *LocalSecretReference `json:"clientCertRef,omitempty"`
}

// LocalSecretReference names a Secret in the TargetSource namespace.
type LocalSecretReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// SecretKeyReference names one key of a Secret in the TargetSource namespace.
type SecretKeyReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// ConfigMapKeyReference names one key of a ConfigMap in the TargetSource namespace.
type ConfigMapKeyReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// HTTPSource reads a document from a remote HTTP API.
type HTTPSource struct {
	EndpointSpec `json:",inline"`

	// Method is the HTTP method.
	// +kubebuilder:default=GET
	// +kubebuilder:validation:Enum=GET;POST
	// +optional
	Method string `json:"method,omitempty"`

	// Body is sent for POST only.
	// +optional
	Body string `json:"body,omitempty"`

	// Mapping says how to read the response. Omit it when the endpoint
	// returns the native list: a JSON or YAML array of objects with name,
	// address, port, profile and labels.
	// +optional
	Mapping *MappingSpec `json:"mapping,omitempty"`

	// Pagination follows a multi-page response.
	// +optional
	Pagination *PaginationSpec `json:"pagination,omitempty"`
}

// PaginationSpec follows server-driven pagination. A Link header with
// rel="next" is always honoured; NextField covers APIs that put the next
// page in the body. A next page must stay on the scheme and host of the
// source URL: every page is sent with the source's credentials, and they are
// never sent to another origin.
type PaginationSpec struct {
	// NextField is a CEL expression over self returning the next page as a
	// full URL, or an opaque token, or null to stop.
	// +optional
	NextField string `json:"nextField,omitempty"`

	// RequestParam is the query parameter used when NextField returns a
	// token rather than a URL.
	// +optional
	RequestParam string `json:"requestParam,omitempty"`

	// MaxPages bounds one run. Reaching it marks the result truncated, so
	// nothing is pruned from an incomplete read, and the run reports it.
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxPages int32 `json:"maxPages,omitempty"`
}

// MappingErrorPolicy decides what happens when an expression fails for one device.
// +kubebuilder:validation:Enum=Skip;Fail
type MappingErrorPolicy string

const (
	// MappingErrorSkip drops the device and counts it in status.invalid.
	MappingErrorSkip MappingErrorPolicy = "Skip"
	// MappingErrorFail fails the whole run.
	MappingErrorFail MappingErrorPolicy = "Fail"
)

// MappingSpec says how to read devices out of an arbitrary JSON or YAML
// document. Every field is a CEL expression. Available variables:
//
//	self  the whole response document
//	item  the current device object
type MappingSpec struct {
	// Items selects the list of device objects. It runs once against self
	// and must return a list. When empty, the document itself must be a list.
	// Example: "self.results"
	// +optional
	Items string `json:"items,omitempty"`

	// Name must return a non-empty string. Defaults to item.name.
	// +optional
	Name string `json:"name,omitempty"`

	// Address must return a non-empty string, an IP or a hostname without a
	// port. Defaults to item.address.
	// +optional
	Address string `json:"address,omitempty"`

	// Port must return an int or a numeric string. Defaults to item.port,
	// then to spec.target.port.
	// +optional
	Port string `json:"port,omitempty"`

	// Profile must return a string. Defaults to item.profile, then to
	// spec.target.profile.
	// +optional
	Profile string `json:"profile,omitempty"`

	// Labels must return a map. Defaults to item.labels.
	// +optional
	Labels string `json:"labels,omitempty"`

	// OnError decides what happens when an expression fails for one device.
	// Skip drops that device and counts it in status.invalid; Fail fails the
	// whole run. Either way the failure is reported, never silent.
	// +kubebuilder:default=Skip
	// +optional
	OnError MappingErrorPolicy `json:"onError,omitempty"`
}

// StaticSource lists devices inline.
type StaticSource struct {
	// Devices is the inventory.
	// +kubebuilder:validation:MinItems=1
	Devices []StaticDevice `json:"devices"`
}

// StaticDevice is one inline device.
type StaticDevice struct {
	// Name of the device as the source knows it.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Address is an IP or hostname without a port.
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// Port overrides spec.target.port for this device.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// Profile overrides spec.target.profile for this device.
	// +optional
	Profile string `json:"profile,omitempty"`

	// Labels are added to the Target.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// ObjectSource reads a document from a ConfigMap or Secret in the
// TargetSource namespace.
type ObjectSource struct {
	// Name of the ConfigMap or Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key to read. When empty, every key is read and the results are
	// concatenated, so an inventory can be split across keys.
	// +optional
	Key string `json:"key,omitempty"`

	// Mapping says how to read the document. Omit it when the document is
	// the native list.
	// +optional
	Mapping *MappingSpec `json:"mapping,omitempty"`
}

// TargetTemplateSpec controls how a discovered device becomes a Target.
type TargetTemplateSpec struct {
	// Port used when the source does not supply one.
	// +kubebuilder:default=57400
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// Profile is the TargetProfile name used when the source does not supply
	// one. It must exist in the same namespace.
	// +optional
	Profile string `json:"profile,omitempty"`

	// Labels added to every Target. Labels from the source win on conflict,
	// except for the reserved operator labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// PruneSpec controls when Targets whose device is gone may be deleted.
type PruneSpec struct {
	// MaxDeleteRatio is the largest share of currently managed Targets that
	// one run may remove, as a percentage. A run that wants to remove more
	// applies its creates and updates, skips all deletions, and reports why.
	// This stops a truncated or misconfigured response from wiping an
	// inventory. Set to 100 to disable the guard, or 0 to hold every deletion.
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	MaxDeleteRatio *int32 `json:"maxDeleteRatio,omitempty"`

	// AllowEmptySource decides whether a source that returns zero devices is
	// a valid answer. When false (the default), an empty result prunes
	// nothing and the run reports Ready=False with reason EmptySource.
	// +kubebuilder:default=false
	// +optional
	AllowEmptySource bool `json:"allowEmptySource,omitempty"`
}

// WebhookSpec enables an HTTP endpoint that requests an immediate run.
// The endpoint carries no target data: a call authenticates, then annotates
// the TargetSource, and the next reconcile does an ordinary full run.
//
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.auth)",message="webhook.auth is required when the webhook is enabled"
type WebhookSpec struct {
	// Enabled turns the endpoint on.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Auth is required when Enabled is true. An unauthenticated endpoint
	// would let anyone who can reach the API port drive fetches against the
	// source at the debounce rate.
	// +optional
	Auth *WebhookAuthSpec `json:"auth,omitempty"`

	// Debounce collapses a burst of calls into a single run. A call within
	// this window of the previous one does not schedule another run.
	// +kubebuilder:default="5s"
	// +optional
	Debounce *metav1.Duration `json:"debounce,omitempty"`
}

// WebhookAuthSpec authenticates webhook callers. When both are set, both
// must pass.
// +kubebuilder:validation:AtLeastOneOf=bearer;signature
type WebhookAuthSpec struct {
	// Bearer compares the Authorization header against a Secret value.
	// +optional
	Bearer *WebhookBearerAuth `json:"bearer,omitempty"`

	// Signature verifies an HMAC over the raw request body.
	// +optional
	Signature *WebhookSignatureAuth `json:"signature,omitempty"`
}

// WebhookBearerAuth compares the Authorization header, minus the "Bearer "
// prefix, against a Secret value in constant time.
type WebhookBearerAuth struct {
	// SecretRef names the Secret key holding the expected token.
	// +kubebuilder:validation:Required
	SecretRef SecretKeyReference `json:"secretRef"`
}

// WebhookSignatureAuth verifies an HMAC over the raw request body.
type WebhookSignatureAuth struct {
	// SecretRef names the Secret key holding the HMAC secret.
	// +kubebuilder:validation:Required
	SecretRef SecretKeyReference `json:"secretRef"`

	// Header carries the hex-encoded signature, optionally prefixed with
	// "<algorithm>=".
	// +kubebuilder:default="X-Hook-Signature"
	// +kubebuilder:validation:MinLength=1
	// +optional
	Header string `json:"header,omitempty"`

	// Algorithm is the HMAC hash.
	// +kubebuilder:default=sha256
	// +kubebuilder:validation:Enum=sha256;sha512
	// +optional
	Algorithm string `json:"algorithm,omitempty"`
}

// TargetSourceStatus is written once per run, at the end.
type TargetSourceStatus struct {
	// ObservedGeneration is the spec generation this status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions: Ready, Reconciling, Stalled, Conflicted.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncTime is when a run last finished, successfully or not.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// LastSuccessfulSyncTime is when a run last finished successfully.
	// +optional
	LastSuccessfulSyncTime *metav1.Time `json:"lastSuccessfulSyncTime,omitempty"`

	// NextSyncTime is when the next run is scheduled.
	// +optional
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`

	// SourceDigest is a stable, order-independent hash of the last
	// successful result. It is informational: it tells a human whether the
	// source moved between two runs. The controller does not branch on it.
	// +optional
	SourceDigest string `json:"sourceDigest,omitempty"`

	// Discovered is how many devices the source returned on the last run.
	// The counters are not omitempty so a zero shows as 0 in kubectl output
	// rather than as an absent field.
	// +optional
	Discovered int32 `json:"discovered"`

	// Managed is how many Targets this TargetSource owns right now.
	// +optional
	Managed int32 `json:"managed"`

	// Invalid counts devices the last run could not turn into a Target.
	// +optional
	Invalid int32 `json:"invalid"`

	// Sanitized counts devices whose labels had to be rewritten to be valid.
	// The originals are kept as annotations on the Target.
	// +optional
	Sanitized int32 `json:"sanitized"`

	// Conflicted counts wanted names owned by something else.
	// +optional
	Conflicted int32 `json:"conflicted"`

	// Pruned counts Targets removed by the last run.
	// +optional
	Pruned int32 `json:"pruned"`

	// FailedDevices samples devices the last run could not turn into a
	// Target, with the reason. Capped at 10 entries.
	// +listType=atomic
	// +optional
	FailedDevices []FailedDevice `json:"failedDevices,omitempty"`

	// ConsecutiveFailures drives the retry backoff.
	// +optional
	ConsecutiveFailures int32 `json:"consecutiveFailures"`

	// LastError is the error from the last failed run, cleared on success.
	// +optional
	LastError string `json:"lastError,omitempty"`
}

// FailedDevice is one device the last run could not turn into a Target.
type FailedDevice struct {
	// Name as the source reported it.
	// +optional
	Name string `json:"name,omitempty"`
	// Reason is a short machine-readable cause.
	Reason string `json:"reason"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.source.type`
// +kubebuilder:printcolumn:name="Targets",type=integer,JSONPath=`.status.managed`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Last Sync",type=date,JSONPath=`.status.lastSuccessfulSyncTime`
// +kubebuilder:printcolumn:name="Suspended",type=boolean,JSONPath=`.spec.suspend`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TargetSource is the Schema for the targetsources API
type TargetSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetSourceSpec   `json:"spec,omitempty"`
	Status TargetSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TargetSourceList contains a list of TargetSource
type TargetSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TargetSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TargetSource{}, &TargetSourceList{})
}
