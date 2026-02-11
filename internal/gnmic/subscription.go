package gnmic

import (
	"strings"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

// buildSubscriptionConfig creates a gNMIc SubscriptionConfig from a Subscription
// TODO: complete the mapping from spec to config
func buildSubscriptionConfig(subNN string, subscription *gnmicv1alpha1.SubscriptionSpec, outputs []string, allSubs map[string]gnmicv1alpha1.SubscriptionSpec) *gapi.SubscriptionConfig {
	mode, streamMode := specModeToConfig(subscription.Mode)

	config := &gapi.SubscriptionConfig{
		Name:        subNN,
		Prefix:      subscription.Prefix,
		Paths:       subscription.Paths,
		Mode:        mode,
		StreamMode:  streamMode,
		UpdatesOnly: subscription.UpdatesOnly,
		Depth:       subscription.Depth,
		Target:      subscription.Target,
		Outputs:     outputs,
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
	if subscription.Qos != nil {
		config.Qos = subscription.Qos
	}
	if subscription.History != nil {
		config.History = &gapi.HistoryConfig{
			Snapshot: subscription.History.Snapshot.Time,
			Start:    subscription.History.Start.Time,
			End:      subscription.History.End.Time,
		}
	}
	// handle streamSubscriptions
	if len(subscription.StreamSubscriptions) > 0 {
		config.StreamSubscriptions = make([]*gapi.SubscriptionConfig, len(subscription.StreamSubscriptions))
		for i, streamSubscription := range subscription.StreamSubscriptions {
			streamSubSpec, ok := allSubs[streamSubscription]
			if !ok {
				continue
			}
			config.StreamSubscriptions[i] = buildSubscriptionConfig(streamSubscription, &streamSubSpec, nil, nil)
		}
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
