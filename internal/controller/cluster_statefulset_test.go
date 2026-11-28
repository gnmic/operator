package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// Editing the REST port and an existing environment variable must update the
// StatefulSet, not only the ConfigMap. The bootstrap file is a subPath mount,
// so the pod template annotation has to change as well or running pods keep
// the file they started with.
func TestStartupSettingsReachRunningStatefulSet(t *testing.T) {
	scheme := unresolvedScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewClusterReconcilerForTest()
	r.Client = cl
	r.Scheme = scheme

	cluster := readyCluster()
	cluster.Spec.API = &gnmicv1alpha1.APIConfig{RestPort: 7890}
	cluster.Spec.Env = []corev1.EnvVar{{Name: "COLLECTOR_MODE", Value: "old"}}

	ctx := context.Background()
	if _, err := r.reconcileStatefulSet(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	first := getCollectorStatefulSet(t, cl)
	if port := restContainerPort(t, first); port != 7890 {
		t.Fatalf("initial port = %d, want 7890", port)
	}
	if got := envValue(first, "COLLECTOR_MODE"); got != "old" {
		t.Fatalf("initial env = %q, want old", got)
	}
	firstHash := first.Spec.Template.Annotations[AnnotationBootstrapConfig]
	if firstHash == "" {
		t.Fatal("bootstrap annotation was not set")
	}

	cluster.Spec.API.RestPort = 7891
	cluster.Spec.Env[0].Value = "new"
	if _, err := r.reconcileStatefulSet(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	second := getCollectorStatefulSet(t, cl)
	if port := restContainerPort(t, second); port != 7891 {
		t.Fatalf("port = %d, want 7891", port)
	}
	if got := envValue(second, "COLLECTOR_MODE"); got != "new" {
		t.Fatalf("env = %q, want new", got)
	}
	secondHash := second.Spec.Template.Annotations[AnnotationBootstrapConfig]
	if secondHash == "" || secondHash == firstHash {
		t.Fatalf("bootstrap annotation = %q, want a new checksum", secondHash)
	}

	var cm corev1.ConfigMap
	if err := cl.Get(ctx, types.NamespacedName{Name: "gnmic-c1-config", Namespace: "default"}, &cm); err != nil {
		t.Fatal(err)
	}
	config := cm.Data[gNMIcConfigFile]
	if !strings.Contains(config, ":7891") || strings.Contains(config, ":7890") {
		t.Fatalf("configmap = %s", config)
	}

	// The same desired config must not roll the StatefulSet again.
	rv := second.ResourceVersion
	if _, err := r.reconcileStatefulSet(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	third := getCollectorStatefulSet(t, cl)
	if third.ResourceVersion != rv {
		t.Fatalf("unchanged spec updated the statefulset: %s -> %s", rv, third.ResourceVersion)
	}
	if third.Spec.Template.Annotations[AnnotationBootstrapConfig] != secondHash {
		t.Fatal("bootstrap annotation changed without a config change")
	}
}

func getCollectorStatefulSet(t *testing.T, cl client.Client) *appsv1.StatefulSet {
	t.Helper()
	var sts appsv1.StatefulSet
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "gnmic-c1", Namespace: "default"}, &sts); err != nil {
		t.Fatal(err)
	}
	return &sts
}

func restContainerPort(t *testing.T, sts *appsv1.StatefulSet) int32 {
	t.Helper()
	for _, p := range sts.Spec.Template.Spec.Containers[0].Ports {
		if p.Name == "rest" {
			return p.ContainerPort
		}
	}
	t.Fatal("rest port not set on the collector container")
	return 0
}

func envValue(sts *appsv1.StatefulSet, name string) string {
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}
