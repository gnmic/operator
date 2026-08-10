//go:build integration

package harness

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// CollectorContainer is the container name in collector pods.
const CollectorContainer = "gnmic"

// FirstCollectorPod returns the name of the first ready collector pod for cluster.
func (k *K8s) FirstCollectorPod(t *testing.T, cluster string) string {
	t.Helper()
	pods := k.ClusterPods(t, cluster)
	for i := range pods {
		if podReady(&pods[i]) {
			return pods[i].Name
		}
	}
	t.Fatalf("no ready collector pod for cluster %s", cluster)
	return ""
}

// ReadCollectorFile returns the contents of path inside the first ready
// collector pod. Missing files yield an empty string rather than failing, so
// callers can poll.
func (k *K8s) ReadCollectorFile(t *testing.T, cluster, path string) string {
	t.Helper()
	pod := k.FirstCollectorPod(t, cluster)
	out := k.Exec(t, pod, CollectorContainer, "sh", "-c", fmt.Sprintf("cat %q 2>/dev/null || true", path))
	return out
}

// WaitCollectorFileNonEmpty polls until the file inside the collector is non-empty.
func (k *K8s) WaitCollectorFileNonEmpty(t *testing.T, cluster, path string) string {
	t.Helper()
	var body string
	Wait(t, Medium, fmt.Sprintf("collector file %s non-empty", path), func() (bool, string) {
		body = k.ReadCollectorFile(t, cluster, path)
		if strings.TrimSpace(body) == "" {
			return false, "empty"
		}
		return true, ""
	})
	return body
}

// WaitCollectorFileGrows waits until the file's byte length increases past floor.
func (k *K8s) WaitCollectorFileGrows(t *testing.T, cluster, path string, floor int) string {
	t.Helper()
	var body string
	Wait(t, Medium, fmt.Sprintf("collector file %s grows past %d", path, floor), func() (bool, string) {
		body = k.ReadCollectorFile(t, cluster, path)
		if len(body) > floor {
			return true, ""
		}
		return false, fmt.Sprintf("len=%d", len(body))
	})
	return body
}

// EventMsg is one gnmic event-format record as written by a file output.
type EventMsg struct {
	Name      string            `json:"name"`
	Timestamp int64             `json:"timestamp"`
	Tags      map[string]string `json:"tags"`
	Values    map[string]any    `json:"values"`
}

// ParseEvents extracts EventMsg values from a file-output body. Lines may be
// single objects or JSON arrays of objects.
func ParseEvents(body string) []EventMsg {
	var out []EventMsg
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var one EventMsg
		if err := json.Unmarshal([]byte(line), &one); err == nil && (one.Tags != nil || one.Values != nil || one.Name != "") {
			out = append(out, one)
			continue
		}
		var many []EventMsg
		if err := json.Unmarshal([]byte(line), &many); err == nil {
			out = append(out, many...)
		}
	}
	return out
}

// EventsHaveTag reports whether any event carries tags[key]=value.
func EventsHaveTag(events []EventMsg, key, value string) bool {
	for _, e := range events {
		if e.Tags != nil && e.Tags[key] == value {
			return true
		}
	}
	return false
}

// EventsHaveSource reports whether any event's tags identify source (substring match).
func EventsHaveSource(events []EventMsg, source string) bool {
	for _, e := range events {
		if e.Tags == nil {
			continue
		}
		for _, v := range e.Tags {
			if strings.Contains(v, source) {
				return true
			}
		}
		if src, ok := e.Tags["source"]; ok && strings.Contains(src, source) {
			return true
		}
	}
	return false
}

// WaitEventsHaveTag polls the collector file until an event carries the tag.
func (k *K8s) WaitEventsHaveTag(t *testing.T, cluster, path, key, value string) []EventMsg {
	t.Helper()
	var events []EventMsg
	Wait(t, Medium, fmt.Sprintf("events with %s=%s in %s", key, value, path), func() (bool, string) {
		events = ParseEvents(k.ReadCollectorFile(t, cluster, path))
		if EventsHaveTag(events, key, value) {
			return true, ""
		}
		return false, fmt.Sprintf("%d events", len(events))
	})
	return events
}

// WaitEventsHaveSources polls until every named source appears in the file.
func (k *K8s) WaitEventsHaveSources(t *testing.T, cluster, path string, sources ...string) []EventMsg {
	t.Helper()
	var events []EventMsg
	Wait(t, Medium, fmt.Sprintf("events from %v in %s", sources, path), func() (bool, string) {
		events = ParseEvents(k.ReadCollectorFile(t, cluster, path))
		missing := make([]string, 0)
		for _, src := range sources {
			if !EventsHaveSource(events, src) {
				missing = append(missing, src)
			}
		}
		if len(missing) == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("missing=%v events=%d", missing, len(events))
	})
	return events
}

// ConsistentlyEventsLackTag fails if any sample in the window carries the tag.
func (k *K8s) ConsistentlyEventsLackTag(t *testing.T, cluster, path, key, value string, dur time.Duration) {
	t.Helper()
	Consistently(t, dur, time.Second, fmt.Sprintf("no events with %s=%s", key, value), func() (bool, string) {
		events := ParseEvents(k.ReadCollectorFile(t, cluster, path))
		if EventsHaveTag(events, key, value) {
			return false, "tag present"
		}
		return true, ""
	})
}
