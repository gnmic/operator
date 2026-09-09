package gnmic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SSE event types returned by the gNMIc pods.
const (
	SSEEventCreate = "create"
	SSEEventUpdate = "update"
	SSEEventDelete = "delete"
)

// SSE store types.
const (
	SSEStoreState  = "state"
	SSEStoreConfig = "config"
)

// sseMaxLineBytes is the longest single line the stream reader accepts.
//
// bufio.Scanner's default is 64 KiB, and an event over that turned into a
// stream error and a reconnect -- with the ApplyCache invalidation and full
// re-apply that a reconnect costs. A target-state event is small, but nothing
// bounds what a pod may put in one, and a line length is no reason to reload
// a collector.
const sseMaxLineBytes = 4 << 20

// SSEEvent represents a parsed SSE event from a gNMIc pod.
type SSEEvent struct {
	// The SSE event type (create, update, delete).
	EventType string
	// The parsed event data.
	Data SSEEventData
}

// SSEEventData represents the JSON payload of an SSE event.
type SSEEventData struct {
	Timestamp time.Time       `json:"timestamp"`
	Store     string          `json:"store"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	EventType string          `json:"event-type"`
	Object    json.RawMessage `json:"object"`
}

// TargetStateObject represents the "object" field of a target state SSE event.
type TargetStateObject struct {
	IntendedState   string            `json:"intended-state"`
	State           string            `json:"state"`
	FailedReason    string            `json:"failed-reason,omitempty"`
	LastUpdated     time.Time         `json:"last-updated"`
	ConnectionState string            `json:"connection-state"`
	Subscriptions   map[string]string `json:"subscriptions"`
}

// StreamTargetState opens an SSE connection to a gNMIc pod and sends parsed
// target state events to the provided channel. It blocks until the context is
// cancelled or the connection is closed. Returns an error on connection failure
// or unexpected stream termination.
func StreamTargetState(ctx context.Context, httpClient *http.Client, podURL string, events chan<- SSEEvent) error {
	return StreamTargetStateWithConnect(ctx, httpClient, podURL, events, nil)
}

// StreamTargetStateWithConnect is StreamTargetState with a hook that runs once
// the pod has accepted the stream (HTTP 200), before any event is delivered. A
// nil onConnect is ignored.
//
// The hook exists because a connection attempt that fails and one that
// succeeds mean different things to the caller: only the latter proves which
// process is on the other end. The operator uses it to notice a collector that
// restarted while no stream was attached to it.
func StreamTargetStateWithConnect(ctx context.Context, httpClient *http.Client, podURL string, events chan<- SSEEvent, onConnect func()) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, podURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE endpoint returned status %d", resp.StatusCode)
	}
	if onConnect != nil {
		onConnect()
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), sseMaxLineBytes)

	// One event is a run of field lines ended by a blank line. Parsing per the
	// spec rather than per line means: the space after the colon is optional,
	// several data: lines are one payload joined by newlines, and event: is
	// taken wherever it appears in the frame, not only ahead of data:.
	var frame sseFrame
	deliver := func() error {
		f := frame
		frame = sseFrame{}
		if !f.hasData {
			return nil
		}
		var data SSEEventData
		if err := json.Unmarshal([]byte(f.data.String()), &data); err != nil {
			return nil // skip malformed events
		}
		// only forward target state events
		if data.Kind != "targets" || data.Store != SSEStoreState {
			return nil
		}
		// A bare send here blocks forever once the buffer is full and the consumer
		// has stopped draining, so the goroutine never returns to scanner.Scan(),
		// never observes the closed body, and leaks along with its connection.
		select {
		case events <- SSEEvent{EventType: f.eventType, Data: data}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := deliver(); err != nil {
				return err
			}
			continue
		}
		// keepalive comment
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			frame.eventType = value
		case "data":
			if frame.hasData {
				frame.data.WriteByte('\n')
			}
			frame.data.WriteString(value)
			frame.hasData = true
		}
		// id, retry and unknown fields are ignored.
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE stream error: %w", err)
	}
	// The server closed the stream without a blank line after the last event.
	// The spec says to drop it; the previous reader delivered on the data line
	// and so would have forwarded it, and losing a final state change over a
	// missing newline is the worse outcome.
	return deliver()
}

// sseFrame accumulates the fields of one event until the blank line ends it.
type sseFrame struct {
	eventType string
	data      strings.Builder
	hasData   bool
}

// ParseTargetStateObject parses the raw JSON object from a target state SSE event.
func ParseTargetStateObject(raw json.RawMessage) (*TargetStateObject, error) {
	var obj TargetStateObject
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("failed to parse target state object: %w", err)
	}
	return &obj, nil
}

// PollTargetEntry represents a single target returned by GET /api/v1/targets.
type PollTargetEntry struct {
	Name  string             `json:"name"`
	State *TargetStateObject `json:"state"`
}

// PollTargetState fetches the full target state snapshot from a gNMIc pod.
func PollTargetState(ctx context.Context, httpClient *http.Client, pollURL string) ([]PollTargetEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create poll request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll endpoint returned status %d", resp.StatusCode)
	}

	var entries []PollTargetEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode poll response: %w", err)
	}

	return entries, nil
}
