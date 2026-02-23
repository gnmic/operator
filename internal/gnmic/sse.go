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

	scanner := bufio.NewScanner(resp.Body)
	var currentEventType string

	for scanner.Scan() {
		line := scanner.Text()

		// keepalive comment
		if strings.HasPrefix(line, ":") {
			continue
		}

		// empty line = end of event (but we process on "data:" line)
		if line == "" {
			currentEventType = ""
			continue
		}
		if after, found := strings.CutPrefix(line, "event: "); found {
			currentEventType = after
			continue
		}
		if dataStr, found := strings.CutPrefix(line, "data: "); found {

			var data SSEEventData
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				continue // skip malformed events
			}

			// only forward target state events
			if data.Kind != "targets" || data.Store != SSEStoreState {
				continue
			}

			events <- SSEEvent{
				EventType: currentEventType,
				Data:      data,
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE stream error: %w", err)
	}

	return nil
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
