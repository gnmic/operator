package controller

import (
	"context"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func secretWatchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func reconcilerWith(t *testing.T, objs ...client.Object) *ClusterReconciler {
	t.Helper()
	scheme := secretWatchScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &ClusterReconciler{Client: cl, Scheme: scheme}
}

func clusterNames(t *testing.T, r *ClusterReconciler, obj client.Object) []string {
	t.Helper()
	reqs := r.findClustersForSecret(context.Background(), obj)
	out := make([]string, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, req.Name)
	}
	sort.Strings(out)
	return out
}

func secret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
}

func profile(name, credsRef string) *gnmicv1alpha1.TargetProfile {
	return &gnmicv1alpha1.TargetProfile{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       gnmicv1alpha1.TargetProfileSpec{CredentialsRef: credsRef},
	}
}

func target(name, profileName string, labels map[string]string) *gnmicv1alpha1.Target {
	return &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
		Spec:       gnmicv1alpha1.TargetSpec{Profile: profileName, Address: "10.0.0.1:57400"},
	}
}

func pipelineSelectingTargets(name, clusterRef string, enabled bool, labels map[string]string) *gnmicv1alpha1.Pipeline {
	return &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef:      clusterRef,
			Enabled:         enabled,
			TargetSelectors: []metav1.LabelSelector{{MatchLabels: labels}},
		},
	}
}

// A rotated Secret must reach the cluster whose pipeline collects with it. This
// is the whole point of the watch: without it the new credentials sit in the
// API until an unrelated event happens to trigger a reconcile.
func TestFindClustersForSecret_ReachesCollectingCluster(t *testing.T) {
	r := reconcilerWith(t,
		secret("creds"),
		profile("default", "creds"),
		target("leaf1", "default", map[string]string{"tag": "prod"}),
		pipelineSelectingTargets("p1", "c1", true, map[string]string{"tag": "prod"}),
	)
	if got := clusterNames(t, r, secret("creds")); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("clusters = %v, want [c1]", got)
	}
}

// Most Secrets in a namespace have nothing to do with the operator. They must
// cost one cached list and no reconcile.
func TestFindClustersForSecret_UnreferencedSecretEnqueuesNothing(t *testing.T) {
	r := reconcilerWith(t,
		secret("creds"),
		secret("unrelated"),
		profile("default", "creds"),
		target("leaf1", "default", map[string]string{"tag": "prod"}),
		pipelineSelectingTargets("p1", "c1", true, map[string]string{"tag": "prod"}),
	)
	if got := clusterNames(t, r, secret("unrelated")); len(got) != 0 {
		t.Fatalf("clusters = %v, want none", got)
	}
}

// Tunnel targets are discovered at runtime, so no Target object names the
// profile. Resolving only through Targets misses them entirely.
func TestFindClustersForSecret_ReachesTunnelTargetPolicy(t *testing.T) {
	policy := &gnmicv1alpha1.TunnelTargetPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "tp1", Namespace: "default", Labels: map[string]string{"tag": "tunnel"}},
		Spec:       gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "default"},
	}
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef:                  "c1",
			Enabled:                     true,
			TunnelTargetPolicySelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"tag": "tunnel"}}},
		},
	}
	r := reconcilerWith(t, secret("creds"), profile("default", "creds"), policy, pipeline)
	if got := clusterNames(t, r, secret("creds")); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("clusters = %v, want [c1]", got)
	}
}

// A disabled pipeline is not collecting, so nothing needs re-pushing.
func TestFindClustersForSecret_SkipsDisabledPipeline(t *testing.T) {
	r := reconcilerWith(t,
		secret("creds"),
		profile("default", "creds"),
		target("leaf1", "default", map[string]string{"tag": "prod"}),
		pipelineSelectingTargets("p1", "c1", false, map[string]string{"tag": "prod"}),
	)
	if got := clusterNames(t, r, secret("creds")); len(got) != 0 {
		t.Fatalf("clusters = %v, want none", got)
	}
}

// One Secret can back several profiles, and several clusters can collect with
// them. Each cluster must be enqueued once.
func TestFindClustersForSecret_DeduplicatesClusters(t *testing.T) {
	r := reconcilerWith(t,
		secret("creds"),
		profile("a", "creds"),
		profile("b", "creds"),
		target("leaf1", "a", map[string]string{"tag": "prod"}),
		target("leaf2", "b", map[string]string{"tag": "prod"}),
		pipelineSelectingTargets("p1", "c1", true, map[string]string{"tag": "prod"}),
		pipelineSelectingTargets("p2", "c1", true, map[string]string{"tag": "prod"}),
		pipelineSelectingTargets("p3", "c2", true, map[string]string{"tag": "prod"}),
	)
	got := clusterNames(t, r, secret("creds"))
	if len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("clusters = %v, want [c1 c2]", got)
	}
}

// Secrets carry no generation, so the predicate is the only thing standing
// between the controller and a reconcile per unrelated Secret write.
func TestSecretDataChangedPredicate(t *testing.T) {
	p := secretDataChangedPredicate{}

	old := secret("creds")
	unchanged := secret("creds")
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: unchanged}) {
		t.Error("identical data triggered a reconcile")
	}

	relabelled := secret("creds")
	relabelled.Labels = map[string]string{"touched": "true"}
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: relabelled}) {
		t.Error("metadata-only change triggered a reconcile")
	}

	rotated := secret("creds")
	rotated.Data["password"] = []byte("new")
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: rotated}) {
		t.Error("rotated password did not trigger a reconcile")
	}

	removed := secret("creds")
	delete(removed.Data, "password")
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: removed}) {
		t.Error("removed key did not trigger a reconcile")
	}

	if p.Update(event.UpdateEvent{ObjectOld: &corev1.ConfigMap{}, ObjectNew: &corev1.ConfigMap{}}) {
		t.Error("non-Secret objects should not pass the predicate")
	}
}
