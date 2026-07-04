package gnmic

import "testing"

func TestNewPipelineData(t *testing.T) {
	pd := NewPipelineData()
	if pd.Targets == nil || pd.TargetProfiles == nil || pd.Subscriptions == nil ||
		pd.Outputs == nil || pd.Inputs == nil || pd.OutputProcessors == nil ||
		pd.InputProcessors == nil || pd.TunnelTargetPolicies == nil ||
		pd.ResolvedOutputAddresses == nil {
		t.Fatal("NewPipelineData should initialize all maps")
	}
}
