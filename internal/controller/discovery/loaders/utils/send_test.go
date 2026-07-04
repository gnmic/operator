package utils

import (
	"context"
	"fmt"
	"testing"

	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-openapi/testify/v2/require"
)

// mockChannel returns a buffered channel and a cancelable context
func mockChannel(bufferSize int) (chan []core.DiscoveryMessage, context.Context, context.CancelFunc) {
	ch := make(chan []core.DiscoveryMessage, bufferSize)
	ctx, cancel := context.WithCancel(context.Background())
	return ch, ctx, cancel
}

// mockTargets returns a slice of fake DiscoveredTarget objects
func mockTargets(n int) []core.DiscoveredTarget {
	targets := make([]core.DiscoveredTarget, n)
	for i := range n {
		targets[i] = core.DiscoveredTarget{
			Name: fmt.Sprintf("target-%d", i),
		}
	}
	return targets
}

// mockEvents returns a slice of fake DiscoveryEvent objects
func mockEvents(n int) []core.DiscoveryEvent {
	events := make([]core.DiscoveryEvent, n)
	for i := 0; i < n; i++ {
		events[i] = core.DiscoveryEvent{
			Event: core.EventApply,
			Target: core.DiscoveredTarget{
				Name: fmt.Sprintf("event-target-%d", i),
			},
		}
	}
	return events
}

// drainChannel reads all messages from a channel until it's empty
func drainChannel(ch chan []core.DiscoveryMessage) [][]core.DiscoveryMessage {
	var out [][]core.DiscoveryMessage
	for {
		select {
		case msgs := <-ch:
			out = append(out, msgs)
		default:
			return out
		}
	}
}

func TestSendSnapshot_Basic(t *testing.T) {
	ch, ctx, cancel := mockChannel(5)
	defer cancel()

	targets := mockTargets(3)

	err := SendSnapshot(ctx, ch, targets, "snap-1", 2)
	require.NoError(t, err)

	msgs := drainChannel(ch)
	require.Len(t, msgs, 2) // 2 chunks for 3 targets with chunkSize=2
}

func TestSendEvents_Basic(t *testing.T) {
	ch, ctx, cancel := mockChannel(5)
	defer cancel()

	events := mockEvents(4)

	err := SendEvents(ctx, ch, events, 2)
	require.NoError(t, err)

	msgs := drainChannel(ch)
	require.Len(t, msgs, 2) // 2 chunks for 4 events with chunkSize=2
}
