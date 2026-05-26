package gnmic

import (
	"errors"
	"testing"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type mockCredsFetcher struct {
	creds *Credentials
	err   error
}

func (m *mockCredsFetcher) FetchCredentials(_, _ string) (*Credentials, error) {
	return m.creds, m.err
}

func TestPlanBuilder_Build(t *testing.T) {
	t.Setenv("DEBUG", "true")

	profile := gnmicv1alpha1.TargetProfileSpec{
		Encoding:   "JSON",
		Timeout:    metav1.Duration{Duration: 10 * time.Second},
		RetryTimer: metav1.Duration{Duration: 2 * time.Second},
	}

	pipeline := NewPipelineData()
	pipeline.Targets["default/t1"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
				"cluster-a": {Pod: "gnmic-cluster-a-2"},
			},
		},
	}
	pipeline.TargetProfiles["default/default"] = profile
	pipeline.Subscriptions["default/sub1"] = gnmicv1alpha1.SubscriptionSpec{
		Paths: []string{"/"},
		Mode:  "STREAM/SAMPLE",
	}
	pipeline.Outputs["default/out1"] = gnmicv1alpha1.OutputSpec{
		Type: PrometheusOutputType,
	}
	pipeline.Inputs["default/in1"] = gnmicv1alpha1.InputSpec{Type: "kafka"}
	pipeline.OutputProcessors["default/proc1"] = gnmicv1alpha1.ProcessorSpec{
		Type:   "event-starlark",
		Config: apiextensionsv1.JSON{Raw: []byte(`script: "true"`)},
	}
	pipeline.TunnelTargetPolicies["default/policy1"] = gnmicv1alpha1.TunnelTargetPolicySpec{
		Profile: "default",
	}

	builder := NewPlanBuilder("cluster-a", &mockCredsFetcher{
		creds: &Credentials{Username: "admin"},
	}).
		WithClientTLS(&ClientTLSPaths{CertFile: "/c", KeyFile: "/k"}).
		WithTargetDistributionCapacity(10).
		AddPipeline("pipe1", pipeline)

	plan, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 1 || len(plan.Subscriptions) != 1 {
		t.Fatalf("unexpected plan sizes: targets=%d subs=%d", len(plan.Targets), len(plan.Subscriptions))
	}
	if len(plan.CurrentTargetAssignment[2]) != 1 {
		t.Fatalf("expected pod 2 assignment, got %+v", plan.CurrentTargetAssignment)
	}
	if len(plan.PrometheusPorts) != 1 {
		t.Fatalf("expected prometheus port assignment, got %v", plan.PrometheusPorts)
	}
	if plan.Outputs["default/out1"]["listen"] == nil {
		t.Fatal("expected listen port on prometheus output")
	}

	// duplicate build paths should be no-ops
	plan2, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan2.Targets) != 1 {
		t.Fatal("expected idempotent build")
	}
}

func TestPlanBuilder_CredentialsError(t *testing.T) {
	pipeline := NewPipelineData()
	pipeline.Targets["default/t1"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
	}
	pipeline.TargetProfiles["default/default"] = gnmicv1alpha1.TargetProfileSpec{
		Encoding:       "JSON",
		CredentialsRef: "secret",
	}

	_, err := NewPlanBuilder("c", &mockCredsFetcher{err: errors.New("secret missing")}).
		AddPipeline("p", pipeline).
		Build()
	if err == nil {
		t.Fatal("expected credentials error")
	}
}

func TestAssignPorts(t *testing.T) {
	ports, err := assignPorts([]string{"a", "b", "c"}, PrometheusDefaultPort, PrmetheusPortPoolSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 3 {
		t.Fatalf("ports = %v", ports)
	}
	seen := make(map[int32]struct{})
	for _, p := range ports {
		if _, ok := seen[p]; ok {
			t.Fatalf("duplicate port %d", p)
		}
		seen[p] = struct{}{}
	}

	if _, err := assignPorts(nil, 1, 0); err == nil {
		t.Fatal("expected rangeSize error")
	}
}

func TestHash32(t *testing.T) {
	if hash32("x") == hash32("y") {
		t.Fatal("expected different hashes")
	}
	if hash32("same") != hash32("same") {
		t.Fatal("expected stable hash")
	}
}

func TestFindTargetCurrentAssignment(t *testing.T) {
	b := NewPlanBuilder("cluster-a", nil)
	target := gnmicv1alpha1.Target{
		Status: gnmicv1alpha1.TargetStatus{
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
				"cluster-a": {Pod: "gnmic-cluster-a-3"},
			},
		},
	}
	idx := b.findTargetCurrentAssignment(target)
	if idx == nil || *idx != 3 {
		t.Fatalf("pod index = %v", idx)
	}

	empty := gnmicv1alpha1.Target{}
	if b.findTargetCurrentAssignment(empty) != nil {
		t.Fatal("expected nil for missing pod")
	}
}
