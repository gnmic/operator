//go:build integration

// Package targetsource covers TargetSource end to end: devices a source
// describes become Targets the cluster collects, the guards that stand
// between a diff and a deletion, ownership rules against hand-made Targets,
// the watches that make runs happen without waiting for the interval, and the
// refresh webhook.
//
// Every TargetSource here has interval 1h. A run that happens within seconds
// therefore happened because of a watch (ConfigMap data, owned Target, spec
// generation) or the webhook -- never because the timer fired.
//
// Test -> ID:
//
//	TestTS001_StaticSourceCreatesCollectedTargets  -> 009-1
//	TestTS002_ConfigMapEditsRunWithoutInterval     -> 009-2
//	TestTS003_PruneGuardHoldsLargeDeletions        -> 009-3
//	TestTS004_EmptySourceHeldUnlessAllowed         -> 009-4
//	TestTS005_UnmanagedNameIsAConflict             -> 009-5
//	TestTS006_HandDeletedTargetIsRestored          -> 009-6
//	TestTS007_SuspendStopsRunsResumeCatchesUp      -> 009-7
//	TestTS008_MissingConfigMapStallsUntilCreated   -> 009-8
//	TestTS009_WebhookRefresh                       -> 009-9
//	TestTS010_MaxTargetsHoldsEverything            -> 009-10
//	TestTS011_DeletingTheSourceCollectsItsTargets  -> 009-11
//
// Run:
//
//	make integration-test-009-targetsource
package targetsource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster = "c1"

	leaf1 = "leaf1"
	leaf2 = "leaf2"
	leaf3 = "leaf3"
	leaf4 = "leaf4"

	profile = "default"

	// Condition vocabulary, mirrored from internal/controller/targetsource_status.go
	// and internal/discovery/prune.go.
	condReady      = "Ready"
	condStalled    = "Stalled"
	condConflicted = "Conflicted"

	reasonSucceeded        = "Succeeded"
	reasonSuspended        = "Suspended"
	reasonCapacityExceeded = "CapacityExceeded"
	reasonNameConflict     = "NameConflict"
	reasonPruneGuard       = "PruneGuard"
	reasonEmptySource      = "EmptySource"
	reasonConfigMapMissing = "ConfigMapNotFound"

	operatorAPIPort = 8082
	hookToken       = "refresh-me-please" // fixtures/baseline.yaml Secret hook-token
)

var allLeaves = []string{leaf1, leaf2, leaf3, leaf4}

type device struct {
	Name    string
	Address string
}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "009-targetsource",
		RequireTargets: allLeaves,
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

// --- fixtures ---------------------------------------------------------------

// simAddress is the host:port a simulated leaf listens on (leaf1 -> :57400).
func simAddress(leaf string) string {
	n := int(leaf[len(leaf)-1] - '0')
	return harness.GnmiGenAddress(s.Namespace, n-1)
}

func devices(leaves ...string) []device {
	out := make([]device, 0, len(leaves))
	for _, l := range leaves {
		out = append(out, device{Name: l, Address: simAddress(l)})
	}
	return out
}

type staticOpts struct {
	collect    bool
	suspend    bool
	maxTargets int
	webhook    bool
}

func applyStatic(t *testing.T, name string, devs []device, o staticOpts) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/targetsource-static.yaml", map[string]any{
		"Name":       name,
		"Devices":    devs,
		"Collect":    o.collect,
		"Suspend":    o.suspend,
		"MaxTargets": o.maxTargets,
		"Webhook":    o.webhook,
	})
}

func applyInventory(t *testing.T, cmName string, devs []device) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/inventory.yaml", map[string]any{"Name": cmName, "Devices": devs})
}

func applyConfigMapSource(t *testing.T, name, cmName string, allowEmpty bool, maxDeleteRatio int) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/targetsource-configmap.yaml", map[string]any{
		"Name":             name,
		"ConfigMap":        cmName,
		"AllowEmptySource": allowEmpty,
		"MaxDeleteRatio":   maxDeleteRatio,
	})
}

// targetName is the name the source gives a discovered device.
func targetName(source, dev string) string {
	n, ok := discovery.TargetName(source, dev)
	if !ok {
		panic("unrepresentable target name: " + source + "/" + dev)
	}
	return n
}

// --- observation helpers -----------------------------------------------------

func getSource(t *testing.T, name string) *gnmicv1alpha1.TargetSource {
	t.Helper()
	var ts gnmicv1alpha1.TargetSource
	if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: name}, &ts); err != nil {
		t.Fatalf("getting TargetSource %s: %v", name, err)
	}
	return &ts
}

func sourceQuiet(name string) (*gnmicv1alpha1.TargetSource, error) {
	var ts gnmicv1alpha1.TargetSource
	err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: name}, &ts)
	return &ts, err
}

// waitCondition waits for a condition with the given status and reason.
func waitCondition(t *testing.T, source, condType string, status metav1.ConditionStatus, reason string) *gnmicv1alpha1.TargetSource {
	t.Helper()
	var last *gnmicv1alpha1.TargetSource
	harness.Wait(t, harness.Medium, fmt.Sprintf("TargetSource %s %s=%s/%s", source, condType, status, reason), func() (bool, string) {
		ts, err := sourceQuiet(source)
		if err != nil {
			return false, err.Error()
		}
		last = ts
		c := apimeta.FindStatusCondition(ts.Status.Conditions, condType)
		if c == nil {
			return false, "condition absent"
		}
		ok := c.Status == status && (reason == "" || c.Reason == reason)
		return ok, fmt.Sprintf("status=%s reason=%s msg=%q", c.Status, c.Reason, c.Message)
	})
	return last
}

func waitConditionAbsent(t *testing.T, source, condType string) {
	t.Helper()
	harness.Wait(t, harness.Medium, fmt.Sprintf("TargetSource %s has no %s condition", source, condType), func() (bool, string) {
		ts, err := sourceQuiet(source)
		if err != nil {
			return false, err.Error()
		}
		return apimeta.FindStatusCondition(ts.Status.Conditions, condType) == nil, "present"
	})
}

func waitManaged(t *testing.T, source string, want int32) *gnmicv1alpha1.TargetSource {
	t.Helper()
	var last *gnmicv1alpha1.TargetSource
	harness.Wait(t, harness.Medium, fmt.Sprintf("TargetSource %s managed=%d", source, want), func() (bool, string) {
		ts, err := sourceQuiet(source)
		if err != nil {
			return false, err.Error()
		}
		last = ts
		return ts.Status.Managed == want, fmt.Sprintf("managed=%d discovered=%d pruned=%d", ts.Status.Managed, ts.Status.Discovered, ts.Status.Pruned)
	})
	return last
}

// ownedTargets lists the Targets carrying the source's label.
func ownedTargets(source string) (map[string]gnmicv1alpha1.Target, error) {
	var list gnmicv1alpha1.TargetList
	if err := s.K8s.Client.List(s.Ctx, &list, client.InNamespace(s.Namespace),
		client.MatchingLabels{discovery.LabelTargetSource: source}); err != nil {
		return nil, err
	}
	out := make(map[string]gnmicv1alpha1.Target, len(list.Items))
	for _, tg := range list.Items {
		out[tg.Name] = tg
	}
	return out, nil
}

func waitTargetsExactly(t *testing.T, source string, names ...string) map[string]gnmicv1alpha1.Target {
	t.Helper()
	want := map[string]struct{}{}
	for _, n := range names {
		want[n] = struct{}{}
	}
	var last map[string]gnmicv1alpha1.Target
	harness.Wait(t, harness.Medium, fmt.Sprintf("%s owns exactly %v", source, names), func() (bool, string) {
		got, err := ownedTargets(source)
		if err != nil {
			return false, err.Error()
		}
		last = got
		if len(got) != len(want) {
			return false, fmt.Sprintf("have %d: %v", len(got), keys(got))
		}
		for n := range want {
			if _, ok := got[n]; !ok {
				return false, fmt.Sprintf("missing %s; have %v", n, keys(got))
			}
		}
		return true, ""
	})
	return last
}

func keys(m map[string]gnmicv1alpha1.Target) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func patchSource(t *testing.T, name, patch string) {
	t.Helper()
	s.K8s.Patch(t, getSource(t, name), patch)
}

// --- tests ------------------------------------------------------------------

// TestTS001_StaticSourceCreatesCollectedTargets: devices in, Targets out, with
// the ownership markers the rest of the suite relies on -- and the Pipeline
// collects them, so the path goes all the way to a stream on the device.
func TestTS001_StaticSourceCreatesCollectedTargets(t *testing.T) {
	const src = "inv"
	harness.WaitClusterReady(t, s.K8s, cluster)
	applyStatic(t, src, devices(leaf1, leaf2), staticOpts{collect: true})

	ts := waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
	if ts.Status.Managed != 2 || ts.Status.Discovered != 2 || ts.Status.Invalid != 0 {
		t.Errorf("status counters: managed=%d discovered=%d invalid=%d", ts.Status.Managed, ts.Status.Discovered, ts.Status.Invalid)
	}
	if ts.Status.SourceDigest == "" || ts.Status.LastSuccessfulSyncTime == nil {
		t.Errorf("status lacks digest or lastSuccessfulSyncTime: %+v", ts.Status)
	}

	targets := waitTargetsExactly(t, src, targetName(src, leaf1), targetName(src, leaf2))
	tg := targets[targetName(src, leaf1)]
	if tg.Spec.Address != simAddress(leaf1) || tg.Spec.Profile != profile {
		t.Errorf("target spec = %+v", tg.Spec)
	}
	if tg.Labels[discovery.LabelManagedBy] != discovery.LabelManagedByValue || tg.Labels["source"] != src || tg.Labels["collect"] != "yes" {
		t.Errorf("target labels = %v", tg.Labels)
	}
	if tg.Annotations[discovery.AnnotationHash] == "" || tg.Annotations[discovery.AnnotationDiscoveredName] != leaf1 {
		t.Errorf("target annotations = %v", tg.Annotations)
	}
	harness.AssertOwnedBy(t, &tg, "TargetSource", src)

	// Collected: the Pipeline's selector matched and the collector subscribed.
	for _, l := range []string{leaf1, leaf2} {
		s.GnmiGen.WaitStreams(t, l, 1)
	}
}

// TestTS002_ConfigMapEditsRunWithoutInterval: editing the inventory ConfigMap
// is enough. An address change updates the Target in place; removing one of
// four devices (25%, under the default 50% guard) prunes it.
func TestTS002_ConfigMapEditsRunWithoutInterval(t *testing.T) {
	const src, cm = "cminv", "cminv-inventory"
	applyInventory(t, cm, devices(leaf1, leaf2, leaf3, leaf4))
	applyConfigMapSource(t, src, cm, false, 0)
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
	waitTargetsExactly(t, src, targetName(src, leaf1), targetName(src, leaf2), targetName(src, leaf3), targetName(src, leaf4))
	before := getSource(t, src).Status.SourceDigest

	// Move leaf2 to leaf4's port: an in-place update, no delete.
	moved := devices(leaf1, leaf2, leaf3, leaf4)
	moved[1].Address = simAddress(leaf4)
	applyInventory(t, cm, moved)
	harness.Wait(t, harness.Medium, "leaf2's address updated from the ConfigMap edit", func() (bool, string) {
		got, err := ownedTargets(src)
		if err != nil {
			return false, err.Error()
		}
		return got[targetName(src, leaf2)].Spec.Address == simAddress(leaf4), got[targetName(src, leaf2)].Spec.Address
	})
	// Status is written once, at the end of the run, after the Targets; the
	// digest can lag the Target update by a moment.
	harness.Wait(t, harness.Short, "sourceDigest reflects the edit", func() (bool, string) {
		ts, err := sourceQuiet(src)
		if err != nil {
			return false, err.Error()
		}
		return ts.Status.SourceDigest != before, "digest unchanged: " + ts.Status.SourceDigest
	})

	// Drop leaf4: 1 of 4 is within the guard, so it is pruned.
	applyInventory(t, cm, devices(leaf1, leaf2, leaf3))
	waitTargetsExactly(t, src, targetName(src, leaf1), targetName(src, leaf2), targetName(src, leaf3))
	ts := waitManaged(t, src, 3)
	if ts.Status.Pruned != 1 {
		t.Errorf("pruned = %d, want 1", ts.Status.Pruned)
	}
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
}

// TestTS003_PruneGuardHoldsLargeDeletions: removing 3 of 4 devices is over the
// default maxDeleteRatio; creates and updates still apply but nothing is
// deleted until the ratio is raised.
func TestTS003_PruneGuardHoldsLargeDeletions(t *testing.T) {
	const src, cm = "guard", "guard-inventory"
	applyInventory(t, cm, devices(leaf1, leaf2, leaf3, leaf4))
	applyConfigMapSource(t, src, cm, false, 0)
	waitManaged(t, src, 4)

	applyInventory(t, cm, devices(leaf1))
	ts := waitCondition(t, src, condReady, metav1.ConditionFalse, reasonPruneGuard)
	if ts.Status.Managed != 4 || ts.Status.Pruned != 0 {
		t.Errorf("guard did not hold: managed=%d pruned=%d", ts.Status.Managed, ts.Status.Pruned)
	}
	harness.Consistently(t, 10*time.Second, 2*time.Second, "all four Targets survive under the guard", func() (bool, string) {
		got, err := ownedTargets(src)
		if err != nil {
			return false, err.Error()
		}
		return len(got) == 4, fmt.Sprintf("%d left", len(got))
	})

	// Raising the ratio is the operator's consent; the deletions then apply.
	patchSource(t, src, `{"spec":{"prune":{"maxDeleteRatio":100}}}`)
	waitTargetsExactly(t, src, targetName(src, leaf1))
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
}

// TestTS004_EmptySourceHeldUnlessAllowed: a source that suddenly returns
// nothing is treated as a broken source, not as "delete everything", unless
// prune.allowEmptySource says otherwise.
func TestTS004_EmptySourceHeldUnlessAllowed(t *testing.T) {
	const src, cm = "empty", "empty-inventory"
	applyInventory(t, cm, devices(leaf1, leaf2))
	applyConfigMapSource(t, src, cm, false, 0)
	waitManaged(t, src, 2)

	applyInventory(t, cm, nil)
	ts := waitCondition(t, src, condReady, metav1.ConditionFalse, reasonEmptySource)
	if ts.Status.Managed != 2 || ts.Status.Discovered != 0 {
		t.Errorf("empty source: managed=%d discovered=%d, want 2/0", ts.Status.Managed, ts.Status.Discovered)
	}

	patchSource(t, src, `{"spec":{"prune":{"allowEmptySource":true,"maxDeleteRatio":100}}}`)
	waitTargetsExactly(t, src)
	waitManaged(t, src, 0)
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
}

// TestTS005_UnmanagedNameIsAConflict: a Target somebody made by hand is never
// adopted or overwritten. The source reports the conflict and manages the rest.
func TestTS005_UnmanagedNameIsAConflict(t *testing.T) {
	const src = "conf"
	human := targetName(src, leaf1)
	s.K8s.ApplyFile(t, "fixtures/target.yaml", map[string]any{"Name": human, "Address": "192.0.2.1:57400"})
	applyStatic(t, src, devices(leaf1, leaf2), staticOpts{})

	ts := waitCondition(t, src, condConflicted, metav1.ConditionTrue, reasonNameConflict)
	if ts.Status.Conflicted != 1 || ts.Status.Managed != 1 {
		t.Errorf("conflicted=%d managed=%d, want 1/1", ts.Status.Conflicted, ts.Status.Managed)
	}
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
	waitTargetsExactly(t, src, targetName(src, leaf2))

	got := s.K8s.Target(t, human)
	if got.Spec.Address != "192.0.2.1:57400" || got.Labels["owner"] != "human" || got.Labels[discovery.LabelTargetSource] != "" {
		t.Errorf("the human's Target was touched: %+v %v", got.Spec, got.Labels)
	}
	if len(got.OwnerReferences) != 0 {
		t.Errorf("the human's Target acquired owner references: %v", got.OwnerReferences)
	}
}

// TestTS006_HandDeletedTargetIsRestored: the source owns its Targets; deleting
// one by hand is undone on the next run, which the owner watch triggers.
func TestTS006_HandDeletedTargetIsRestored(t *testing.T) {
	const src = "heal"
	applyStatic(t, src, devices(leaf1), staticOpts{})
	targets := waitTargetsExactly(t, src, targetName(src, leaf1))
	first := targets[targetName(src, leaf1)]

	s.K8s.DeleteNow(t, &first)
	harness.Wait(t, harness.Medium, "deleted Target recreated", func() (bool, string) {
		got, err := ownedTargets(src)
		if err != nil {
			return false, err.Error()
		}
		tg, ok := got[targetName(src, leaf1)]
		if !ok {
			return false, "absent"
		}
		return tg.UID != first.UID, "same UID as the deleted object"
	})
}

// TestTS007_SuspendStopsRunsResumeCatchesUp: suspend freezes the source --
// spec changes are not acted on -- and clearing it applies what accumulated.
func TestTS007_SuspendStopsRunsResumeCatchesUp(t *testing.T) {
	const src = "susp"
	applyStatic(t, src, devices(leaf1), staticOpts{})
	waitManaged(t, src, 1)

	patchSource(t, src, `{"spec":{"suspend":true}}`)
	waitCondition(t, src, condReady, metav1.ConditionFalse, reasonSuspended)

	// A spec change while suspended: no run.
	applyStatic(t, src, devices(leaf1, leaf2), staticOpts{suspend: true})
	harness.Consistently(t, 10*time.Second, 2*time.Second, "no Target created while suspended", func() (bool, string) {
		got, err := ownedTargets(src)
		if err != nil {
			return false, err.Error()
		}
		return len(got) == 1, fmt.Sprintf("%d Targets", len(got))
	})

	applyStatic(t, src, devices(leaf1, leaf2), staticOpts{})
	waitTargetsExactly(t, src, targetName(src, leaf1), targetName(src, leaf2))
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
}

// TestTS008_MissingConfigMapStallsUntilCreated: a source naming a ConfigMap
// that does not exist is Stalled -- a retry cannot fix it -- and recovers the
// moment the ConfigMap appears, with no edit to the TargetSource.
func TestTS008_MissingConfigMapStallsUntilCreated(t *testing.T) {
	const src, cm = "late", "late-inventory"
	applyConfigMapSource(t, src, cm, false, 0)
	ts := waitCondition(t, src, condStalled, metav1.ConditionTrue, reasonConfigMapMissing)
	if ts.Status.ConsecutiveFailures == 0 || ts.Status.LastError == "" {
		t.Errorf("stalled status not recorded: %+v", ts.Status)
	}

	applyInventory(t, cm, devices(leaf3))
	waitConditionAbsent(t, src, condStalled)
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
	waitTargetsExactly(t, src, targetName(src, leaf3))
}

// TestTS009_WebhookRefresh drives the operator API: no token is 401, a good
// token is 202 and a run happens now, a second call inside the debounce window
// is 200 with debounced=true.
func TestTS009_WebhookRefresh(t *testing.T) {
	const src = "hook"
	applyStatic(t, src, devices(leaf1), staticOpts{webhook: true})
	before := waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
	// metav1.Time has second precision. A refresh run that finishes within the
	// same second as the initial run would be indistinguishable from no run at
	// all, so step past that second before asking for one.
	if d := time.Until(before.Status.LastSyncTime.Time.Add(1100 * time.Millisecond)); d > 0 {
		time.Sleep(d)
	}

	pods := s.K8s.OperatorPods(t)
	fw, err := s.K8s.ForwardPodPortIn(harness.OperatorNamespace, pods[0].Name, operatorAPIPort)
	if err != nil {
		t.Fatalf("forwarding operator API: %v", err)
	}
	t.Cleanup(fw.Close)
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/targetsources/%s/refresh", fw.URL, s.Namespace, src)

	post := func(token string) (int, []byte) {
		req, _ := http.NewRequestWithContext(s.Ctx, http.MethodPost, url, bytes.NewReader([]byte(`{"event":"test"}`)))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", url, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}

	if code, body := post(""); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d %s, want 401", code, body)
	}
	if code, body := post("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d %s, want 401", code, body)
	}

	code, body := post(hookToken)
	if code != http.StatusAccepted {
		t.Fatalf("good token: %d %s, want 202", code, body)
	}
	var first struct {
		Debounced   bool      `json:"debounced"`
		RequestedAt time.Time `json:"requestedAt"`
	}
	if err := json.Unmarshal(body, &first); err != nil || first.Debounced || first.RequestedAt.IsZero() {
		t.Fatalf("202 body = %s (err %v)", body, err)
	}

	// Right behind it: debounced, same requestedAt.
	code, body = post(hookToken)
	var second struct {
		Debounced   bool      `json:"debounced"`
		RequestedAt time.Time `json:"requestedAt"`
	}
	if err := json.Unmarshal(body, &second); err != nil || code != http.StatusOK || !second.Debounced || !second.RequestedAt.Equal(first.RequestedAt) {
		t.Fatalf("second call: %d %s (err %v), want 200 debounced with the same requestedAt", code, body, err)
	}

	// The annotation is what the controller reacts to, and it did: a run
	// finished after the request, on a source whose interval is an hour.
	harness.Wait(t, harness.Medium, "refresh produced a run", func() (bool, string) {
		ts, err := sourceQuiet(src)
		if err != nil {
			return false, err.Error()
		}
		if ts.Annotations[discovery.AnnotationRequestedAt] == "" {
			return false, "requested-at annotation not set"
		}
		if ts.Status.LastSyncTime == nil || !ts.Status.LastSyncTime.After(before.Status.LastSyncTime.Time) {
			return false, fmt.Sprintf("lastSyncTime %v not after %v", ts.Status.LastSyncTime, before.Status.LastSyncTime)
		}
		return true, ""
	})
}

// TestTS010_MaxTargetsHoldsEverything: over spec.maxTargets nothing is
// written -- not even the first N -- and raising the limit applies the set.
func TestTS010_MaxTargetsHoldsEverything(t *testing.T) {
	const src = "cap"
	applyStatic(t, src, devices(leaf1, leaf2, leaf3), staticOpts{maxTargets: 2})
	ts := waitCondition(t, src, condReady, metav1.ConditionFalse, reasonCapacityExceeded)
	if ts.Status.Managed != 0 {
		t.Errorf("managed = %d with capacity exceeded, want 0", ts.Status.Managed)
	}
	harness.Consistently(t, 5*time.Second, time.Second, "no Target created over capacity", func() (bool, string) {
		got, err := ownedTargets(src)
		return err == nil && len(got) == 0, fmt.Sprintf("%d Targets", len(got))
	})

	applyStatic(t, src, devices(leaf1, leaf2, leaf3), staticOpts{maxTargets: 3})
	waitTargetsExactly(t, src, targetName(src, leaf1), targetName(src, leaf2), targetName(src, leaf3))
	waitCondition(t, src, condReady, metav1.ConditionTrue, reasonSucceeded)
}

// TestTS011_DeletingTheSourceCollectsItsTargets: the Targets go with the
// source through garbage collection, and the collector stops streaming from
// the device.
func TestTS011_DeletingTheSourceCollectsItsTargets(t *testing.T) {
	const src = "gc"
	applyStatic(t, src, devices(leaf3), staticOpts{collect: true})
	waitTargetsExactly(t, src, targetName(src, leaf3))
	s.GnmiGen.WaitStreams(t, leaf3, 1)

	s.K8s.DeleteNow(t, getSource(t, src))
	harness.Wait(t, harness.Medium, "owned Target garbage-collected", func() (bool, string) {
		var tg gnmicv1alpha1.Target
		err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: targetName(src, leaf3)}, &tg)
		return apierrors.IsNotFound(err), fmt.Sprintf("err=%v deletionTimestamp=%v", err, tg.DeletionTimestamp)
	})
	s.GnmiGen.WaitStreams(t, leaf3, 0)
	var ts gnmicv1alpha1.TargetSource
	if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: src}, &ts); !apierrors.IsNotFound(err) {
		t.Errorf("TargetSource still present: %v", err)
	}
}
