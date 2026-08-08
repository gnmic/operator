package v1alpha1

import (
	"context"
	"testing"
	"time"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
