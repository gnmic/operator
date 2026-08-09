//go:build integration

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// GnmiGen is the device-side ground truth client: what the collectors actually
// did on the wire, observed without trusting the collector's own API.
type GnmiGen struct {
	BaseURL string // http://127.0.0.1:<forwarded>/api/v1
	http    *http.Client
}

func NewGnmiGen(baseURL string) *GnmiGen {
	return &GnmiGen{
		BaseURL: baseURL + "/api/v1",
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// SimTarget is one simulated device.
type SimTarget struct {
	Name             string `json:"name"`
	Listen           string `json:"listen"`
	TLS              bool   `json:"tls"`
	Status           string `json:"status"` // starting | up | rebooting
	PathCount        int    `json:"path_count"`
	UptimeSeconds    int    `json:"uptime_seconds"`
	RebootCount      int    `json:"reboot_count"`
	RebootConfigured bool   `json:"reboot_configured"`
}

// SubEntry is one path entry of a client's SubscribeRequest.
type SubEntry struct {
	Path              string `json:"path"`
	Mode              string `json:"mode"`
	SampleInterval    string `json:"sample_interval"`
	HeartbeatInterval string `json:"heartbeat_interval"`
	SuppressRedundant bool   `json:"suppress_redundant"`
}

// Subscription is one open Subscribe stream on a simulated device.
type Subscription struct {
	ID                      string     `json:"id"`
	EstablishedAt           time.Time  `json:"established_at"`
	Mode                    string     `json:"mode"`
	Prefix                  string     `json:"prefix"`
	Encoding                string     `json:"encoding"`
	AllPaths                bool       `json:"all_paths"`
	Paths                   []string   `json:"paths"`
	Entries                 []SubEntry `json:"subscription_entries"`
	EffectiveSampleInterval string     `json:"effective_sample_interval"`
	NotificationsSent       int        `json:"notifications_sent"`
	SyncMessagesSent        int        `json:"sync_messages_sent"`
	UpdatesOnly             bool       `json:"updates_only"`
}

func (g *GnmiGen) getInto(path string, out any) error {
	resp, err := g.http.Get(g.BaseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, string(body))
	}
	return json.Unmarshal(body, out)
}

// Targets returns every simulated target keyed by name.
func (g *GnmiGen) Targets() (map[string]SimTarget, error) {
	var resp struct {
		Targets []SimTarget `json:"targets"`
	}
	if err := g.getInto("/targets", &resp); err != nil {
		return nil, err
	}
	out := make(map[string]SimTarget, len(resp.Targets))
	for _, t := range resp.Targets {
		out[t.Name] = t
	}
	return out, nil
}

// Target returns one simulated target.
func (g *GnmiGen) Target(name string) (SimTarget, error) {
	var t SimTarget
	err := g.getInto("/targets/"+name, &t)
	return t, err
}

// Subscriptions returns the open Subscribe streams on a target. An unknown or
// idle target yields an empty slice rather than an error, so callers can poll.
func (g *GnmiGen) Subscriptions(target string) ([]Subscription, error) {
	var resp struct {
		Target        string         `json:"target"`
		Subscriptions []Subscription `json:"subscriptions"`
	}
	if err := g.getInto("/subscriptions/"+target, &resp); err != nil {
		return nil, err
	}
	return resp.Subscriptions, nil
}

// StreamCount is the number of open Subscribe streams on a target.
//
// gNMIc opens one stream per Subscription, so a target collected by one pod
// under three subscriptions shows three streams, not one. Duplicate collection
// therefore shows up as a multiple of the expected count rather than as 2.
func (g *GnmiGen) StreamCount(target string) int {
	subs, err := g.Subscriptions(target)
	if err != nil {
		return -1
	}
	return len(subs)
}

// Paths returns the union of resolved leaf paths across a target's streams.
func (g *GnmiGen) Paths(target string) []string {
	subs, err := g.Subscriptions(target)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range subs {
		for _, p := range s.Paths {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Notifications is the total notification count across a target's streams.
func (g *GnmiGen) Notifications(target string) int {
	subs, err := g.Subscriptions(target)
	if err != nil {
		return -1
	}
	total := 0
	for _, s := range subs {
		total += s.NotificationsSent
	}
	return total
}

// EstablishedAt is the earliest established_at among a target's open streams.
// Zero means no stream is open. Used to prove a stream was not torn down and
// re-created across a placement change.
func (g *GnmiGen) EstablishedAt(target string) time.Time {
	subs, err := g.Subscriptions(target)
	if err != nil || len(subs) == 0 {
		return time.Time{}
	}
	earliest := subs[0].EstablishedAt
	for _, s := range subs[1:] {
		if !s.EstablishedAt.IsZero() && (earliest.IsZero() || s.EstablishedAt.Before(earliest)) {
			earliest = s.EstablishedAt
		}
	}
	return earliest
}

// Reboot injects a connection outage. With no targets named, all are rebooted.
func (g *GnmiGen) Reboot(downtime time.Duration, targets ...string) error {
	body, _ := json.Marshal(map[string]any{
		"targets":  targets,
		"downtime": downtime.String(),
	})
	resp, err := g.http.Post(g.BaseURL+"/targets/reboot", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reboot: %s: %s", resp.Status, string(raw))
	}
	var out struct {
		Results []struct {
			Target   string `json:"target"`
			Accepted bool   `json:"accepted"`
			Error    string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, r := range out.Results {
		if !r.Accepted {
			return fmt.Errorf("reboot of %s not accepted: %s", r.Target, r.Error)
		}
	}
	return nil
}

// --- polling assertions --------------------------------------------------

// WaitTargetsUp blocks until every named simulated target reports "up".
func (g *GnmiGen) WaitTargetsUp(t *testing.T, names ...string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("gnmi-gen targets %v to be up", names), func() (bool, string) {
		targets, err := g.Targets()
		if err != nil {
			return false, "API not answering: " + err.Error()
		}
		for _, n := range names {
			tgt, ok := targets[n]
			if !ok {
				return false, fmt.Sprintf("target %s not registered", n)
			}
			if tgt.Status != "up" {
				return false, fmt.Sprintf("target %s is %s", n, tgt.Status)
			}
		}
		return true, ""
	})
}

// WaitStreams blocks until a target has exactly want open Subscribe streams.
func (g *GnmiGen) WaitStreams(t *testing.T, target string, want int) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("%d subscribe stream(s) on %s", want, target), func() (bool, string) {
		got := g.StreamCount(target)
		return got == want, fmt.Sprintf("want %d, got %d", want, got)
	})
}

// WaitPathPresent blocks until a target's streams include path.
func (g *GnmiGen) WaitPathPresent(t *testing.T, target, path string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("path %s on %s", path, target), func() (bool, string) {
		paths := g.Paths(target)
		for _, p := range paths {
			if p == path {
				return true, ""
			}
		}
		return false, fmt.Sprintf("paths are %v", paths)
	})
}

// WaitPathAbsent blocks until a target's streams no longer include path.
// Removal assertions are where the interesting operator bugs live.
func (g *GnmiGen) WaitPathAbsent(t *testing.T, target, path string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("path %s gone from %s", path, target), func() (bool, string) {
		for _, p := range g.Paths(target) {
			if p == path {
				return false, "still present"
			}
		}
		return true, ""
	})
}

// WaitNotificationsAdvance blocks until a target's notification counter grows,
// proving data is actually flowing rather than a stream merely being open.
func (g *GnmiGen) WaitNotificationsAdvance(t *testing.T, target string) {
	t.Helper()
	start := g.Notifications(target)
	Wait(t, Medium, fmt.Sprintf("notifications to advance on %s", target), func() (bool, string) {
		now := g.Notifications(target)
		return now > start, fmt.Sprintf("still at %d (started at %d)", now, start)
	})
}

// AssertCollectedOnce checks the suite's central invariant: every target is
// collected exactly once cluster-wide. Duplicate collection double-counts
// metrics; missing collection loses data silently.
//
// perTarget is the number of Subscriptions bound to each target, which is the
// stream count a single collector produces. Passing the wrong number here turns
// the assertion into a tautology, so tests state it explicitly rather than
// defaulting it.
func (g *GnmiGen) AssertCollectedOnce(t *testing.T, perTarget int, targets ...string) {
	t.Helper()
	for _, target := range targets {
		g.WaitStreams(t, target, perTarget)
	}
}

// ConsistentlyCollectedOnce holds the invariant over a window, since
// distribution bugs often flap rather than settle wrong.
func (g *GnmiGen) ConsistentlyCollectedOnce(t *testing.T, dur time.Duration, perTarget int, targets ...string) {
	t.Helper()
	desc := fmt.Sprintf("exactly %d stream(s) per target", perTarget)
	Consistently(t, dur, time.Second, desc, func() (bool, string) {
		for _, target := range targets {
			if got := g.StreamCount(target); got != perTarget {
				return false, fmt.Sprintf("%s has %d streams, want %d", target, got, perTarget)
			}
		}
		return true, ""
	})
}
