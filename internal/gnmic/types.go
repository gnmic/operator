package gnmic

import (
	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

// Delimiter used for namespaced names (namespace/name)
const Delimiter = "/"

// ApplyPlan represents the configuration to be applied to gNMIc
type ApplyPlan struct {
	Targets map[string]*gapi.TargetConfig `json:"targets,omitempty"`
	// pod index -> target names
	CurrentTargetAssignment map[int]map[string]struct{}         `json:"current-target-assignment,omitempty"`
	Subscriptions           map[string]*gapi.SubscriptionConfig `json:"subscriptions,omitempty"`
	Outputs                 map[string]map[string]any           `json:"outputs,omitempty"`
	Inputs                  map[string]map[string]any           `json:"inputs,omitempty"`
	Processors              map[string]map[string]any           `json:"processors,omitempty"`
	TunnelTargetMatches     map[string]*TunnelTargetMatch       `json:"tunnel-target-matches,omitempty"`
	PrometheusPorts         map[string]int32                    `json:"prometheus-output-ports,omitempty"` // For status reporting
}

// TunnelTargetMatch defines a policy for matching tunnel targets
//
// yaml tags are required in addition to json: the final config.yaml is
// rendered via gopkg.in/yaml.v2 (see buildConfigContent in
// internal/controller/cluster_controller.go), which does not read json
// struct tags at all. Without an explicit yaml:",omitempty" tag, yaml.v2
// serializes every field regardless of value -- an omitted
// TunnelTargetPolicy.spec.match (meant to match all targets, per
// docs/content/docs/user-guide/tunneltargetpolicy.md#match-all-targets)
// was rendering as literal `type: ""` / `id: ""` instead of omitting the
// keys, which gnmic did not treat as a wildcard.
type TunnelTargetMatch struct {
	// A regex pattern to check the target type as reported by
	// the tunnel.Target to the Tunnel Server.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// A Regex pattern to check the target ID as reported by
	// the tunnel.Target to the Tunnel Server
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Matching target desired configuration.
	// This is build from the target profile and the credentials.
	Config *gapi.TargetConfig `json:"config,omitempty" yaml:"config,omitempty"`
}

// PipelineData holds the resolved resources for a single pipeline
type PipelineData struct {
	Targets              map[string]gnmicv1alpha1.Target
	TargetProfiles       map[string]gnmicv1alpha1.TargetProfileSpec
	Subscriptions        map[string]gnmicv1alpha1.SubscriptionSpec
	Outputs              map[string]gnmicv1alpha1.OutputSpec
	Inputs               map[string]gnmicv1alpha1.InputSpec
	OutputProcessors     map[string]gnmicv1alpha1.ProcessorSpec
	InputProcessors      map[string]gnmicv1alpha1.ProcessorSpec
	TunnelTargetPolicies map[string]gnmicv1alpha1.TunnelTargetPolicySpec
	// Ordered list of output processor names (refs first, then sorted selectors)
	OutputProcessorOrder []string
	// Ordered list of input processor names (refs first, then sorted selectors)
	InputProcessorOrder []string
	// ResolvedOutputAddresses holds resolved service addresses for outputs (outputNN -> addresses)
	ResolvedOutputAddresses map[string][]string
}

// NewPipelineData creates a new PipelineData with initialized maps
func NewPipelineData() *PipelineData {
	return &PipelineData{
		Targets:                 make(map[string]gnmicv1alpha1.Target),
		TargetProfiles:          make(map[string]gnmicv1alpha1.TargetProfileSpec),
		Subscriptions:           make(map[string]gnmicv1alpha1.SubscriptionSpec),
		Outputs:                 make(map[string]gnmicv1alpha1.OutputSpec),
		Inputs:                  make(map[string]gnmicv1alpha1.InputSpec),
		OutputProcessors:        make(map[string]gnmicv1alpha1.ProcessorSpec),
		InputProcessors:         make(map[string]gnmicv1alpha1.ProcessorSpec),
		TunnelTargetPolicies:    make(map[string]gnmicv1alpha1.TunnelTargetPolicySpec),
		ResolvedOutputAddresses: make(map[string][]string),
	}
}

// Credentials holds authentication credentials for a target
type Credentials struct {
	Username string
	Password string
	Token    string
}

// CredentialsFetcher is an interface for fetching credentials from a secret reference
type CredentialsFetcher interface {
	// FetchCredentials retrieves credentials from a secret reference in the given namespace
	FetchCredentials(namespace, secretRef string) (*Credentials, error)
}
