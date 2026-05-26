package v1alpha1

import (
	"context"
	"testing"
	"time"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidators_RejectWrongType(t *testing.T) {
	ctx := context.Background()
	wrong := &operatorv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: "t"}}

	cases := []struct {
		name string
		test func() error
	}{
		{"cluster create", func() error { _, err := (&ClusterCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"cluster update", func() error {
			_, err := (&ClusterCustomValidator{}).ValidateUpdate(ctx, wrong, wrong)
			return err
		}},
		{"cluster delete", func() error { _, err := (&ClusterCustomValidator{}).ValidateDelete(ctx, wrong); return err }},
		{"pipeline create", func() error { _, err := (&PipelineCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"pipeline update", func() error { _, err := (&PipelineCustomValidator{}).ValidateUpdate(ctx, wrong, wrong); return err }},
		{"target create", func() error { _, err := (&TargetCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"subscription create", func() error { _, err := (&SubscriptionCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"output create", func() error { _, err := (&OutputCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"output update", func() error { _, err := (&OutputCustomValidator{}).ValidateUpdate(ctx, wrong, wrong); return err }},
		{"input create", func() error { _, err := (&InputCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"processor create", func() error { _, err := (&ProcessorCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"targetprofile create", func() error { _, err := (&TargetProfileCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
		{"tunneltargetpolicy create", func() error {
			_, err := (&TunnelTargetPolicyCustomValidator{}).ValidateCreate(ctx, wrong)
			return err
		}},
		{"targetsource create", func() error { _, err := (&TargetSourceCustomValidator{}).ValidateCreate(ctx, wrong); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.test(); err == nil {
				t.Fatal("expected type error")
			}
		})
	}
}

func TestDefaulters_RejectWrongType(t *testing.T) {
	ctx := context.Background()
	wrong := &operatorv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: "t"}}

	defaulters := []struct {
		name string
		run  func() error
	}{
		{"pipeline", func() error { return (&PipelineCustomDefaulter{}).Default(ctx, wrong) }},
		{"output", func() error { return (&OutputCustomDefaulter{}).Default(ctx, wrong) }},
		{"input", func() error { return (&InputCustomDefaulter{}).Default(ctx, wrong) }},
		{"processor", func() error { return (&ProcessorCustomDefaulter{}).Default(ctx, wrong) }},
		{"targetprofile", func() error { return (&TargetProfileCustomDefaulter{}).Default(ctx, wrong) }},
		{"tunneltargetpolicy", func() error { return (&TunnelTargetPolicyCustomDefaulter{}).Default(ctx, wrong) }},
		{"targetsource", func() error { return (&TargetSourceCustomDefaulter{}).Default(ctx, wrong) }},
	}
	for _, d := range defaulters {
		t.Run(d.name, func(t *testing.T) {
			if err := d.run(); err == nil {
				t.Fatal("expected type error")
			}
		})
	}
}

func TestTargetProfileValidator_Update(t *testing.T) {
	v := TargetProfileCustomValidator{}
	tp := &operatorv1alpha1.TargetProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: operatorv1alpha1.TargetProfileSpec{
			Encoding:   "JSON",
			Timeout:    metav1.Duration{Duration: time.Second},
			RetryTimer: metav1.Duration{Duration: time.Second},
		},
	}
	if _, err := v.ValidateUpdate(context.Background(), tp, tp); err != nil {
		t.Fatal(err)
	}
}
