package core

import (
	"context"
)

// sendMessages sends discovery messages over a channel in a context-aware manner
func sendMessages(ctx context.Context, out chan<- []DiscoveryMessage, messages []DiscoveryMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- messages:
	}
	return nil
}

// createDiscoverySnapshots takes a list of discovered targets and returns chunked DiscoverySnapshots
func createDiscoverySnapshots(targets []DiscoveredTarget, snapshotID string, chunkSize int) []DiscoverySnapshot {
	if chunkSize <= 0 {
		chunkSize = 1
	}

	var snapshots []DiscoverySnapshot
	totalTargets := len(targets)

	for i := 0; i < totalTargets; i += chunkSize {
		end := i + chunkSize
		if end > totalTargets {
			end = totalTargets
		}

		chunk := targets[i:end]
		snapshots = append(snapshots, DiscoverySnapshot{
			Target:      chunk,
			SnapshotID:  snapshotID,
			IsLastChunk: (end == totalTargets),
		})
	}

	return snapshots
}

// SendSnapshot sends discovered targets as a snapshot over a channel in chunks
func SendSnapshot(ctx context.Context, out chan<- []DiscoveryMessage, targets []DiscoveredTarget, snapshotID string, chunkSize int) error {
	snapshots := createDiscoverySnapshots(targets, snapshotID, chunkSize)

	for _, snapshot := range snapshots {
		// Convert DiscoverySnapshot to DiscoveryMessage interface
		messages := make([]DiscoveryMessage, 1)
		messages[0] = snapshot

		if err := sendMessages(ctx, out, messages); err != nil {
			return err
		}
	}

	return nil
}

// SendEvents sends discovery messages over channel in a context-aware manner
func SendEvents(ctx context.Context, out chan<- []DiscoveryMessage, events []DiscoveryEvent) error {
	// Convert DiscoveryEvent slice to DiscoveryMessage slice
	messages := make([]DiscoveryMessage, len(events))
	for i, msg := range events {
		messages[i] = msg
	}

	return sendMessages(ctx, out, messages)
}
