package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func statusScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := gnmicv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func runningState(pod string, at time.Time) gnmicv1alpha1.ClusterTargetState {
	return gnmicv1alpha1.ClusterTargetState{
		Pod:             pod,
		State:           "running",
		ConnectionState: "READY",
		Subscriptions:   map[string]string{"sub1": "running"},
		LastUpdated:     metav1.NewTime(at),
	}
}

func TestClusterTargetStateEqual(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	base := runningState("gnmic-c1-0", at)

	if !clusterTargetStateEqual(base, runningState("gnmic-c1-0", at)) {
		t.Fatal("identical states compared unequal; nothing would ever be suppressed")
	}

	moved := runningState("gnmic-c1-1", at)
	if clusterTargetStateEqual(base, moved) {
		t.Fatal("a pod move must not be suppressed: status is the only record of assignment")
	}

	degraded := runningState("gnmic-c1-0", at)
	degraded.ConnectionState = "TRANSIENT_FAILURE"
	if clusterTargetStateEqual(base, degraded) {
		t.Fatal("connection state change was suppressed")
	}

	transitioned := runningState("gnmic-c1-0", at.Add(time.Second))
	if clusterTargetStateEqual(base, transitioned) {
		t.Fatal("LastUpdated change was suppressed")
	}

	subChanged := runningState("gnmic-c1-0", at)
	subChanged.Subscriptions = map[string]string{"sub1": "stopped"}
	if clusterTargetStateEqual(base, subChanged) {
		t.Fatal("subscription state change was suppressed")
	}

	subAdded := runningState("gnmic-c1-0", at)
	subAdded.Subscriptions = map[string]string{"sub1": "running", "sub2": "running"}
	if clusterTargetStateEqual(base, subAdded) {
		t.Fatal("added subscription was suppressed")
	}
}

// The whole point of the item: a poll that observes no change must not write.
func TestApplyClusterState_UnchangedDoesNotWrite(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	scheme := statusScheme(t)
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "leaf1", Namespace: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			Clusters:      1,
			State:         "READY",
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{"c1": runningState("gnmic-c1-0", at)},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(target).WithStatusSubresource(&gnmicv1alpha1.Target{}).Build()
	r := &TargetStateReconciler{Client: cl, Scheme: scheme}
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	before := readVersion(t, cl, nn)
	r.applyClusterState(context.Background(), nn, "c1", runningState("gnmic-c1-0", at), logf.Log)
	if after := readVersion(t, cl, nn); after != before {
		t.Fatalf("resourceVersion moved %s -> %s on an unchanged state; the write was not suppressed", before, after)
	}
}

func TestApplyClusterState_ChangeIsWritten(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	scheme := statusScheme(t)
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "leaf1", Namespace: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{"c1": runningState("gnmic-c1-0", at)},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(target).WithStatusSubresource(&gnmicv1alpha1.Target{}).Build()
	r := &TargetStateReconciler{Client: cl, Scheme: scheme}
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	r.applyClusterState(context.Background(), nn, "c1", runningState("gnmic-c1-2", at), logf.Log)

	var got gnmicv1alpha1.Target
	if err := cl.Get(context.Background(), nn, &got); err != nil {
		t.Fatal(err)
	}
	if pod := got.Status.ClusterStates["c1"].Pod; pod != "gnmic-c1-2" {
		t.Fatalf("pod = %q, want gnmic-c1-2", pod)
	}
	// The summary is derived, so it has to be recomputed on every real write.
	if got.Status.Clusters != 1 || got.Status.State != "READY" {
		t.Fatalf("summary not recomputed: clusters=%d state=%q", got.Status.Clusters, got.Status.State)
	}
}

// A target this operator never recorded must still get its first write.
func TestApplyClusterState_FirstWrite(t *testing.T) {
	scheme := statusScheme(t)
	target := &gnmicv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: "leaf1", Namespace: "default"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(target).WithStatusSubresource(&gnmicv1alpha1.Target{}).Build()
	r := &TargetStateReconciler{Client: cl, Scheme: scheme}
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	r.applyClusterState(context.Background(), nn, "c1", runningState("gnmic-c1-0", time.Unix(1000, 0)), logf.Log)

	var got gnmicv1alpha1.Target
	if err := cl.Get(context.Background(), nn, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Status.ClusterStates) != 1 {
		t.Fatalf("clusterStates = %v", got.Status.ClusterStates)
	}
}

func TestRemoveClusterState(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	scheme := statusScheme(t)
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "leaf1", Namespace: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			Clusters: 2,
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
				"c1": runningState("gnmic-c1-0", at),
				"c2": runningState("gnmic-c2-0", at),
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(target).WithStatusSubresource(&gnmicv1alpha1.Target{}).Build()
	r := &TargetStateReconciler{Client: cl, Scheme: scheme}
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	r.removeClusterState(context.Background(), nn, "c1", logf.Log)

	var got gnmicv1alpha1.Target
	if err := cl.Get(context.Background(), nn, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Status.ClusterStates["c1"]; ok {
		t.Fatal("c1 entry survived removal; a merge patch must null the key, not merge over it")
	}
	if _, ok := got.Status.ClusterStates["c2"]; !ok {
		t.Fatal("removing c1 took c2 with it")
	}
	if got.Status.Clusters != 1 {
		t.Fatalf("clusters = %d, want 1", got.Status.Clusters)
	}

	// Removing what is not there must not write.
	before := readVersion(t, cl, nn)
	r.removeClusterState(context.Background(), nn, "c1", logf.Log)
	if after := readVersion(t, cl, nn); after != before {
		t.Fatalf("removing an absent entry wrote: %s -> %s", before, after)
	}
}

func TestSwapReported(t *testing.T) {
	r := &TargetStateReconciler{}
	key := podStateKey("default", "c1", "gnmic-c1-0")

	if dropped := r.swapReported(key, set("a", "b")); len(dropped) != 0 {
		t.Fatalf("first poll reported drops: %v", dropped)
	}
	if dropped := r.swapReported(key, set("a", "b")); len(dropped) != 0 {
		t.Fatalf("unchanged poll reported drops: %v", dropped)
	}
	dropped := r.swapReported(key, set("a"))
	if len(dropped) != 1 || dropped[0] != "b" {
		t.Fatalf("dropped = %v, want [b]", dropped)
	}
	// b is already released; it must not be reported again.
	if again := r.swapReported(key, set("a")); len(again) != 0 {
		t.Fatalf("already-released target reported again: %v", again)
	}
}

func TestDueForSweepAndForget(t *testing.T) {
	r := &TargetStateReconciler{}
	key := podStateKey("default", "c1", "gnmic-c1-0")

	if !r.dueForSweep(key) {
		t.Fatal("first poll must sweep: the remembered set is empty and cannot be diffed")
	}
	if r.dueForSweep(key) {
		t.Fatal("swept twice in a row")
	}

	r.forgetPod(key)
	if !r.dueForSweep(key) {
		t.Fatal("a pod we lost sight of must sweep on its next poll")
	}
}

func set(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out
}

func readVersion(t *testing.T, cl client.Client, nn types.NamespacedName) string {
	t.Helper()
	var got gnmicv1alpha1.Target
	if err := cl.Get(context.Background(), nn, &got); err != nil {
		t.Fatal(err)
	}
	return got.ResourceVersion
}
