package utils

import (
	"context"
	"fmt"

	"github.com/gnmic/operator/internal/controller/discovery/core"
)

// sendMessages sends discovery messages over a channel in a context-aware manner
func sendMessages(ctx context.Context, out chan<- []core.DiscoveryMessage, messages []core.DiscoveryMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- messages:
	}
	return nil
}

// forEachChunk iterates over ranges [start,end) for a total count using the provided chunkSize
func forEachChunk(total, chunkSize int, fn func(start, end int) error) error {
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
func createDiscoverySnapshots(targets []core.DiscoveredTarget, snapshotID string, chunkSize int) []core.DiscoverySnapshot {
	var snapshots []core.DiscoverySnapshot
	totalTargets := len(targets)
	totalChunks := (totalTargets + chunkSize - 1) / chunkSize

	_ = forEachChunk(totalTargets, chunkSize, func(i, end int) error {
		chunk := targets[i:end]
		snapshots = append(snapshots, core.DiscoverySnapshot{
			Targets:     chunk,
			SnapshotID:  snapshotID,
			ChunkIndex:  i / chunkSize,
			TotalChunks: totalChunks,
		})
		return nil
	})

	return snapshots
}

// SendSnapshot sends discovered targets as a snapshot over a channel in chunks
func SendSnapshot(ctx context.Context, out chan<- []core.DiscoveryMessage, targets []core.DiscoveredTarget, snapshotID string, chunkSize int) error {
	if len(targets) == 0 {
		return fmt.Errorf("no targets in Snapshot")
	}

	snapshots := createDiscoverySnapshots(targets, snapshotID, chunkSize)
	for _, snapshot := range snapshots {
		// Convert DiscoverySnapshot to DiscoveryMessage
		messages := make([]core.DiscoveryMessage, 1)
		messages[0] = snapshot

		if err := sendMessages(ctx, out, messages); err != nil {
			return err
		}
	}

	return nil
}

func eventsToMessages(events []core.DiscoveryEvent) []core.DiscoveryMessage {
	message := make([]core.DiscoveryMessage, len(events))
	for i, event := range events {
		message[i] = event
	}
	return message
}

// SendEvents sends discovery messages over channel in a context-aware manner
func SendEvents(ctx context.Context, out chan<- []core.DiscoveryMessage, events []core.DiscoveryEvent, chunkSize int) error {
	if len(events) == 0 {
		return fmt.Errorf("no events to process")
	}

	messages := eventsToMessages(events)
	total := len(messages)

	return forEachChunk(total, chunkSize, func(i, end int) error {
		return sendMessages(ctx, out, messages[i:end])
	})
}
