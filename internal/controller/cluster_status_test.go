package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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
