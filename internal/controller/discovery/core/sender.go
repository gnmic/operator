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

// forEachChunk iterates over ranges [start,end) for a total count using the provided chunkSize
func forEachChunk(total, chunkSize int, fn func(start, end int) error) error {
	if chunkSize <= 0 {
		chunkSize = 1
	}

	for i := 0; i < total; i += chunkSize {
		end := i + chunkSize
		if end > total {
			end = total
		}
		if err := fn(i, end); err != nil {
			return err
		}
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

	_ = forEachChunk(totalTargets, chunkSize, func(i, end int) error {
		chunk := targets[i:end]
		snapshots = append(snapshots, DiscoverySnapshot{
			Targets:     chunk,
			SnapshotID:  snapshotID,
			IsLastChunk: (end == totalTargets),
		})
		return nil
	})

	return snapshots
}

// SendSnapshot sends discovered targets as a snapshot over a channel in chunks
func SendSnapshot(ctx context.Context, out chan<- []DiscoveryMessage, targets []DiscoveredTarget, snapshotID string, chunkSize int) error {
	snapshots := createDiscoverySnapshots(targets, snapshotID, chunkSize)

	for _, snapshot := range snapshots {
		// Convert DiscoverySnapshot to DiscoveryMessage
		messages := make([]DiscoveryMessage, 1)
		messages[0] = snapshot

		if err := sendMessages(ctx, out, messages); err != nil {
			return err
		}
	}

	return nil
}

func eventsToMessages(events []DiscoveryEvent) []DiscoveryMessage {
	message := make([]DiscoveryMessage, len(events))
	for i, event := range events {
		message[i] = event
	}
	return message
}

// SendEvents sends discovery messages over channel in a context-aware manner
func SendEvents(ctx context.Context, out chan<- []DiscoveryMessage, events []DiscoveryEvent, chunkSize int) error {
	if chunkSize <= 0 {
		chunkSize = 1
	}
	messages := eventsToMessages(events)
	total := len(messages)

	return forEachChunk(total, chunkSize, func(i, end int) error {
		return sendMessages(ctx, out, messages[i:end])
	})
}
