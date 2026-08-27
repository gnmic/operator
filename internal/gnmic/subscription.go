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
		Name:              subNN,
		Prefix:            subscription.Prefix,
		Paths:             subscription.Paths,
		Mode:              mode,
		StreamMode:        streamMode,
		UpdatesOnly:       subscription.UpdatesOnly,
		SuppressRedundant: subscription.SuppressRedundant,
		Depth:             subscription.Depth,
		Target:            subscription.Target,
		Outputs:           outputs,
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
	//
	// spec.streamSubscriptions holds bare Subscription names, while allSubs is keyed
	// "<namespace>/<pipeline>/<name>" so two pipelines sharing one Subscription CR
	// keep separate output bindings. Looking up the bare name therefore never matched,
	// and because the slice was pre-sized to the number of names, every entry stayed
	// nil and marshalled as a literal null in the payload sent to the collectors.
	for _, name := range subscription.StreamSubscriptions {
		key := siblingSubscriptionKey(subNN, name)
		streamSubSpec, ok := allSubs[key]
		if !ok {
			// A referenced stream subscription has to be selected into the same
			// pipeline, or there is nothing to point at.
			logger.Warn("stream subscription not found in the pipeline, skipping",
				"subscription", subNN, "streamSubscription", name, "lookup", key)
			continue
		}
		config.StreamSubscriptions = append(config.StreamSubscriptions,
			buildSubscriptionConfig(key, &streamSubSpec, nil, nil))
	}
	return config
}

// siblingSubscriptionKey resolves a bare Subscription name against the
// pipeline-scoped key of the Subscription that referenced it.
func siblingSubscriptionKey(subNN, name string) string {
	if i := strings.LastIndex(subNN, Delimiter); i >= 0 {
		return subNN[:i+1] + name
	}
	return name
}

// specModeToConfig splits a mode string like "STREAM/SAMPLE" into mode and stream mode
func specModeToConfig(mode string) (string, string) {
	parts := strings.SplitN(mode, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
