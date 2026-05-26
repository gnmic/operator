package v1alpha1

import (
	"context"
	"testing"
	"time"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
)

func TestValidateClusterSpec(t *testing.T) {
	valid := &operatorv1alpha1.ClusterSpec{Image: "gnmic:latest"}
	if err := validateClusterSpec(valid); err != nil {
		t.Fatalf("valid spec: %v", err)
	}

	invalid := &operatorv1alpha1.ClusterSpec{
		Image:   "",
		Replicas: ptr.To(int32(0)),
		API: &operatorv1alpha1.APIConfig{
			RestPort: 0,
			TLS:      &operatorv1alpha1.ClusterTLSConfig{},
		},
	}
	if err := validateClusterSpec(invalid); err == nil {
		t.Fatal("expected validation errors")
	}
}

func TestClusterValidator(t *testing.T) {
	v := ClusterCustomValidator{}
	cluster := &operatorv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1"},
		Spec:       operatorv1alpha1.ClusterSpec{Image: "img"},
	}
	if _, err := v.ValidateCreate(context.Background(), cluster); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateCreate(context.Background(), &operatorv1alpha1.Target{}); err == nil {
		t.Fatal("expected type error")
	}
}

func TestValidatePipelineSpec(t *testing.T) {
	if err := validatePipelineSpec(&operatorv1alpha1.PipelineSpec{}); err == nil {
		t.Fatal("expected required fields error")
	}

	valid := &operatorv1alpha1.PipelineSpec{
		ClusterRef: "cluster-a",
		TargetRefs: []string{"t1"},
		SubscriptionRefs: []string{"sub1"},
		Outputs: operatorv1alpha1.OutputSelector{
			OutputRefs: []string{"out1"},
		},
	}
	if err := validatePipelineSpec(valid); err != nil {
		t.Fatalf("valid pipeline: %v", err)
	}
}

func TestValidateTargetSpec(t *testing.T) {
	if err := validateTargetSpec("t1", &operatorv1alpha1.TargetSpec{}); err == nil {
		t.Fatal("expected errors")
	}
	if err := validateTargetSpec("t1", &operatorv1alpha1.TargetSpec{
		Address: "10.0.0.1:57400",
		Profile: "default",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSubscriptionSpec(t *testing.T) {
	if err := validateSubscriptionSpec("s1", &operatorv1alpha1.SubscriptionSpec{}); err == nil {
		t.Fatal("expected paths required")
	}
	if err := validateSubscriptionSpec("s1", &operatorv1alpha1.SubscriptionSpec{
		Paths: []string{"/"},
		Mode:  "STREAM/SAMPLE",
	}); err != nil {
		t.Fatal(err)
	}
	if err := validateSubscriptionSpec("s1", &operatorv1alpha1.SubscriptionSpec{
		Paths:               []string{"/"},
		StreamSubscriptions: []string{"child"},
		Mode:                "POLL",
	}); err == nil {
		t.Fatal("expected stream mode error")
	}
	if err := validateSubscriptionSpec("s1", &operatorv1alpha1.SubscriptionSpec{
		Paths:             []string{"/"},
		SampleInterval:    metav1.Duration{Duration: -1},
		HeartbeatInterval: metav1.Duration{Duration: -1},
	}); err == nil {
		t.Fatal("expected negative interval errors")
	}
}

func TestValidateTargetSpec_AddressErrors(t *testing.T) {
	if err := validateTargetSpec("t", &operatorv1alpha1.TargetSpec{
		Address: "not-a-hostport",
		Profile: "p",
	}); err == nil {
		t.Fatal("expected host:port error")
	}
	if err := validateTargetSpec("t", &operatorv1alpha1.TargetSpec{
		Address: ":57400",
		Profile: "p",
	}); err == nil {
		t.Fatal("expected missing host error")
	}
}

func TestValidateTargetProfileSpec(t *testing.T) {
	errs := validateTargetProfileSpec(&operatorv1alpha1.TargetProfileSpec{
		Encoding: "INVALID",
	})
	if len(errs) == 0 {
		t.Fatal("expected encoding error")
	}
	errs = validateTargetProfileSpec(&operatorv1alpha1.TargetProfileSpec{
		Encoding:   "JSON",
		Timeout:    metav1.Duration{Duration: time.Second},
		RetryTimer: metav1.Duration{Duration: time.Second},
	})
	if len(errs) != 0 {
		t.Fatalf("valid profile: %v", errs)
	}
}

func TestValidateTunnelTargetPolicySpec(t *testing.T) {
	if err := validateTunnelTargetPolicySpec("p1", &operatorv1alpha1.TunnelTargetPolicySpec{}); err == nil {
		t.Fatal("expected profile required")
	}
	if err := validateTunnelTargetPolicySpec("p1", &operatorv1alpha1.TunnelTargetPolicySpec{
		Profile: "default",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClusterDefaulter(t *testing.T) {
	d := ClusterCustomDefaulter{}
	if err := d.Default(context.Background(), &operatorv1alpha1.Cluster{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Default(context.Background(), &operatorv1alpha1.Target{}); err == nil {
		t.Fatal("expected type error")
	}
}

func TestClusterValidator_AllMethods(t *testing.T) {
	v := ClusterCustomValidator{}
	cluster := &operatorv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1"},
		Spec: operatorv1alpha1.ClusterSpec{
			Image: "img",
			API: &operatorv1alpha1.APIConfig{
				RestPort: 7890,
				GNMIPort: 7890,
			},
		},
	}
	if _, err := v.ValidateUpdate(context.Background(), cluster, cluster); err == nil {
		t.Fatal("expected port conflict error")
	}
	if _, err := v.ValidateDelete(context.Background(), cluster); err != nil {
		t.Fatal(err)
	}
}

func TestPipelineWebhookMethods(t *testing.T) {
	v := PipelineCustomValidator{}
	d := PipelineCustomDefaulter{}
	p := &operatorv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: operatorv1alpha1.PipelineSpec{
			ClusterRef:       "c1",
			TargetRefs:       []string{"t1"},
			SubscriptionRefs: []string{"s1"},
			Outputs:          operatorv1alpha1.OutputSelector{OutputRefs: []string{"o1"}},
		},
	}
	if err := d.Default(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateCreate(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateUpdate(context.Background(), p, p); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateDelete(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestTargetWebhookMethods(t *testing.T) {
	v := TargetCustomValidator{}
	d := TargetCustomDefaulter{}
	target := &operatorv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Name: "t1"},
		Spec:       operatorv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
	}
	if err := d.Default(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateCreate(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateUpdate(context.Background(), target, target); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateDelete(context.Background(), target); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionWebhookMethods(t *testing.T) {
	v := SubscriptionCustomValidator{}
	d := SubscriptionCustomDefaulter{}
	sub := &operatorv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: "s1"},
		Spec: operatorv1alpha1.SubscriptionSpec{
			Paths:               []string{"/"},
			Mode:                "POLL",
			StreamSubscriptions: []string{"child"},
		},
	}
	if _, err := v.ValidateCreate(context.Background(), sub); err == nil {
		t.Fatal("expected stream mode error")
	}
	sub.Spec.Mode = "STREAM/SAMPLE"
	if err := d.Default(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateCreate(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
}

func TestTargetProfileWebhookMethods(t *testing.T) {
	v := TargetProfileCustomValidator{}
	d := TargetProfileCustomDefaulter{}
	tp := &operatorv1alpha1.TargetProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: operatorv1alpha1.TargetProfileSpec{
			Encoding:   "JSON",
			Timeout:    metav1.Duration{Duration: time.Second},
			RetryTimer: metav1.Duration{Duration: time.Second},
		},
	}
	if err := d.Default(context.Background(), tp); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateCreate(context.Background(), tp); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateDelete(context.Background(), tp); err != nil {
		t.Fatal(err)
	}
}

func TestTunnelTargetPolicyWebhookMethods(t *testing.T) {
	v := TunnelTargetPolicyCustomValidator{}
	d := TunnelTargetPolicyCustomDefaulter{}
	policy := &operatorv1alpha1.TunnelTargetPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: operatorv1alpha1.TunnelTargetPolicySpec{
			Profile: "default",
		},
	}
	if err := d.Default(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateUpdate(context.Background(), policy, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateDelete(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
}

func TestValidateClusterTLSAndPorts(t *testing.T) {
	errs := validateClusterTLS(&operatorv1alpha1.ClusterTLSConfig{}, field.NewPath("tls"))
	if len(errs) == 0 {
		t.Fatal("expected issuerRef required")
	}
	if validateClusterTLS(nil, field.NewPath("tls")) != nil {
		t.Fatal("expected nil for nil tls")
	}
}

func TestOtherWebhookDefaulters(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"output", func() error {
			return (&OutputCustomDefaulter{}).Default(ctx, &operatorv1alpha1.Output{})
		}},
		{"input", func() error {
			return (&InputCustomDefaulter{}).Default(ctx, &operatorv1alpha1.Input{})
		}},
		{"processor", func() error {
			return (&ProcessorCustomDefaulter{}).Default(ctx, &operatorv1alpha1.Processor{})
		}},
		{"targetsource", func() error {
			return (&TargetSourceCustomDefaulter{}).Default(ctx, &operatorv1alpha1.TargetSource{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOtherWebhookValidators_NoOp(t *testing.T) {
	ctx := context.Background()
	output := &operatorv1alpha1.Output{ObjectMeta: metav1.ObjectMeta{Name: "o1"}}
	v := OutputCustomValidator{}
	if _, err := v.ValidateCreate(ctx, output); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateUpdate(ctx, output, output); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateDelete(ctx, output); err != nil {
		t.Fatal(err)
	}

	input := &operatorv1alpha1.Input{ObjectMeta: metav1.ObjectMeta{Name: "i1"}}
	iv := InputCustomValidator{}
	if _, err := iv.ValidateCreate(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := iv.ValidateUpdate(ctx, input, input); err != nil {
		t.Fatal(err)
	}
	if _, err := iv.ValidateDelete(ctx, input); err != nil {
		t.Fatal(err)
	}

	proc := &operatorv1alpha1.Processor{ObjectMeta: metav1.ObjectMeta{Name: "p1"}}
	pv := ProcessorCustomValidator{}
	if _, err := pv.ValidateCreate(ctx, proc); err != nil {
		t.Fatal(err)
	}
	if _, err := pv.ValidateUpdate(ctx, proc, proc); err != nil {
		t.Fatal(err)
	}
	if _, err := pv.ValidateDelete(ctx, proc); err != nil {
		t.Fatal(err)
	}

	ts := &operatorv1alpha1.TargetSource{ObjectMeta: metav1.ObjectMeta{Name: "ts1"}}
	tsv := TargetSourceCustomValidator{}
	if _, err := tsv.ValidateCreate(ctx, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := tsv.ValidateUpdate(ctx, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := tsv.ValidateDelete(ctx, ts); err != nil {
		t.Fatal(err)
	}
}
