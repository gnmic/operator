//go:build integration

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strings"
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
	var rejected []string
	for _, r := range out.Results {
		if !r.Accepted {
			rejected = append(rejected, fmt.Sprintf("%s (%s)", r.Target, r.Error))
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	if len(rejected) == len(out.Results) {
		return fmt.Errorf("reboot: none accepted: %s", strings.Join(rejected, "; "))
	}
	// Partial accept is normal during overlapping chaos waves.
	return fmt.Errorf("reboot: %d/%d not accepted: %s", len(rejected), len(out.Results), strings.Join(rejected, "; "))
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

// FleetStreamStats is one sample of stream counts across a set of targets.
type FleetStreamStats struct {
	Exact   int // targets with exactly want streams
	Over    int // targets with more than want
	Under   int // targets with fewer than want (including errors)
	Samples map[string]int
}

// SampleFleetStreams counts open streams for every named target in one pass.
func (g *GnmiGen) SampleFleetStreams(perTarget int, targets []string) FleetStreamStats {
	st := FleetStreamStats{Samples: make(map[string]int, len(targets))}
	for _, name := range targets {
		n := g.StreamCount(name)
		st.Samples[name] = n
		switch {
		case n == perTarget:
			st.Exact++
		case n > perTarget:
			st.Over++
		default:
			st.Under++
		}
	}
	return st
}

// WaitFleetStreams waits until every target has exactly perTarget streams.
// One poll loop covers the whole fleet; serial WaitStreams would take too long
// at hundreds of targets.
func (g *GnmiGen) WaitFleetStreams(t *testing.T, timeout time.Duration, perTarget int, targets []string) {
	t.Helper()
	WaitFor(t, timeout, 2*time.Second, fmt.Sprintf("fleet exactly %d stream(s) each", perTarget), func() (bool, string) {
		st := g.SampleFleetStreams(perTarget, targets)
		if st.Exact == len(targets) {
			return true, ""
		}
		return false, formatFleetStreamMismatch(st, perTarget, 8)
	})
}

func formatFleetStreamMismatch(st FleetStreamStats, perTarget, limit int) string {
	var under, over []string
	for name, n := range st.Samples {
		switch {
		case n > perTarget:
			over = append(over, fmt.Sprintf("%s=%d", name, n))
		case n < perTarget:
			under = append(under, fmt.Sprintf("%s=%d", name, n))
		}
	}
	sort.Strings(under)
	sort.Strings(over)
	msg := fmt.Sprintf("exact=%d over=%d under=%d (of %d)", st.Exact, st.Over, st.Under, len(st.Samples))
	if len(under) > 0 {
		msg += "; under=[" + joinCapped(under, limit) + "]"
	}
	if len(over) > 0 {
		msg += "; over=[" + joinCapped(over, limit) + "]"
	}
	return msg
}

func joinCapped(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, " ")
	}
	return strings.Join(items[:limit], " ") + fmt.Sprintf(" …+%d", len(items)-limit)
}

// ConsistentlyFleetCollectedOnce holds the fleet invariant over a window.
func (g *GnmiGen) ConsistentlyFleetCollectedOnce(t *testing.T, dur time.Duration, perTarget int, targets []string) {
	t.Helper()
	Consistently(t, dur, 2*time.Second, fmt.Sprintf("fleet exactly %d stream(s) each", perTarget), func() (bool, string) {
		st := g.SampleFleetStreams(perTarget, targets)
		if st.Over > 0 {
			return false, fmt.Sprintf("%d target(s) over-collected", st.Over)
		}
		if st.Exact != len(targets) {
			return false, fmt.Sprintf("exact=%d under=%d (of %d)", st.Exact, st.Under, len(targets))
		}
		return true, ""
	})
}

// RebootRandom reboots a random non-empty subset of currently-up targets with
// a downtime uniformly chosen in [min, max]. maxSubset caps wave size so
// overlapping chaos does not reboot the entire fleet every tick (which mostly
// yields "busy or rebooting" and a reconnect storm). maxSubset <= 0 means
// min(32, len(targets)/4) with a floor of 1.
func (g *GnmiGen) RebootRandom(targets []string, min, max time.Duration, maxSubset int) error {
	if len(targets) == 0 {
		return nil
	}
	if max < min {
		min, max = max, min
	}
	pool := g.upTargets(targets)
	if len(pool) == 0 {
		return fmt.Errorf("reboot: no targets currently up")
	}
	capN := maxSubset
	if capN <= 0 {
		capN = len(pool) / 4
		if capN < 1 {
			capN = 1
		}
		if capN > 32 {
			capN = 32
		}
	}
	if capN > len(pool) {
		capN = len(pool)
	}
	n := 1 + rand.Intn(capN)
	perm := rand.Perm(len(pool))
	subset := make([]string, 0, n)
	for i := 0; i < n; i++ {
		subset = append(subset, pool[perm[i]])
	}
	span := max - min
	downtime := min
	if span > 0 {
		downtime = min + time.Duration(rand.Int63n(int64(span)+1))
	}
	return g.Reboot(downtime, subset...)
}

func (g *GnmiGen) upTargets(names []string) []string {
	all, err := g.Targets()
	if err != nil {
		return append([]string(nil), names...)
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if t, ok := all[n]; ok && t.Status == "up" {
			out = append(out, n)
		}
	}
	return out
}
