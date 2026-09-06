package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
)

var tsNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func tsScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func newTSReconciler(t *testing.T, objs ...client.Object) (*TargetSourceReconciler, client.Client) {
	scheme := tsScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&gnmicv1alpha1.TargetSource{}, &gnmicv1alpha1.Target{}).
		WithIndex(&gnmicv1alpha1.TargetSource{}, TargetSourceSecretsIndex, func(o client.Object) []string {
			return discovery.ReferencedSecrets(&o.(*gnmicv1alpha1.TargetSource).Spec)
		}).
		WithIndex(&gnmicv1alpha1.TargetSource{}, TargetSourceConfigMapsIndex, func(o client.Object) []string {
			return discovery.ReferencedConfigMaps(&o.(*gnmicv1alpha1.TargetSource).Spec)
		}).
		Build()
	r := &TargetSourceReconciler{
		Client: c,
		Scheme: scheme,
		Now:    func() time.Time { return tsNow },
		Random: func() float64 { return 0 },
	}
	return r, c
}

func staticSource(devices ...gnmicv1alpha1.StaticDevice) *gnmicv1alpha1.TargetSource {
	return &gnmicv1alpha1.TargetSource{
		ObjectMeta: metav1.ObjectMeta{Name: "inv", Namespace: "default", UID: "ts-uid-1", Generation: 1},
		Spec: gnmicv1alpha1.TargetSourceSpec{
			Interval: &metav1.Duration{Duration: 5 * time.Minute},
			Source:   gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeStatic, Static: &gnmicv1alpha1.StaticSource{Devices: devices}},
			Target:   gnmicv1alpha1.TargetTemplateSpec{Port: 57400, Profile: "default"},
			Prune:    gnmicv1alpha1.PruneSpec{MaxDeleteRatio: ptr.To(int32(50))},
		},
	}
}

func httpSource(url string) *gnmicv1alpha1.TargetSource {
	ts := staticSource()
	ts.Spec.Source = gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeHTTP, HTTP: &gnmicv1alpha1.HTTPSource{
		EndpointSpec: gnmicv1alpha1.EndpointSpec{URL: url},
	}}
	return ts
}

func dev(name, addr string) gnmicv1alpha1.StaticDevice {
	return gnmicv1alpha1.StaticDevice{Name: name, Address: addr}
}

func reconcileTS(t *testing.T, r *TargetSourceReconciler) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "inv"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

func getTS(t *testing.T, c client.Client) *gnmicv1alpha1.TargetSource {
	t.Helper()
	var ts gnmicv1alpha1.TargetSource
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "inv"}, &ts); err != nil {
		t.Fatal(err)
	}
	return &ts
}

func updateTS(t *testing.T, c client.Client, mutate func(*gnmicv1alpha1.TargetSource)) {
	t.Helper()
	ts := getTS(t, c)
	mutate(ts)
	ts.Generation++
	if err := c.Update(context.Background(), ts); err != nil {
		t.Fatal(err)
	}
}

func listTargets(t *testing.T, c client.Client) map[string]gnmicv1alpha1.Target {
	t.Helper()
	var list gnmicv1alpha1.TargetList
	if err := c.List(context.Background(), &list, client.InNamespace("default")); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]gnmicv1alpha1.Target, len(list.Items))
	for _, item := range list.Items {
		out[item.Name] = item
	}
	return out
}

func condition(t *testing.T, ts *gnmicv1alpha1.TargetSource, condType string) *metav1.Condition {
	t.Helper()
	return apimeta.FindStatusCondition(ts.Status.Conditions, condType)
}

func expectCondition(t *testing.T, ts *gnmicv1alpha1.TargetSource, condType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	c := condition(t, ts, condType)
	if c == nil {
		t.Fatalf("condition %s missing: %+v", condType, ts.Status.Conditions)
	}
	if c.Status != status || c.Reason != reason {
		t.Fatalf("condition %s = %s/%s (%s), want %s/%s", condType, c.Status, c.Reason, c.Message, status, reason)
	}
	if c.ObservedGeneration != ts.Generation {
		t.Fatalf("condition %s observedGeneration = %d, want %d", condType, c.ObservedGeneration, ts.Generation)
	}
}

func TestTargetSourceCreatesTargets(t *testing.T) {
	r, c := newTSReconciler(t, staticSource(dev("leaf1", "10.0.0.1"), gnmicv1alpha1.StaticDevice{
		Name: "Spine_1", Address: "2001:db8::1", Port: 57401, Profile: "srl", Labels: map[string]string{"site": "ams 1"},
	}))
	res := reconcileTS(t, r)
	if res.RequeueAfter != 5*time.Minute {
		t.Fatalf("requeue = %s, want 5m", res.RequeueAfter)
	}

	targets := listTargets(t, c)
	if len(targets) != 2 {
		t.Fatalf("targets = %v", targets)
	}
	leaf := targets["inv-leaf1"]
	if leaf.Spec.Address != "10.0.0.1:57400" || leaf.Spec.Profile != "default" {
		t.Errorf("leaf spec = %+v", leaf.Spec)
	}
	if leaf.Labels[discovery.LabelTargetSource] != "inv" || leaf.Labels[discovery.LabelManagedBy] != discovery.LabelManagedByValue {
		t.Errorf("leaf labels = %v", leaf.Labels)
	}
	owner := metav1.GetControllerOf(&leaf)
	if owner == nil || owner.UID != "ts-uid-1" || owner.Kind != "TargetSource" {
		t.Errorf("leaf owner = %+v", owner)
	}
	if leaf.Annotations[discovery.AnnotationDiscoveredName] != "leaf1" || leaf.Annotations[discovery.AnnotationHash] == "" {
		t.Errorf("leaf annotations = %v", leaf.Annotations)
	}
	spine := targets["inv-spine-1"]
	if spine.Spec.Address != "[2001:db8::1]:57401" || spine.Spec.Profile != "srl" || spine.Labels["site"] != "ams-1" {
		t.Errorf("spine = %+v labels=%v", spine.Spec, spine.Labels)
	}
	if spine.Annotations[discovery.AnnotationLabelPrefix+"site"] != "ams 1" {
		t.Errorf("spine label original missing: %v", spine.Annotations)
	}

	ts := getTS(t, c)
	expectCondition(t, ts, TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
	expectCondition(t, ts, TargetSourceConditionReconciling, metav1.ConditionFalse, ReasonConverged)
	if condition(t, ts, TargetSourceConditionStalled) != nil || condition(t, ts, TargetSourceConditionConflicted) != nil {
		t.Errorf("Stalled/Conflicted should be absent on a clean run: %+v", ts.Status.Conditions)
	}
	st := ts.Status
	if st.Discovered != 2 || st.Managed != 2 || st.Sanitized != 1 || st.Invalid != 0 || st.ObservedGeneration != 1 {
		t.Errorf("status = %+v", st)
	}
	if st.LastSuccessfulSyncTime == nil || !st.LastSuccessfulSyncTime.Time.Equal(tsNow) {
		t.Errorf("lastSuccessfulSyncTime = %v", st.LastSuccessfulSyncTime)
	}
	if st.NextSyncTime == nil || !st.NextSyncTime.Time.Equal(tsNow.Add(5*time.Minute)) {
		t.Errorf("nextSyncTime = %v", st.NextSyncTime)
	}
	if st.SourceDigest == "" {
		t.Error("sourceDigest empty")
	}
}

func TestTargetSourceUpdatesOnlyChangedTargets(t *testing.T) {
	r, c := newTSReconciler(t, staticSource(dev("leaf1", "10.0.0.1"), dev("leaf2", "10.0.0.2")))
	reconcileTS(t, r)
	before := listTargets(t, c)

	updateTS(t, c, func(ts *gnmicv1alpha1.TargetSource) {
		ts.Spec.Source.Static.Devices[1].Address = "10.0.0.22"
	})
	reconcileTS(t, r)
	after := listTargets(t, c)

	if after["inv-leaf2"].Spec.Address != "10.0.0.22:57400" {
		t.Errorf("leaf2 not updated: %+v", after["inv-leaf2"].Spec)
	}
	if after["inv-leaf1"].ResourceVersion != before["inv-leaf1"].ResourceVersion {
		t.Errorf("leaf1 was rewritten although nothing changed: rv %s -> %s", before["inv-leaf1"].ResourceVersion, after["inv-leaf1"].ResourceVersion)
	}
	if getTS(t, c).Status.Managed != 2 {
		t.Errorf("managed = %d", getTS(t, c).Status.Managed)
	}
}

func TestTargetSourcePrunesWithinRatio(t *testing.T) {
	r, c := newTSReconciler(t, staticSource(dev("leaf1", "10.0.0.1"), dev("leaf2", "10.0.0.2")))
	reconcileTS(t, r)
	updateTS(t, c, func(ts *gnmicv1alpha1.TargetSource) {
		ts.Spec.Source.Static.Devices = ts.Spec.Source.Static.Devices[:1]
	})
	reconcileTS(t, r)
	targets := listTargets(t, c)
	if len(targets) != 1 {
		t.Fatalf("targets = %v", targets)
	}
	if _, ok := targets["inv-leaf1"]; !ok {
		t.Fatalf("wrong target pruned: %v", targets)
	}
	ts := getTS(t, c)
	expectCondition(t, ts, TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
	if ts.Status.Pruned != 1 || ts.Status.Managed != 1 {
		t.Errorf("status = %+v", ts.Status)
	}
}

func TestTargetSourcePruneGuardHoldsDeletions(t *testing.T) {
	r, c := newTSReconciler(t, staticSource(dev("a", "10.0.0.1"), dev("b", "10.0.0.2"), dev("c", "10.0.0.3"), dev("d", "10.0.0.4")))
	reconcileTS(t, r)
	// 3 of 4 gone is 75%, over the 50% guard. A new device still gets created.
	updateTS(t, c, func(ts *gnmicv1alpha1.TargetSource) {
		ts.Spec.Source.Static.Devices = []gnmicv1alpha1.StaticDevice{dev("a", "10.0.0.1"), dev("new", "10.0.0.9")}
	})
	res := reconcileTS(t, r)
	targets := listTargets(t, c)
	if len(targets) != 5 {
		t.Fatalf("expected the 4 old Targets kept and the new one created, got %v", targets)
	}
	ts := getTS(t, c)
	expectCondition(t, ts, TargetSourceConditionReady, metav1.ConditionFalse, "PruneGuard")
	if condition(t, ts, TargetSourceConditionStalled) != nil {
		t.Error("a held prune is not Stalled")
	}
	if ts.Status.ConsecutiveFailures != 0 || res.RequeueAfter != 5*time.Minute {
		t.Errorf("a held prune keeps the normal interval: failures=%d requeue=%s", ts.Status.ConsecutiveFailures, res.RequeueAfter)
	}

	updateTS(t, c, func(ts *gnmicv1alpha1.TargetSource) { ts.Spec.Prune.MaxDeleteRatio = ptr.To(int32(100)) })
	reconcileTS(t, r)
	if targets = listTargets(t, c); len(targets) != 2 {
		t.Fatalf("after raising the ratio: %v", targets)
	}
	expectCondition(t, getTS(t, c), TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
}

func TestTargetSourceEmptySourceGuard(t *testing.T) {
	var body atomic.Value
	body.Store(`[{"name":"a","address":"10.0.0.1"},{"name":"b","address":"10.0.0.2"}]`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer srv.Close()
	ts := httpSource(srv.URL)
	ts.Spec.Prune.MaxDeleteRatio = ptr.To(int32(100))
	r, c := newTSReconciler(t, ts)
	reconcileTS(t, r)
	if len(listTargets(t, c)) != 2 {
		t.Fatal("setup failed")
	}

	body.Store(`[]`)
	reconcileTS(t, r)
	if len(listTargets(t, c)) != 2 {
		t.Fatal("an empty source must not prune by default")
	}
	got := getTS(t, c)
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionFalse, "EmptySource")

	updateTS(t, c, func(ts *gnmicv1alpha1.TargetSource) { ts.Spec.Prune.AllowEmptySource = true })
	reconcileTS(t, r)
	if len(listTargets(t, c)) != 0 {
		t.Fatal("allowEmptySource should prune everything")
	}
	got = getTS(t, c)
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
	if got.Status.Managed != 0 || got.Status.Pruned != 2 {
		t.Errorf("status = %+v", got.Status)
	}
}

func TestTargetSourceRestoresHandDeletedTarget(t *testing.T) {
	r, c := newTSReconciler(t, staticSource(dev("leaf1", "10.0.0.1")))
	reconcileTS(t, r)
	target := listTargets(t, c)["inv-leaf1"]
	if err := c.Delete(context.Background(), &target); err != nil {
		t.Fatal(err)
	}
	reconcileTS(t, r)
	if _, ok := listTargets(t, c)["inv-leaf1"]; !ok {
		t.Fatal("hand-deleted Target was not recreated")
	}
}

func TestTargetSourceLeavesUnmanagedTargetAlone(t *testing.T) {
	foreign := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "inv-leaf1", Namespace: "default", Labels: map[string]string{"owner": "human"}},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "192.168.0.1:57400", Profile: "manual"},
	}
	r, c := newTSReconciler(t, staticSource(dev("leaf1", "10.0.0.1"), dev("leaf2", "10.0.0.2")), foreign)
	reconcileTS(t, r)
	targets := listTargets(t, c)
	if targets["inv-leaf1"].Spec.Address != "192.168.0.1:57400" || targets["inv-leaf1"].Labels["owner"] != "human" {
		t.Fatalf("unmanaged Target was overwritten: %+v", targets["inv-leaf1"])
	}
	if _, ok := targets["inv-leaf2"]; !ok {
		t.Fatal("the other device should still be created")
	}
	ts := getTS(t, c)
	expectCondition(t, ts, TargetSourceConditionConflicted, metav1.ConditionTrue, ReasonNameConflict)
	expectCondition(t, ts, TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
	if ts.Status.Conflicted != 1 || ts.Status.Managed != 1 {
		t.Errorf("status = %+v", ts.Status)
	}
}

func TestTargetSourceSuspend(t *testing.T) {
	ts := staticSource(dev("leaf1", "10.0.0.1"))
	ts.Spec.Suspend = true
	r, c := newTSReconciler(t, ts)
	res := reconcileTS(t, r)
	if res != (ctrl.Result{}) {
		t.Fatalf("suspended sources are not requeued: %+v", res)
	}
	if len(listTargets(t, c)) != 0 {
		t.Fatal("suspended source created Targets")
	}
	got := getTS(t, c)
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionFalse, ReasonSuspended)
	if got.Status.NextSyncTime != nil {
		t.Errorf("nextSyncTime should be cleared: %v", got.Status.NextSyncTime)
	}
}

func TestTargetSourceFetchFailureBacksOffAndRecovers(t *testing.T) {
	var status atomic.Int32
	status.Store(503)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(status.Load())
		w.WriteHeader(code)
		if code == 200 {
			_, _ = w.Write([]byte(`[{"name":"a","address":"10.0.0.1"}]`))
		}
	}))
	defer srv.Close()
	r, c := newTSReconciler(t, httpSource(srv.URL))

	res := reconcileTS(t, r)
	got := getTS(t, c)
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionFalse, ReasonFetchFailed)
	expectCondition(t, got, TargetSourceConditionReconciling, metav1.ConditionTrue, ReasonRetryScheduled)
	if got.Status.ConsecutiveFailures != 1 || res.RequeueAfter != 5*time.Minute || got.Status.LastError == "" {
		t.Fatalf("first failure: failures=%d requeue=%s err=%q", got.Status.ConsecutiveFailures, res.RequeueAfter, got.Status.LastError)
	}
	res = reconcileTS(t, r)
	got = getTS(t, c)
	if got.Status.ConsecutiveFailures != 2 || res.RequeueAfter != 10*time.Minute {
		t.Fatalf("second failure: failures=%d requeue=%s", got.Status.ConsecutiveFailures, res.RequeueAfter)
	}
	if got.Status.LastSuccessfulSyncTime != nil || len(listTargets(t, c)) != 0 {
		t.Fatal("nothing should have succeeded yet")
	}

	status.Store(200)
	res = reconcileTS(t, r)
	got = getTS(t, c)
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
	if got.Status.ConsecutiveFailures != 0 || got.Status.LastError != "" || res.RequeueAfter != 5*time.Minute {
		t.Fatalf("recovery: %+v requeue=%s", got.Status, res.RequeueAfter)
	}
	if len(listTargets(t, c)) != 1 {
		t.Fatal("recovery should create the Target")
	}
}

func TestTargetSourceMissingSecretIsStalled(t *testing.T) {
	ts := httpSource("http://127.0.0.1:1")
	ts.Spec.Source.HTTP.Auth = &gnmicv1alpha1.AuthSpec{Token: &gnmicv1alpha1.TokenAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "missing", Key: "token"}}}
	r, c := newTSReconciler(t, ts)
	res := reconcileTS(t, r)
	got := getTS(t, c)
	expectCondition(t, got, TargetSourceConditionStalled, metav1.ConditionTrue, "SecretNotFound")
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionFalse, "SecretNotFound")
	if res.RequeueAfter != 50*time.Minute {
		t.Fatalf("stalled sources wait at the backoff cap, got %s", res.RequeueAfter)
	}
}

func TestTargetSourceReportsInvalidDevices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"hostname":"a","ip":"10.0.0.1"},{"hostname":"b"}]}`))
	}))
	defer srv.Close()
	ts := httpSource(srv.URL)
	ts.Spec.Source.HTTP.Mapping = &gnmicv1alpha1.MappingSpec{Items: "self.results", Name: "item.hostname", Address: "item.ip"}
	r, c := newTSReconciler(t, ts)
	reconcileTS(t, r)
	got := getTS(t, c)
	expectCondition(t, got, TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded)
	if got.Status.Invalid != 1 || got.Status.Managed != 1 || len(got.Status.FailedDevices) != 1 {
		t.Fatalf("status = %+v", got.Status)
	}
	if got.Status.FailedDevices[0].Name != "b" || got.Status.FailedDevices[0].Reason != "MappingError(address)" {
		t.Fatalf("failedDevices = %+v", got.Status.FailedDevices)
	}
}

func TestTargetSourceMaxTargets(t *testing.T) {
	ts := staticSource(dev("a", "10.0.0.1"), dev("b", "10.0.0.2"))
	ts.Spec.MaxTargets = ptr.To(int32(1))
	r, c := newTSReconciler(t, ts)
	reconcileTS(t, r)
	if len(listTargets(t, c)) != 0 {
		t.Fatal("over-capacity runs must change nothing")
	}
	expectCondition(t, getTS(t, c), TargetSourceConditionReady, metav1.ConditionFalse, ReasonCapacityExceeded)
}

func TestTargetSourceUserLabelsSurvive(t *testing.T) {
	r, c := newTSReconciler(t, staticSource(gnmicv1alpha1.StaticDevice{Name: "leaf1", Address: "10.0.0.1", Labels: map[string]string{"site": "ams"}}))
	reconcileTS(t, r)

	target := listTargets(t, c)["inv-leaf1"]
	target.Labels["team"] = "netops"
	if err := c.Update(context.Background(), &target); err != nil {
		t.Fatal(err)
	}
	updateTS(t, c, func(ts *gnmicv1alpha1.TargetSource) {
		ts.Spec.Source.Static.Devices[0].Labels["site"] = "fra"
	})
	reconcileTS(t, r)
	got := listTargets(t, c)["inv-leaf1"]
	if got.Labels["team"] != "netops" {
		t.Errorf("user label was erased: %v", got.Labels)
	}
	if got.Labels["site"] != "fra" {
		t.Errorf("source label was not updated: %v", got.Labels)
	}
}

func TestTargetSourceChangedPredicate(t *testing.T) {
	old := staticSource()
	p := targetSourceChangedPredicate{}
	statusOnly := old.DeepCopy()
	statusOnly.Status.Managed = 3
	if p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: statusOnly}) {
		t.Error("a status-only write must not trigger a run")
	}
	annotated := old.DeepCopy()
	annotated.Annotations = map[string]string{discovery.AnnotationRequestedAt: "2026-09-06T12:00:00Z"}
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: annotated}) {
		t.Error("the requested-at annotation must trigger a run")
	}
	bumped := old.DeepCopy()
	bumped.Generation = 2
	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: bumped}) {
		t.Error("a generation change must trigger a run")
	}
}

func TestTargetSourcesForSecretUsesIndex(t *testing.T) {
	withSecret := httpSource("http://x")
	withSecret.Spec.Source.HTTP.Auth = &gnmicv1alpha1.AuthSpec{Token: &gnmicv1alpha1.TokenAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "tok", Key: "t"}}}
	other := staticSource(dev("a", "1.1.1.1"))
	other.Name = "other"
	other.UID = "ts-uid-2"
	r, _ := newTSReconciler(t, withSecret, other)

	reqs := r.targetSourcesForSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tok", Namespace: "default"}})
	if len(reqs) != 1 || reqs[0].Name != "inv" {
		t.Fatalf("requests = %v", reqs)
	}
	if reqs := r.targetSourcesForSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"}}); len(reqs) != 0 {
		t.Fatalf("unrelated secret mapped to %v", reqs)
	}
}

func TestTargetSourceTimeoutIsCappedAtInterval(t *testing.T) {
	r, _ := newTSReconciler(t)
	cases := []struct {
		interval, timeout *metav1.Duration
		want              time.Duration
	}{
		{nil, nil, time.Minute},
		{&metav1.Duration{Duration: 30 * time.Second}, nil, 30 * time.Second},
		{&metav1.Duration{Duration: 30 * time.Second}, &metav1.Duration{Duration: 2 * time.Minute}, 30 * time.Second},
		{&metav1.Duration{Duration: 5 * time.Minute}, &metav1.Duration{Duration: 20 * time.Second}, 20 * time.Second},
	}
	for _, c := range cases {
		ts := staticSource()
		ts.Spec.Interval, ts.Spec.Timeout = c.interval, c.timeout
		if got := r.newRun(ts).timeout(); got != c.want {
			t.Errorf("interval=%v timeout=%v: got %s, want %s", c.interval, c.timeout, got, c.want)
		}
	}
}
