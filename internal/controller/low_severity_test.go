package controller

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// #22: exhausting the conflict retries used to fall out of the loop and return
// nil, so the caller believed a status it never wrote.
func TestUpdatePipelineStatusFailsAfterRepeatedConflicts(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{ClusterRef: "c1", Enabled: true},
	}
	attempts := 0
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pipeline).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ ...client.SubResourceUpdateOption) error {
				attempts++
				return apierrors.NewConflict(
					schema.GroupResource{Group: "operator.gnmic.dev", Resource: "pipelines"},
					obj.GetName(), errors.New("stale"))
			},
		}).
		Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	data := gnmic.NewPipelineData()
	data.Targets["default/t1"] = gnmicv1alpha1.Target{}
	data.Subscriptions["default/p1/s1"] = gnmicv1alpha1.SubscriptionSpec{}
	data.Outputs["default/p1/o1"] = gnmicv1alpha1.OutputSpec{}

	err := r.updatePipelineStatus(context.Background(), pipeline, data, nil)
	if err == nil {
		t.Fatal("five conflicts in a row reported success")
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5", attempts)
	}
}

// #35: the Service name is three user-chosen names joined; Kubernetes caps it
// at 63 characters and a too-long Create fails without pointing at the length.
func TestPrometheusServiceNameFitsTheServiceLimit(t *testing.T) {
	label := regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)

	short := PrometheusServiceName("c1", "p1", "o1")
	if short != "gnmic-c1-prom-p1-o1" {
		t.Fatalf("a name that fits must be unchanged, got %q", short)
	}

	long := PrometheusServiceName("telemetry-collectors-production-eu-west", "core-routers-interfaces-counters", "prometheus-remote-write-main")
	if len(long) > maxServiceNameLen {
		t.Fatalf("len(%q) = %d, over %d", long, len(long), maxServiceNameLen)
	}
	if !label.MatchString(long) {
		t.Fatalf("%q is not a DNS-1035 label", long)
	}
	if again := PrometheusServiceName("telemetry-collectors-production-eu-west", "core-routers-interfaces-counters", "prometheus-remote-write-main"); again != long {
		t.Fatalf("not stable: %q then %q", long, again)
	}
	other := PrometheusServiceName("telemetry-collectors-production-eu-west", "core-routers-interfaces-counters", "prometheus-remote-write-other")
	if other == long {
		t.Fatalf("two outputs that only differ past the cut collapsed onto %q", long)
	}

	// A cut that lands on a separator must not leave a trailing "-" before the hash.
	edge := PrometheusServiceName("abcdefghijklmnopqrstuvwxyz0123456789abcdefghij", "a", "b")
	if !label.MatchString(edge) || len(edge) > maxServiceNameLen {
		t.Fatalf("%q (len %d) is not a valid label", edge, len(edge))
	}
}

// #37: a Pipeline with an empty clusterRef enqueued a request for the bare
// "gnmic-" prefix, driving the not-found cleanup path for a Cluster that never
// existed.
func TestEmptyClusterRefIsNotEnqueued(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	bound := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "bound", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{ClusterRef: "c1", Enabled: true, TargetRefs: []string{"t1"}},
	}
	unbound := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "unbound", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{Enabled: true, TargetRefs: []string{"t1"}},
	}
	profile := &gnmicv1alpha1.TargetProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "prof", Namespace: "default"},
		Spec:       gnmicv1alpha1.TargetProfileSpec{CredentialsRef: "creds"},
	}
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "1.1.1.1", Profile: "prof"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bound, unbound, profile, target).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	// findClustersReferencingResource
	reqs := r.findClustersReferencingResource(context.Background(), "default", "t1", nil, "target")
	if len(reqs) != 1 || reqs[0].Name != "c1" {
		t.Fatalf("direct reference: requests = %v, want only c1", reqs)
	}
	// findClustersUsingProfiles, via the Secret mapping
	reqs = r.findClustersForSecret(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"},
	})
	if len(reqs) != 1 || reqs[0].Name != "c1" {
		t.Fatalf("profile path: requests = %v, want only c1", reqs)
	}
}

// #26: streams stopped for a whole cluster return through ctx.Done(), never
// through the disconnect path that forgets a pod, so the per-pod report state
// stayed for the life of the process.
func TestStopStreamsForClusterForgetsPodState(t *testing.T) {
	r := &TargetStateReconciler{
		streams: map[string]context.CancelFunc{},
		reported: map[string]map[string]struct{}{
			podStateKey("ns", "gone", "gnmic-gone-0"): {"ns/t1": {}},
			podStateKey("ns", "gone", "gnmic-gone-1"): {"ns/t2": {}},
			podStateKey("ns", "kept", "gnmic-kept-0"): {"ns/t3": {}},
		},
		lastSweep: map[string]time.Time{
			podStateKey("ns", "gone", "gnmic-gone-0"): time.Now(),
			podStateKey("ns", "kept", "gnmic-kept-0"): time.Now(),
		},
	}
	cancelled := false
	r.streams[streamKey("ns", "gone", 0)] = func() { cancelled = true }

	r.stopStreamsForCluster("ns", "gone")

	if !cancelled {
		t.Fatal("stream not cancelled")
	}
	if len(r.reported) != 1 || len(r.lastSweep) != 1 {
		t.Fatalf("cluster state not forgotten: reported=%v lastSweep=%v", r.reported, r.lastSweep)
	}
	if _, ok := r.reported[podStateKey("ns", "kept", "gnmic-kept-0")]; !ok {
		t.Fatal("another cluster's state was dropped")
	}
}

// #31: every reader of spec.replicas agrees on what nil means.
func TestDesiredReplicas(t *testing.T) {
	if got := desiredReplicas(&gnmicv1alpha1.Cluster{}); got != 1 {
		t.Fatalf("nil replicas = %d, want the CRD default 1", got)
	}
	if got := desiredReplicas(&gnmicv1alpha1.Cluster{Spec: gnmicv1alpha1.ClusterSpec{Replicas: ptr.To[int32](3)}}); got != 3 {
		t.Fatalf("replicas = %d, want 3", got)
	}
}
