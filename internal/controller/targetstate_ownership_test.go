package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func targetOwnedBy(pod string) *gnmicv1alpha1.Target {
	return &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "leaf1", Namespace: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			Clusters: 1,
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
				"c1": {Pod: pod, State: "running", ConnectionState: "READY"},
			},
		},
	}
}

func targetStateReconcilerWith(t *testing.T, objs ...client.Object) (*TargetStateReconciler, client.Client) {
	t.Helper()
	scheme := secretWatchScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(objs...).WithStatusSubresource(&gnmicv1alpha1.Target{}).Build()
	return &TargetStateReconciler{Client: cl, Scheme: scheme}, cl
}

// A target moving from pod 0 to pod 1 is reported by pod 1 immediately, but pod 0 only
// notices it is gone on its next poll — up to a poll interval later. Releasing
// unconditionally then deleted the entry pod 1 had already written.
func TestRemoveClusterStateLeavesTheNewOwnerAlone(t *testing.T) {
	r, cl := targetStateReconcilerWith(t, targetOwnedBy("gnmic-c1-1"))
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	// pod 0 releases a target it no longer holds
	r.removeClusterState(context.Background(), nn, "c1", "gnmic-c1-0", logf.Log)

	var got gnmicv1alpha1.Target
	if err := cl.Get(context.Background(), nn, &got); err != nil {
		t.Fatal(err)
	}
	state, ok := got.Status.ClusterStates["c1"]
	if !ok {
		t.Fatal("pod 0's release deleted the entry pod 1 owns")
	}
	if state.Pod != "gnmic-c1-1" {
		t.Fatalf("owner = %q, want gnmic-c1-1", state.Pod)
	}
	if got.Status.Clusters != 1 {
		t.Errorf("clusters = %d, want 1", got.Status.Clusters)
	}
}

// The owning pod still releases its own entry.
func TestRemoveClusterStateReleasesItsOwnEntry(t *testing.T) {
	r, cl := targetStateReconcilerWith(t, targetOwnedBy("gnmic-c1-0"))
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	r.removeClusterState(context.Background(), nn, "c1", "gnmic-c1-0", logf.Log)

	var got gnmicv1alpha1.Target
	_ = cl.Get(context.Background(), nn, &got)
	if _, ok := got.Status.ClusterStates["c1"]; ok {
		t.Fatal("the owning pod's release was ignored")
	}
}

// An empty pod name means "whoever holds it", for cluster-wide cleanup.
func TestRemoveClusterStateEmptyPodReleasesAnyOwner(t *testing.T) {
	r, cl := targetStateReconcilerWith(t, targetOwnedBy("gnmic-c1-7"))
	nn := types.NamespacedName{Name: "leaf1", Namespace: "default"}

	r.removeClusterState(context.Background(), nn, "c1", "", logf.Log)

	var got gnmicv1alpha1.Target
	_ = cl.Get(context.Background(), nn, &got)
	if _, ok := got.Status.ClusterStates["c1"]; ok {
		t.Fatal("an unconditional release did nothing")
	}
}
