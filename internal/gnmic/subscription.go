package gnmic

import (
	"strings"

	gnmicv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

// buildSubscriptionConfig creates a gNMIc SubscriptionConfig from a Subscription
// TODO: complete the mapping from spec to config
func buildSubscriptionConfig(subNN string, subscription *gnmicv1alpha1.SubscriptionSpec, outputs []string) *gapi.SubscriptionConfig {
	mode, streamMode := specModeToConfig(subscription.Mode)

	config := &gapi.SubscriptionConfig{
		Name:        subNN,
		Prefix:      subscription.Prefix,
		Paths:       subscription.Paths,
		Mode:        mode,
		StreamMode:  streamMode,
		UpdatesOnly: subscription.UpdatesOnly,
		Depth:       subscription.Depth,
	}

	if len(outputs) > 0 {
		config.Outputs = outputs
	}

	if subscription.Encoding != "" {
		config.Encoding = &subscription.Encoding
	}
	if subscription.SampleInterval.Duration > 0 {
		config.SampleInterval = &subscription.SampleInterval.Duration
	}
	if subscription.HeartbeatInterval.Duration > 0 {
		config.HeartbeatInterval = &subscription.HeartbeatInterval.Duration
	}

	return config
}

// specModeToConfig splits a mode string like "STREAM/SAMPLE" into mode and stream mode
func specModeToConfig(mode string) (string, string) {
	parts := strings.SplitN(mode, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
