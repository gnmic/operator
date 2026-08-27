package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// A ref naming a resource that does not exist must be reported, not dropped. Each
// resolver used to silently shorten its result list instead, so a typo'd ref produced
// a pipeline that collected slightly less than it was asked to, with no signal at all.
func TestResolversReportUnresolvedRefs(t *testing.T) {
	tests := []struct {
		name      string
		spec      gnmicv1alpha1.PipelineSpec
		existing  client.Object
		resolve   func(*ClusterReconciler, *gnmicv1alpha1.Pipeline) (int, []string, error)
		wantCount int
		wantUnres []string
	}{
		{
			name:     "target",
			spec:     gnmicv1alpha1.PipelineSpec{TargetRefs: []string{"here", "gone"}},
			existing: &gnmicv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveTargets(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 1,
			wantUnres: []string{"target/gone"},
		},
		{
			name:     "subscription",
			spec:     gnmicv1alpha1.PipelineSpec{SubscriptionRefs: []string{"gone"}},
			existing: &gnmicv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveSubscriptions(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 0,
			wantUnres: []string{"subscription/gone"},
		},
		{
			name: "output",
			spec: gnmicv1alpha1.PipelineSpec{
				Outputs: gnmicv1alpha1.OutputSelector{OutputRefs: []string{"gone"}},
			},
			existing: &gnmicv1alpha1.Output{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveOutputs(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 0,
			wantUnres: []string{"output/gone"},
		},
		{
			name: "input",
			spec: gnmicv1alpha1.PipelineSpec{
				Inputs: gnmicv1alpha1.InputSelector{InputRefs: []string{"gone"}},
			},
			existing: &gnmicv1alpha1.Input{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveInputs(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 0,
			wantUnres: []string{"input/gone"},
		},
		{
			name:     "tunnel target policy",
			spec:     gnmicv1alpha1.PipelineSpec{TunnelTargetPolicyRefs: []string{"gone"}},
			existing: &gnmicv1alpha1.TunnelTargetPolicy{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveTunnelTargetPolicies(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 0,
			wantUnres: []string{"tunneltargetpolicy/gone"},
		},
		{
			name: "output processor",
			spec: gnmicv1alpha1.PipelineSpec{
				Outputs: gnmicv1alpha1.OutputSelector{ProcessorRefs: []string{"gone"}},
			},
			existing: &gnmicv1alpha1.Processor{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveOutputProcessors(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 0,
			wantUnres: []string{"processor/gone"},
		},
		{
			name: "input processor",
			spec: gnmicv1alpha1.PipelineSpec{
				Inputs: gnmicv1alpha1.InputSelector{ProcessorRefs: []string{"gone"}},
			},
			existing: &gnmicv1alpha1.Processor{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}},
			resolve: func(r *ClusterReconciler, p *gnmicv1alpha1.Pipeline) (int, []string, error) {
				got, u, err := r.resolveInputProcessors(context.Background(), p)
				return len(got), u, err
			},
			wantCount: 0,
			wantUnres: []string{"processor/gone"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := reconcilerWith(t, tt.existing)
			pipeline := &gnmicv1alpha1.Pipeline{
				ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
				Spec:       tt.spec,
			}
			count, unresolved, err := tt.resolve(r, pipeline)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if count != tt.wantCount {
				t.Errorf("resolved %d resources, want %d", count, tt.wantCount)
			}
			if strings.Join(unresolved, ",") != strings.Join(tt.wantUnres, ",") {
				t.Errorf("unresolved = %v, want %v", unresolved, tt.wantUnres)
			}
		})
	}
}

// A selector is a query, and an empty answer is a legitimate answer. Only refs are a
// stated intent that can dangle, so a selector matching nothing must stay silent.
func TestSelectorMatchingNothingIsNotUnresolved(t *testing.T) {
	r := reconcilerWith(t)
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			TargetSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"app": "nothing"}}},
			Outputs: gnmicv1alpha1.OutputSelector{
				OutputSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"app": "nothing"}}},
			},
		},
	}
	if _, unresolved, err := r.resolveTargets(context.Background(), pipeline); err != nil || len(unresolved) != 0 {
		t.Fatalf("targets: unresolved=%v err=%v", unresolved, err)
	}
	if _, unresolved, err := r.resolveOutputs(context.Background(), pipeline); err != nil || len(unresolved) != 0 {
		t.Fatalf("outputs: unresolved=%v err=%v", unresolved, err)
	}
}

// A missing processor ref used to fall through and append the zero-valued Processor,
// which reached the plan as a processor with an empty name and an empty type and was
// then attached to every output in the pipeline. The refs that do resolve must still
// come back, in order, and the empty one must not be among them.
func TestMissingProcessorRefIsNotAppendedAsEmpty(t *testing.T) {
	r := reconcilerWith(t,
		&gnmicv1alpha1.Processor{ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "default"}},
		&gnmicv1alpha1.Processor{ObjectMeta: metav1.ObjectMeta{Name: "third", Namespace: "default"}},
	)
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			Outputs: gnmicv1alpha1.OutputSelector{
				ProcessorRefs: []string{"first", "second", "third"},
			},
		},
	}

	procs, unresolved, err := r.resolveOutputProcessors(context.Background(), pipeline)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 2 {
		t.Fatalf("resolved %d processors, want 2", len(procs))
	}
	for i, p := range procs {
		if p.Name == "" {
			t.Fatalf("processor %d has an empty name; the zero value leaked into the result", i)
		}
	}
	if procs[0].Name != "first" || procs[1].Name != "third" {
		t.Fatalf("ref order not preserved: %q, %q", procs[0].Name, procs[1].Name)
	}
	if strings.Join(unresolved, ",") != "processor/second" {
		t.Fatalf("unresolved = %v", unresolved)
	}
}

// The ResourcesResolved condition existed before this change but was hardcoded True,
// so a pipeline whose refs had evaporated still reported that everything resolved.
func TestUpdatePipelineStatusReportsUnresolved(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{ClusterRef: "c1", Enabled: true},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pipeline).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}).
		Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	data := gnmic.NewPipelineData()
	data.Targets["default/t1"] = gnmicv1alpha1.Target{}
	data.Subscriptions["default/p1/s1"] = gnmicv1alpha1.SubscriptionSpec{}

	if err := r.updatePipelineStatus(context.Background(), pipeline, data,
		[]string{"output/gone", "processor/also-gone"}); err != nil {
		t.Fatal(err)
	}

	var got gnmicv1alpha1.Pipeline
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(pipeline), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Status != "Error" {
		t.Errorf("status = %q, want Error", got.Status.Status)
	}
	// The counts stay populated so partial resolution remains visible while diagnosing.
	if got.Status.TargetsCount != 1 || got.Status.SubscriptionsCount != 1 {
		t.Errorf("counts dropped: targets=%d subs=%d", got.Status.TargetsCount, got.Status.SubscriptionsCount)
	}

	resolved := findCondition(got.Status.Conditions, PipelineConditionTypeResourcesResolved)
	if resolved == nil {
		t.Fatal("ResourcesResolved condition missing")
	}
	if resolved.Status != metav1.ConditionFalse {
		t.Errorf("ResourcesResolved = %v, want False", resolved.Status)
	}
	if resolved.Reason != ReasonUnresolvedReferences {
		t.Errorf("reason = %q", resolved.Reason)
	}
	if !strings.Contains(resolved.Message, "output/gone") || !strings.Contains(resolved.Message, "processor/also-gone") {
		t.Errorf("message does not name both refs: %q", resolved.Message)
	}

	ready := findCondition(got.Status.Conditions, PipelineConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != ReasonUnresolvedReferences {
		t.Errorf("ready condition = %+v", ready)
	}
}

func TestUpdatePipelineStatusResolvedStaysTrue(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = gnmicv1alpha1.AddToScheme(scheme)
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       gnmicv1alpha1.PipelineSpec{ClusterRef: "c1", Enabled: true},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pipeline).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}).
		Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	data := gnmic.NewPipelineData()
	data.Targets["default/t1"] = gnmicv1alpha1.Target{}
	data.Subscriptions["default/p1/s1"] = gnmicv1alpha1.SubscriptionSpec{}
	data.Outputs["default/p1/o1"] = gnmicv1alpha1.OutputSpec{}

	if err := r.updatePipelineStatus(context.Background(), pipeline, data, nil); err != nil {
		t.Fatal(err)
	}

	var got gnmicv1alpha1.Pipeline
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(pipeline), &got)
	if got.Status.Status != "Active" {
		t.Errorf("status = %q, want Active", got.Status.Status)
	}
	resolved := findCondition(got.Status.Conditions, PipelineConditionTypeResourcesResolved)
	if resolved == nil || resolved.Status != metav1.ConditionTrue {
		t.Errorf("ResourcesResolved = %+v, want True", resolved)
	}
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------- reconcile level

func unresolvedScheme(t *testing.T) *runtime.Scheme {
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

func readyCluster() *gnmicv1alpha1.Cluster {
	return &gnmicv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "c1",
			Namespace:  "default",
			Finalizers: []string{clusterFinalizer},
		},
		Spec: gnmicv1alpha1.ClusterSpec{
			Image:    "ghcr.io/gnmic/gnmic:test",
			Replicas: ptr.To(int32(1)),
		},
	}
}

// statefulSet returns a StatefulSet standing in for one the reconciler would create,
// with readyReplicas set so the reconcile gets past the readiness gate.
func statefulSet(readyReplicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "gnmic-c1", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    ptr.To(int32(1)),
			ServiceName: "gnmic-c1",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{LabelClusterName: "c1"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{LabelClusterName: "c1"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "gnmic", Image: "ghcr.io/gnmic/gnmic:test"}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: readyReplicas},
	}
}

func reconcileCluster(t *testing.T, objs ...client.Object) (*ClusterReconciler, client.Client, ctrl.Result) {
	t.Helper()
	scheme := unresolvedScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&gnmicv1alpha1.Pipeline{}, &gnmicv1alpha1.Cluster{}).
		Build()

	r := NewClusterReconcilerForTest()
	r.Client = cl
	r.Scheme = scheme

	res, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "c1"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return r, cl, res
}

// The whole point of the change: a pipeline with a dangling ref is left out of the
// plan rather than applied without the resource it named.
func TestReconcileSkipsPipelineWithUnresolvedRef(t *testing.T) {
	pipeline := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef:       "c1",
			Enabled:          true,
			TargetRefs:       []string{"t1"},
			SubscriptionRefs: []string{"s1"},
			Outputs:          gnmicv1alpha1.OutputSelector{OutputRefs: []string{"missing-output"}},
		},
	}
	r, cl, res := reconcileCluster(t,
		readyCluster(),
		statefulSet(1),
		pipeline,
		&gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
			Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "prof"},
		},
		&gnmicv1alpha1.TargetProfile{ObjectMeta: metav1.ObjectMeta{Name: "prof", Namespace: "default"}},
		&gnmicv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
	)

	// Nothing from the skipped pipeline reaches the plan.
	plan, err := r.GetClusterPlan("default", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 0 || len(plan.Subscriptions) != 0 {
		t.Fatalf("skipped pipeline still contributed to the plan: %d targets, %d subscriptions",
			len(plan.Targets), len(plan.Subscriptions))
	}

	// Every pipeline was skipped, so the empty plan must not be applied: doing so
	// would drain the collectors over a name that usually resolves a moment later.
	if res.RequeueAfter != reconcileBackstopInterval {
		t.Fatalf("requeue = %v, want %v (drain guard should have returned before apply)",
			res.RequeueAfter, reconcileBackstopInterval)
	}

	var got gnmicv1alpha1.Pipeline
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(pipeline), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Status != "Error" {
		t.Errorf("pipeline status = %q, want Error", got.Status.Status)
	}
	resolved := findCondition(got.Status.Conditions, PipelineConditionTypeResourcesResolved)
	if resolved == nil || resolved.Status != metav1.ConditionFalse {
		t.Fatalf("ResourcesResolved = %+v, want False", resolved)
	}
	if !strings.Contains(resolved.Message, "output/missing-output") {
		t.Errorf("message does not name the ref: %q", resolved.Message)
	}
}

// One broken pipeline must not take the healthy ones down with it.
func TestReconcileSkipsOnlyTheBrokenPipeline(t *testing.T) {
	broken := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef:       "c1",
			Enabled:          true,
			TargetRefs:       []string{"t1"},
			SubscriptionRefs: []string{"s1"},
			Outputs: gnmicv1alpha1.OutputSelector{
				OutputRefs:    []string{"o1"},
				ProcessorRefs: []string{"missing-proc"},
			},
		},
	}
	healthy := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "healthy", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef:       "c1",
			Enabled:          true,
			TargetRefs:       []string{"t2"},
			SubscriptionRefs: []string{"s1"},
			Outputs:          gnmicv1alpha1.OutputSelector{OutputRefs: []string{"o1"}},
		},
	}

	// readyReplicas 0 stops the reconcile at the readiness gate, after the plan is
	// built and cached but before any config is posted to a pod.
	r, cl, _ := reconcileCluster(t,
		readyCluster(),
		statefulSet(0),
		broken, healthy,
		&gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
			Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "prof"},
		},
		&gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{Name: "t2", Namespace: "default"},
			Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.2:57400", Profile: "prof"},
		},
		&gnmicv1alpha1.TargetProfile{ObjectMeta: metav1.ObjectMeta{Name: "prof", Namespace: "default"}},
		&gnmicv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
		&gnmicv1alpha1.Output{
			ObjectMeta: metav1.ObjectMeta{Name: "o1", Namespace: "default"},
			Spec:       gnmicv1alpha1.OutputSpec{Type: "file"},
		},
	)

	plan, err := r.GetClusterPlan("default", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.Targets["default/t2"]; !ok {
		t.Errorf("healthy pipeline's target missing from the plan: %v", keysOf(plan.Targets))
	}
	if _, ok := plan.Targets["default/t1"]; ok {
		t.Errorf("broken pipeline's target reached the plan: %v", keysOf(plan.Targets))
	}
	// No processor at all, and in particular none with an empty name.
	if len(plan.Processors) != 0 {
		t.Errorf("processors = %v, want none", plan.Processors)
	}

	var gotBroken, gotHealthy gnmicv1alpha1.Pipeline
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(broken), &gotBroken)
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(healthy), &gotHealthy)

	if gotBroken.Status.Status != "Error" {
		t.Errorf("broken pipeline status = %q, want Error", gotBroken.Status.Status)
	}
	if c := findCondition(gotBroken.Status.Conditions, PipelineConditionTypeResourcesResolved); c == nil ||
		!strings.Contains(c.Message, "processor/missing-proc") {
		t.Errorf("broken pipeline condition = %+v", c)
	}
	if gotHealthy.Status.Status == "Error" {
		t.Errorf("healthy pipeline was marked Error by its neighbour's bad ref")
	}
	if c := findCondition(gotHealthy.Status.Conditions, PipelineConditionTypeResourcesResolved); c == nil ||
		c.Status != metav1.ConditionTrue {
		t.Errorf("healthy pipeline ResourcesResolved = %+v, want True", c)
	}
}

// A missing TargetProfile used to fail the whole reconcile, which stalled every other
// pipeline on the cluster over one bad name.
func TestMissingTargetProfileSkipsPipelineNotCluster(t *testing.T) {
	broken := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef: "c1", Enabled: true,
			TargetRefs:       []string{"t1"},
			SubscriptionRefs: []string{"s1"},
			Outputs:          gnmicv1alpha1.OutputSelector{OutputRefs: []string{"o1"}},
		},
	}
	healthy := &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "healthy", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef: "c1", Enabled: true,
			TargetRefs:       []string{"t2"},
			SubscriptionRefs: []string{"s1"},
			Outputs:          gnmicv1alpha1.OutputSelector{OutputRefs: []string{"o1"}},
		},
	}

	r, cl, _ := reconcileCluster(t,
		readyCluster(),
		statefulSet(0),
		broken, healthy,
		&gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
			Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "gone"},
		},
		&gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{Name: "t2", Namespace: "default"},
			Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.2:57400", Profile: "prof"},
		},
		&gnmicv1alpha1.TargetProfile{ObjectMeta: metav1.ObjectMeta{Name: "prof", Namespace: "default"}},
		&gnmicv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
		&gnmicv1alpha1.Output{
			ObjectMeta: metav1.ObjectMeta{Name: "o1", Namespace: "default"},
			Spec:       gnmicv1alpha1.OutputSpec{Type: "file"},
		},
	)

	plan, err := r.GetClusterPlan("default", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.Targets["default/t2"]; !ok {
		t.Errorf("healthy pipeline blocked by the other pipeline's missing profile: %v", keysOf(plan.Targets))
	}

	var gotBroken gnmicv1alpha1.Pipeline
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(broken), &gotBroken)
	c := findCondition(gotBroken.Status.Conditions, PipelineConditionTypeResourcesResolved)
	if c == nil || !strings.Contains(c.Message, "targetprofile/gone") {
		t.Errorf("condition = %+v, want it to name targetprofile/gone", c)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
