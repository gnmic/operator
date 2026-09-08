package gnmic

import (
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	"k8s.io/utils/ptr"
)

// RedactedSecret is what a masked credential reads as. Same marker
// gapi.TargetConfig.String() uses, so a plan looks the same whether it was
// logged or fetched.
const RedactedSecret = "****"

// Redacted returns a copy of the plan with every secret masked, for anything that
// leaves the process -- today the operator API's plan endpoint.
//
// The plan is what the collectors are sent, so its TargetConfigs carry the
// device password and token straight from the credentials Secret. Every other
// field is the operator's own derivation from CRDs the caller could read
// anyway; only what came out of a Secret is masked. The username stays: it is
// the half of the credential that identifies, not authenticates, and it is what
// someone reading the plan needs to tell two profiles apart.
//
// Every map is rebuilt, and every TargetConfig is copied before being touched,
// so the cached plan is never written to and the caller cannot reach it through
// the result. Values the plan shares by pointer but never mutates after Build()
// -- subscriptions, output/input/processor maps -- are carried over as is.
func (p *ApplyPlan) Redacted() *ApplyPlan {
	if p == nil {
		return nil
	}
	out := &ApplyPlan{
		Targets:             make(map[string]*gapi.TargetConfig, len(p.Targets)),
		Subscriptions:       make(map[string]*gapi.SubscriptionConfig, len(p.Subscriptions)),
		Outputs:             make(map[string]map[string]any, len(p.Outputs)),
		Inputs:              make(map[string]map[string]any, len(p.Inputs)),
		Processors:          make(map[string]map[string]any, len(p.Processors)),
		TunnelTargetMatches: make(map[string]*TunnelTargetMatch, len(p.TunnelTargetMatches)),
		PrometheusPorts:     make(map[string]int32, len(p.PrometheusPorts)),
	}
	for k, v := range p.Targets {
		out.Targets[k] = redactTargetConfig(v)
	}
	for k, v := range p.Subscriptions {
		out.Subscriptions[k] = v
	}
	for k, v := range p.Outputs {
		out.Outputs[k] = v
	}
	for k, v := range p.Inputs {
		out.Inputs[k] = v
	}
	for k, v := range p.Processors {
		out.Processors[k] = v
	}
	for k, v := range p.TunnelTargetMatches {
		if v == nil {
			out.TunnelTargetMatches[k] = nil
			continue
		}
		out.TunnelTargetMatches[k] = &TunnelTargetMatch{
			Type:   v.Type,
			ID:     v.ID,
			Config: redactTargetConfig(v.Config),
		}
	}
	for k, v := range p.PrometheusPorts {
		out.PrometheusPorts[k] = v
	}
	if p.CurrentTargetAssignment != nil {
		out.CurrentTargetAssignment = make(map[int]map[string]struct{}, len(p.CurrentTargetAssignment))
		for pod, names := range p.CurrentTargetAssignment {
			copied := make(map[string]struct{}, len(names))
			for n := range names {
				copied[n] = struct{}{}
			}
			out.CurrentTargetAssignment[pod] = copied
		}
	}
	return out
}

// redactTargetConfig masks the fields applyCredentials fills from a Secret. A
// shallow copy is enough: the other pointer fields are never written after the
// plan is built, and the masked ones are replaced rather than modified in place.
//
// Not gapi's DeepCopy, which copies EventTags back onto the source.
func redactTargetConfig(tc *gapi.TargetConfig) *gapi.TargetConfig {
	if tc == nil {
		return nil
	}
	c := *tc
	if c.Password != nil {
		c.Password = ptr.To(RedactedSecret)
	}
	if c.Token != nil {
		c.Token = ptr.To(RedactedSecret)
	}
	return &c
}
