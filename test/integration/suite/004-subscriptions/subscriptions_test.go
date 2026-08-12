//go:build integration

// Package subscriptions covers Subscription CR edits reaching the wire:
// paths, sample interval, stream mode, updatesOnly, suppressRedundant,
// prefix, deletion, and rapid successive patches — without collector restarts.
package subscriptions

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster  = "c1"
	pipeline = "collect"
	output   = "prom"

	leaf1  = "leaf1"
	leaf2  = "leaf2"
	spine1 = "spine1"

	pathIF  = "/interface/statistics"
	pathCPU = "/platform/control/process"
	pathSys = "/system/name"
)

var allTargets = []string{leaf1, leaf2, spine1}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "004-subscriptions",
		RequireTargets: allTargets,
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

func waitClusterReady(t *testing.T) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, cluster)
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

func setPipelineEnabled(t *testing.T, enabled bool) {
	t.Helper()
	s.K8s.Patch(t, s.K8s.Pipeline(t, pipeline), fmt.Sprintf(`{"spec":{"enabled":%t}}`, enabled))
}

// waitIdle deletes suite Subscriptions left by a prior test, then waits until
// every simulated target reports zero streams.
//
// gNMIc rejects an apply that still has targets but no subscriptions
// ("if targets are provided, at least one subscription is required"), so
// deleting the last Subscription alone cannot drain the collector — the
// previous config sticks and streams stay open. Disable the pipeline so the
// operator can apply an empty plan and tear the streams down.
func waitIdle(t *testing.T) {
	t.Helper()
	var list gnmicv1alpha1.SubscriptionList
	if err := s.K8s.Client.List(s.Ctx, &list, client.InNamespace(s.Namespace)); err == nil {
		for i := range list.Items {
			sub := &list.Items[i]
			if sub.Labels["suite"] != "004" {
				continue
			}
			_ = s.K8s.Client.Delete(s.Ctx, sub)
		}
	}
	harness.Wait(t, harness.Medium, "suite-004 Subscriptions gone", func() (bool, string) {
		var left gnmicv1alpha1.SubscriptionList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		n := 0
		for _, sub := range left.Items {
			if sub.Labels["suite"] == "004" {
				n++
			}
		}
		return n == 0, fmt.Sprintf("%d remain", n)
	})
	setPipelineEnabled(t, false)
	harness.Wait(t, harness.Medium, "all simulated targets idle", func() (bool, string) {
		for _, name := range allTargets {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

func applySubscription(t *testing.T, name string, vars map[string]any) {
	t.Helper()
	base := map[string]any{
		"Name":              name,
		"Mode":              "STREAM/SAMPLE",
		"SampleInterval":    "1s",
		"HeartbeatInterval": "",
		"UpdatesOnly":       false,
		"SuppressRedundant": false,
		"Prefix":            "",
		"Target":            "",
		"Paths":             []string{pathIF},
	}
	for k, v := range vars {
		base[k] = v
	}
	b, err := os.ReadFile("fixtures/subscription.yaml")
	if err != nil {
		t.Fatalf("reading subscription fixture: %v", err)
	}
	// Create the Subscription before re-enabling the pipeline. Enabling first
	// can race an apply with targets but no subscriptions, which gNMIc rejects
	// and can leave the collector mid-reconnect with paths=[].
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), base); err != nil {
		t.Fatalf("applying subscription %s: %v", name, err)
	}
	setPipelineEnabled(t, true)
	waitClusterReady(t)
}

func subscription(t *testing.T, name string) *gnmicv1alpha1.Subscription {
	t.Helper()
	o := &gnmicv1alpha1.Subscription{}
	s.K8s.WaitExists(t, name, o)
	return o
}

func patchSubscription(t *testing.T, name string, patch map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"spec": patch})
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	s.K8s.Patch(t, subscription(t, name), string(body))
}

func deleteSubscription(t *testing.T, name string) {
	t.Helper()
	sub := subscription(t, name)
	if err := s.K8s.Client.Delete(s.Ctx, sub); err != nil {
		t.Fatalf("deleting subscription %s: %v", name, err)
	}
	s.K8s.WaitAbsent(t, name, &gnmicv1alpha1.Subscription{})
}

func waitStreams(t *testing.T, want int) {
	t.Helper()
	// One shared Medium budget across the fleet. Sequential per-target waits
	// burn the full timeout on the first lagging target and hide siblings.
	harness.Wait(t, harness.Medium, fmt.Sprintf("%d stream(s) on all targets", want), func() (bool, string) {
		for _, name := range allTargets {
			if n := s.GnmiGen.StreamCount(name); n != want {
				return false, fmt.Sprintf("%s streams=%d", name, n)
			}
		}
		return true, ""
	})
}

func waitPathOnAll(t *testing.T, path string) {
	t.Helper()
	for _, name := range allTargets {
		s.GnmiGen.WaitPathPresent(t, name, path)
	}
}

// waitPathsOnAll waits until every target carries all want paths on the same
// stream snapshot. Split waitPathOnAll calls race stream reconnects: the first
// path can still be visible on the old stream, then paths go empty before the
// next assertion.
func waitPathsOnAll(t *testing.T, want ...string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("paths %v on all targets", want), func() (bool, string) {
		for _, name := range allTargets {
			paths := s.GnmiGen.Paths(name)
			for _, w := range want {
				found := false
				for _, p := range paths {
					if p == w {
						found = true
						break
					}
				}
				if !found {
					return false, fmt.Sprintf("%s paths=%v", name, paths)
				}
			}
		}
		return true, ""
	})
}

// waitPathReplacedOnAll waits until every target shows have and not gone on the
// same snapshot. Sequential present-then-absent waits can pass the first check
// on a transitional stream (or see paths=[] mid-reconnect) and then burn the
// full Medium timeout on the second.
func waitPathReplacedOnAll(t *testing.T, have, gone string) {
	t.Helper()
	waitExactPathsOnAll(t, []string{have}, []string{gone})
}

// waitExactPathsOnAll waits until every target has all want paths, none of the
// gone paths, and exactly one stream — in a single snapshot per poll.
func waitExactPathsOnAll(t *testing.T, want, gone []string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("paths want=%v gone=%v on all targets", want, gone), func() (bool, string) {
		for _, name := range allTargets {
			if n := s.GnmiGen.StreamCount(name); n != 1 {
				return false, fmt.Sprintf("%s streams=%d paths=%v", name, n, s.GnmiGen.Paths(name))
			}
			paths := s.GnmiGen.Paths(name)
			for _, w := range want {
				found := false
				for _, p := range paths {
					if p == w {
						found = true
						break
					}
				}
				if !found {
					return false, fmt.Sprintf("%s paths=%v", name, paths)
				}
			}
			for _, g := range gone {
				for _, p := range paths {
					if p == g {
						return false, fmt.Sprintf("%s still has %s: %v", name, g, paths)
					}
				}
			}
		}
		return true, ""
	})
}

func waitPathAbsentOnAll(t *testing.T, path string) {
	t.Helper()
	for _, name := range allTargets {
		s.GnmiGen.WaitPathAbsent(t, name, path)
	}
}

func firstStream(t *testing.T, target string) harness.Subscription {
	t.Helper()
	var out harness.Subscription
	harness.Wait(t, harness.Medium, fmt.Sprintf("one stream on %s", target), func() (bool, string) {
		subs, err := s.GnmiGen.Subscriptions(target)
		if err != nil {
			return false, err.Error()
		}
		if len(subs) != 1 {
			return false, fmt.Sprintf("got %d streams", len(subs))
		}
		out = subs[0]
		return true, ""
	})
	return out
}

func waitEntryMode(t *testing.T, target, want string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("entry mode %s on %s", want, target), func() (bool, string) {
		subs, err := s.GnmiGen.Subscriptions(target)
		if err != nil {
			return false, err.Error()
		}
		if len(subs) != 1 || len(subs[0].Entries) == 0 {
			return false, fmt.Sprintf("streams=%d", len(subs))
		}
		got := subs[0].Entries[0].Mode
		return got == want, fmt.Sprintf("got %s", got)
	})
}

func waitEffectiveSample(t *testing.T, target, want string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("effective_sample_interval=%s on %s", want, target), func() (bool, string) {
		subs, err := s.GnmiGen.Subscriptions(target)
		if err != nil {
			return false, err.Error()
		}
		if len(subs) != 1 {
			return false, fmt.Sprintf("streams=%d", len(subs))
		}
		got := subs[0].EffectiveSampleInterval
		if got != want {
			return false, fmt.Sprintf("effective=%s", got)
		}
		if len(subs[0].Entries) == 0 {
			return false, "no entries"
		}
		if entry := subs[0].Entries[0].SampleInterval; entry != want {
			return false, fmt.Sprintf("entry=%s", entry)
		}
		return true, ""
	})
}

func waitUpdatesOnly(t *testing.T, target string, want bool) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("updates_only=%v on %s", want, target), func() (bool, string) {
		subs, err := s.GnmiGen.Subscriptions(target)
		if err != nil {
			return false, err.Error()
		}
		if len(subs) != 1 {
			return false, fmt.Sprintf("streams=%d", len(subs))
		}
		got := subs[0].UpdatesOnly
		return got == want, fmt.Sprintf("got %v", got)
	})
}

func waitUpdatesOnlyOnAll(t *testing.T, want bool) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("updates_only=%v on all targets", want), func() (bool, string) {
		for _, name := range allTargets {
			subs, err := s.GnmiGen.Subscriptions(name)
			if err != nil {
				return false, err.Error()
			}
			if len(subs) != 1 {
				return false, fmt.Sprintf("%s streams=%d", name, len(subs))
			}
			if subs[0].UpdatesOnly != want {
				return false, fmt.Sprintf("%s updates_only=%v", name, subs[0].UpdatesOnly)
			}
		}
		return true, ""
	})
}

func waitSuppressAndHeartbeat(t *testing.T, target string, suppress bool, heartbeat string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("suppress=%v heartbeat=%s on %s", suppress, heartbeat, target), func() (bool, string) {
		subs, err := s.GnmiGen.Subscriptions(target)
		if err != nil {
			return false, err.Error()
		}
		if len(subs) != 1 || len(subs[0].Entries) == 0 {
			return false, fmt.Sprintf("streams=%d", len(subs))
		}
		e := subs[0].Entries[0]
		if e.SuppressRedundant != suppress {
			return false, fmt.Sprintf("suppress=%v", e.SuppressRedundant)
		}
		if heartbeat == "" {
			if e.HeartbeatInterval != "" && e.HeartbeatInterval != "0s" {
				return false, fmt.Sprintf("heartbeat still %s", e.HeartbeatInterval)
			}
			return true, ""
		}
		return e.HeartbeatInterval == heartbeat, fmt.Sprintf("heartbeat=%s", e.HeartbeatInterval)
	})
}

func notificationDelta(target string, window time.Duration) int {
	start := s.GnmiGen.Notifications(target)
	time.Sleep(window)
	return s.GnmiGen.Notifications(target) - start
}

func waitPromContains(t *testing.T, fragment string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("prometheus contains %s", fragment), func() (bool, string) {
		body := s.K8s.ScrapeClusterPrometheus(t, cluster, pipeline, output)
		if strings.Contains(body, fragment) {
			return true, ""
		}
		return false, fmt.Sprintf("%d samples", harness.SampleCount(body))
	})
}

func waitPromLacks(t *testing.T, fragment string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("prometheus lacks %s", fragment), func() (bool, string) {
		body := s.K8s.ScrapeClusterPrometheus(t, cluster, pipeline, output)
		if !strings.Contains(body, fragment) {
			return true, ""
		}
		return false, "still present"
	})
}

func TestSub001_PathChangeReplacesCollectedPaths(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{"Paths": []string{pathIF}})
	waitStreams(t, 1)
	waitPathOnAll(t, pathIF)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	patchSubscription(t, "s1", map[string]any{"paths": []string{pathCPU}})
	harness.WaitConfigApplied(t, s.K8s, cluster)

	waitPathReplacedOnAll(t, pathCPU, pathIF)
	waitStreams(t, 1)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))

	waitPromContains(t, "platform_control_process")
	waitPromLacks(t, "interface_statistics")
}

func TestSub002_AddingPathExtendsSubscription(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{"Paths": []string{pathIF}})
	waitStreams(t, 1)
	waitPathOnAll(t, pathIF)
	s.GnmiGen.WaitNotificationsAdvance(t, leaf1)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	patchSubscription(t, "s1", map[string]any{"paths": []string{pathIF, pathCPU}})

	waitPathsOnAll(t, pathIF, pathCPU)
	// Path edits re-establish the Subscribe stream, which resets
	// notifications_sent. Prove data still flows on the new stream rather
	// than comparing absolute counters across the teardown.
	s.GnmiGen.WaitNotificationsAdvance(t, leaf1)
	waitStreams(t, 1)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestSub003_SampleIntervalChange(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{
		"Paths":          []string{pathIF},
		"SampleInterval": "10s",
	})
	waitStreams(t, 1)
	waitEffectiveSample(t, leaf1, "10s")
	slow := notificationDelta(leaf1, 10*time.Second)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	patchSubscription(t, "s1", map[string]any{"sampleInterval": "2s"})
	waitEffectiveSample(t, leaf1, "2s")
	fast := notificationDelta(leaf1, 10*time.Second)

	if fast <= slow {
		t.Errorf("notification rate did not increase after interval shrink: slow=%d fast=%d over 10s", slow, fast)
	}
	waitStreams(t, 1)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestSub004_StreamModeChange(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{
		"Mode":           "STREAM/SAMPLE",
		"SampleInterval": "1s",
		"Paths":          []string{pathIF},
	})
	waitStreams(t, 1)
	waitEntryMode(t, leaf1, "SAMPLE")

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	patchSubscription(t, "s1", map[string]any{"mode": "STREAM/ON_CHANGE"})
	waitEntryMode(t, leaf1, "ON_CHANGE")
	waitStreams(t, 1)
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, allTargets...)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestSub005_UpdatesOnlyReachesWire(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{
		"Paths":       []string{pathIF},
		"UpdatesOnly": false,
	})
	waitStreams(t, 1)
	waitUpdatesOnlyOnAll(t, false)
	syncBefore := firstStream(t, leaf1).SyncMessagesSent

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	patchSubscription(t, "s1", map[string]any{"updatesOnly": true})
	harness.WaitConfigApplied(t, s.K8s, cluster)
	// Assert the flag on every target in one snapshot. Waiting only on leaf1
	// then waitStreams(leaf2) races a mid-reconnect gap where leaf2 has
	// streams=0 after the apply ACK (ApplyCache verify re-POST recovers it).
	waitUpdatesOnlyOnAll(t, true)

	syncAfter := firstStream(t, leaf1).SyncMessagesSent
	if syncBefore == 0 && syncAfter == 0 {
		t.Log("sync_messages_sent was 0 both before and after; flag still asserted on wire")
	} else if syncAfter >= syncBefore && syncBefore > 0 {
		// Soft signal only — gnmi-gen may still emit a sync response.
		t.Logf("sync_messages_sent before=%d after=%d", syncBefore, syncAfter)
	}
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestSub006_SuppressRedundantAndHeartbeat(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{
		"Paths":          []string{pathIF},
		"SampleInterval": "1s",
	})
	waitStreams(t, 1)
	waitSuppressAndHeartbeat(t, leaf1, false, "")

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	patchSubscription(t, "s1", map[string]any{
		"suppressRedundant": true,
		"heartbeatInterval": "30s",
	})
	waitSuppressAndHeartbeat(t, leaf1, true, "30s")

	patchSubscription(t, "s1", map[string]any{
		"suppressRedundant": false,
		"heartbeatInterval": nil,
	})
	waitSuppressAndHeartbeat(t, leaf1, false, "")
	waitStreams(t, 1)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestSub007_PerTargetSubscriptionScoped(t *testing.T) {
	t.Skip("spec.target is the gNMI SubscribeRequest target field, not Target-CR scoping; CR-level pin is not implemented")
}

func TestSub008_PrefixApplied(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "absolute", map[string]any{
		"Paths": []string{pathIF},
	})
	waitStreams(t, 1)
	waitPathOnAll(t, pathIF)
	want := s.GnmiGen.Paths(leaf1)
	deleteSubscription(t, "absolute")
	// Last subscription gone: disable the pipeline so gNMIc accepts an empty
	// apply and tears streams down (targets-without-subscriptions is invalid).
	setPipelineEnabled(t, false)
	waitStreams(t, 0)

	applySubscription(t, "prefixed", map[string]any{
		"Prefix":         "/interface",
		"Paths":          []string{"/statistics"},
		"SampleInterval": "1s",
	})
	waitStreams(t, 1)
	waitPathOnAll(t, pathIF)
	got := s.GnmiGen.Paths(leaf1)
	if !samePathSet(want, got) {
		t.Fatalf("prefix-joined paths %v do not match absolute paths %v", got, want)
	}
}

func TestSub009_DeletingSubscriptionStopsPaths(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "if", map[string]any{"Paths": []string{pathIF}})
	applySubscription(t, "cpu", map[string]any{"Paths": []string{pathCPU}})
	waitStreams(t, 2)
	waitPathsOnAll(t, pathIF, pathCPU)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		SubscriptionsCount: harness.I32(2),
	})
	s.GnmiGen.WaitNotificationsAdvance(t, leaf1)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	deleteSubscription(t, "cpu")
	waitPathAbsentOnAll(t, pathCPU)
	waitPathOnAll(t, pathIF)
	waitStreams(t, 1)
	s.GnmiGen.WaitNotificationsAdvance(t, leaf1)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		SubscriptionsCount: harness.I32(1),
	})
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))

	deleteSubscription(t, "if")
	setPipelineEnabled(t, false)
	waitStreams(t, 0)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		SubscriptionsCount: harness.I32(0),
	})
}

func TestSub010_RapidSuccessiveEditsConverge(t *testing.T) {
	waitIdle(t)
	applySubscription(t, "s1", map[string]any{"Paths": []string{pathIF}})
	waitStreams(t, 1)
	waitPathOnAll(t, pathIF)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	since := time.Now()
	paths := [][]string{
		{pathCPU},
		{pathSys},
		{pathIF},
		{pathCPU, pathSys},
		{pathIF},
	}
	for _, p := range paths {
		patchSubscription(t, "s1", map[string]any{"paths": p})
	}
	harness.WaitConfigApplied(t, s.K8s, cluster)

	// Final state in one snapshot: pathIF present, prior paths gone, one stream.
	// Split present/absent waits race reconnect gaps after the patch burst.
	waitExactPathsOnAll(t, []string{pathIF}, []string{pathCPU, pathSys})
	s.GnmiGen.ConsistentlyCollectedOnce(t, 15*time.Second, 1, allTargets...)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
	harness.AssertNoPanics(t, s.K8s.OperatorLogs(t, time.Since(since)+time.Minute))
}

func samePathSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, p := range a {
		seen[p]++
	}
	for _, p := range b {
		seen[p]--
		if seen[p] < 0 {
			return false
		}
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}
