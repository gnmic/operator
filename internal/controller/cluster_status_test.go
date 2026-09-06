package controller

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func TestBuildClusterStatusPartiallyReadyMessage(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default", Generation: 4},
		Spec:       gnmicv1alpha1.ClusterSpec{Replicas: ptr.To(int32(3))},
	}
	sts := &appsv1.StatefulSet{
		Spec:   appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 1},
	}

	status := buildClusterStatus(cluster, sts, 0, nil, applyOutcome{applied: true, numPods: 3})

	ready := findCondition(status.Conditions, ConditionTypeReady)
	if ready == nil {
		t.Fatal("Ready condition missing")
	}
	if ready.Status != metav1.ConditionTrue || ready.Reason != "ClusterPartiallyReady" {
		t.Fatalf("unexpected Ready condition: %+v", *ready)
	}
	if want := "1 of 3 replicas are ready and configured"; ready.Message != want {
		t.Fatalf("message = %q, want %q", ready.Message, want)
	}
	if ready.ObservedGeneration != 4 {
		t.Fatalf("ObservedGeneration = %d, want 4", ready.ObservedGeneration)
	}
}

func TestWriteClusterStatusPreservesTransitionTimeFromLiveObject(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = gnmicv1alpha1.AddToScheme(scheme)

	// metav1.Time survives the fake client at second precision only
	transitioned := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	live := &gnmicv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default"},
		Status: gnmicv1alpha1.ClusterStatus{
			Conditions: []metav1.Condition{{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionTrue,
				Reason:             "ClusterReady",
				LastTransitionTime: transitioned,
			}},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(live).
		WithStatusSubresource(&gnmicv1alpha1.Cluster{}).
		Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	// The copy this reconcile started from predates the live transition to Ready.
	// Merging against it would stamp a new time over a transition that already happened.
	stale := live.DeepCopy()
	stale.Status.Conditions[0].Status = metav1.ConditionFalse
	stale.Status.Conditions[0].LastTransitionTime = metav1.NewTime(transitioned.Add(-time.Hour))

	now := metav1.Now()
	next := gnmicv1alpha1.ClusterStatus{
		PipelinesCount: 1, // differs from live so the write is not skipped
		Conditions: []metav1.Condition{{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             "ClusterReady",
			LastTransitionTime: now,
		}},
	}
	if err := r.writeClusterStatus(context.Background(), stale, next); err != nil {
		t.Fatal(err)
	}

	var got gnmicv1alpha1.Cluster
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "c1"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.PipelinesCount != 1 {
		t.Fatalf("status was not written: %+v", got.Status)
	}
	ready := findCondition(got.Status.Conditions, ConditionTypeReady)
	if ready == nil {
		t.Fatal("Ready condition missing")
	}
	if !ready.LastTransitionTime.Time.Equal(transitioned.Time) {
		t.Fatalf("LastTransitionTime = %v, want the live transition %v", ready.LastTransitionTime.Time, transitioned.Time)
	}
}

func TestPreserveTransitionTimesOnlyForUnchangedStatus(t *testing.T) {
	old := metav1.NewTime(time.Now().Add(-time.Hour))
	now := metav1.Now()
	prior := []metav1.Condition{
		{Type: ConditionTypeReady, Status: metav1.ConditionTrue, LastTransitionTime: old},
		{Type: ConditionTypeConfigApplied, Status: metav1.ConditionFalse, LastTransitionTime: old},
	}
	conds := []metav1.Condition{
		{Type: ConditionTypeReady, Status: metav1.ConditionTrue, LastTransitionTime: now},              // unchanged: keep old
		{Type: ConditionTypeConfigApplied, Status: metav1.ConditionTrue, LastTransitionTime: now},      // flipped: keep now
		{Type: ConditionTypeCapacityExhausted, Status: metav1.ConditionFalse, LastTransitionTime: now}, // new: keep now
	}
	preserveTransitionTimes(prior, conds)
	if !conds[0].LastTransitionTime.Equal(&old) {
		t.Errorf("unchanged condition: got %v, want %v", conds[0].LastTransitionTime, old)
	}
	if !conds[1].LastTransitionTime.Equal(&now) {
		t.Errorf("flipped condition: got %v, want %v", conds[1].LastTransitionTime, now)
	}
	if !conds[2].LastTransitionTime.Equal(&now) {
		t.Errorf("new condition: got %v, want %v", conds[2].LastTransitionTime, now)
	}
}
