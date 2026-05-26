package controller

import (
	"context"
	"testing"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestClusterStatusEqual(t *testing.T) {
	a := gnmicv1alpha1.ClusterStatus{ReadyReplicas: 1, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}}
	b := a
	if !clusterStatusEqual(a, b) {
		t.Fatal("expected equal status")
	}
	b.ReadyReplicas = 2
	if clusterStatusEqual(a, b) {
		t.Fatal("expected different status")
	}
}

func TestPipelineReferencesResource(t *testing.T) {
	pipeline := &gnmicv1alpha1.Pipeline{
		Spec: gnmicv1alpha1.PipelineSpec{
			TargetRefs: []string{"my-target"},
			TargetSelectors: []metav1.LabelSelector{{
				MatchLabels: map[string]string{"app": "router"},
			}},
		},
	}
	if !pipelineReferencesResource(pipeline, "my-target", nil, "target") {
		t.Fatal("expected ref match")
	}
	if !pipelineReferencesResource(pipeline, "other", map[string]string{"app": "router"}, "target") {
		t.Fatal("expected selector match")
	}
	if pipelineReferencesResource(pipeline, "x", nil, "unknown") {
		t.Fatal("expected false for unknown type")
	}
}

func TestParseListenPortAndPath(t *testing.T) {
	port, path, err := parseListenPortAndPath([]byte(`listen: ":9805"`))
	if err != nil || port != 9805 || path != "/metrics" {
		t.Fatalf("port=%d path=%q err=%v", port, path, err)
	}

	port, path, err = parseListenPortAndPath([]byte("listen: \"127.0.0.1:8080\"\npath: /custom\n"))
	if err != nil || port != 8080 || path != "/custom" {
		t.Fatalf("port=%d path=%q err=%v", port, path, err)
	}

	if _, _, err := parseListenPortAndPath([]byte("not yaml")); err == nil {
		t.Fatal("expected yaml error")
	}
}

func TestComputeStatusSummary(t *testing.T) {
	status := &gnmicv1alpha1.TargetStatus{
		ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
			"c1": {State: "running", ConnectionState: "READY"},
		},
	}
	computeStatusSummary(status)
	if status.State != "READY" || status.Clusters != 1 {
		t.Fatalf("status = %+v", status)
	}

	status.ClusterStates["c2"] = gnmicv1alpha1.ClusterTargetState{State: "failed"}
	computeStatusSummary(status)
	if status.State != "DEGRADED" {
		t.Fatalf("expected DEGRADED, got %q", status.State)
	}
}

func TestParseTargetNameAndStreamKey(t *testing.T) {
	ns, name, ok := parseTargetName("default/router1")
	if !ok || ns != "default" || name != "router1" {
		t.Fatalf("parseTargetName = %q %q %v", ns, name, ok)
	}
	if key := streamKey("default", "cluster", 2); key != "default/cluster/2" {
		t.Fatalf("streamKey = %q", key)
	}
}

func TestBackoff(t *testing.T) {
	if got := backoff(time.Second); got != 2*time.Second {
		t.Fatalf("backoff = %v", got)
	}
	if got := backoff(reconnectMaxDelay); got != reconnectMaxDelay {
		t.Fatalf("backoff cap = %v", got)
	}
}

func TestBuildContainerPorts(t *testing.T) {
	cluster := &gnmicv1alpha1.Cluster{
		Spec: gnmicv1alpha1.ClusterSpec{
			API: &gnmicv1alpha1.APIConfig{
				RestPort: 8080,
				GNMIPort: 9339,
			},
			GRPCTunnel: &gnmicv1alpha1.GRPCTunnelConfig{Port: 57400},
		},
	}
	ports := buildContainerPorts(cluster)
	if len(ports) != 3 {
		t.Fatalf("ports = %d", len(ports))
	}
}

func TestExtractOrdinalFromCertName(t *testing.T) {
	r := &ClusterReconciler{}
	if got := r.extractOrdinalFromCertName("gnmic-cluster-2-tls", "gnmic-cluster"); got != 2 {
		t.Fatalf("ordinal = %d", got)
	}
	if got := r.extractOrdinalFromCertName("invalid", "gnmic-cluster"); got != -1 {
		t.Fatalf("expected -1, got %d", got)
	}
}

func TestPipelineStatusEqual(t *testing.T) {
	a := gnmicv1alpha1.PipelineStatus{Status: "Ready", TargetsCount: 1}
	if !pipelineStatusEqual(a, a) {
		t.Fatal("expected equal")
	}
	b := a
	b.TargetsCount = 2
	if pipelineStatusEqual(a, b) {
		t.Fatal("expected different")
	}
}

func TestResourcesEqual(t *testing.T) {
	req := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
	}
	if !resourcesEqual(req, req) {
		t.Fatal("expected equal resources")
	}
}

func TestGenerationOrLabelsChangedPredicate(t *testing.T) {
	p := generationOrLabelsChangedPredicate{}
	old := &gnmicv1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"a": "1"}}}
	new := old.DeepCopy()
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: new}) {
		t.Fatal("expected false when unchanged")
	}
	new.Generation = 2
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: new}) {
		t.Fatal("expected true on generation change")
	}
}

func TestPipelineReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	cluster := &gnmicv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default"},
	}
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef: "c1",
			Enabled:    true,
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, pipeline).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}).
		Build()

	r := &PipelineReconciler{Client: cl, Scheme: scheme}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != 0 {
		t.Fatalf("unexpected requeue: %+v", res)
	}

	var updated gnmicv1alpha1.Pipeline
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(pipeline), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Status != "Ready" {
		t.Fatalf("status = %q", updated.Status.Status)
	}

	// missing cluster
	pipeline2 := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{ClusterRef: "missing"},
	}
	cl2 := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pipeline2).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}).
		Build()
	r2 := &PipelineReconciler{Client: cl2, Scheme: scheme}
	res, err = r2.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "p2"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != 30*time.Second {
		t.Fatalf("requeue = %v", res.RequeueAfter)
	}

	// disabled pipeline
	pipeline3 := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p3", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{ClusterRef: "c1", Enabled: false},
	}
	cl3 := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, pipeline3).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}).
		Build()
	r3 := &PipelineReconciler{Client: cl3, Scheme: scheme}
	if _, err := r3.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "p3"}}); err != nil {
		t.Fatal(err)
	}
	var updated3 gnmicv1alpha1.Pipeline
	_ = cl3.Get(context.Background(), client.ObjectKeyFromObject(pipeline3), &updated3)
	if updated3.Status.Status != "Disabled" {
		t.Fatalf("status = %q", updated3.Status.Status)
	}
}

func TestTunnelTargetPolicyReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = gnmicv1alpha1.AddToScheme(scheme)
	r := &TunnelTargetPolicyReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
		t.Fatal(err)
	}
}

func TestGetClusterPlan(t *testing.T) {
	r := NewClusterReconcilerForTest()
	plan := &gnmic.ApplyPlan{}
	r.CachePlan("ns", "c", plan)

	got, err := r.GetClusterPlan("ns", "c")
	if err != nil || got != plan {
		t.Fatalf("GetClusterPlan: plan=%p err=%v", got, err)
	}
	if _, err := r.GetClusterPlan("ns", "missing"); err == nil {
		t.Fatal("expected not found")
	}
	r.cleanupPlan("ns", "c")
	if _, err := r.GetClusterPlan("ns", "c"); err == nil {
		t.Fatal("expected plan removed")
	}
}
