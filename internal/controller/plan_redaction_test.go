package controller

import (
	"testing"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
	"k8s.io/utils/ptr"

	"github.com/gnmic/operator/internal/gnmic"
)

// The API server is the only reader outside this package, and what it gets must
// be a redacted copy: no credential, and no pointer into the cached plan.
func TestRedactedClusterPlanIsACopyWithoutSecrets(t *testing.T) {
	r := NewClusterReconcilerForTest()
	plan := &gnmic.ApplyPlan{Targets: map[string]*gapi.TargetConfig{
		"ns/t1": {Name: "ns/t1", Username: ptr.To("u"), Password: ptr.To("plaintext")},
	}}
	r.CachePlan("ns", "c", plan)

	got, err := r.RedactedClusterPlan("ns", "c")
	if err != nil {
		t.Fatal(err)
	}
	if got == plan {
		t.Fatal("RedactedClusterPlan handed out the cached pointer")
	}
	if *got.Targets["ns/t1"].Password != gnmic.RedactedSecret {
		t.Errorf("password = %q, want masked", *got.Targets["ns/t1"].Password)
	}
	if *plan.Targets["ns/t1"].Password != "plaintext" {
		t.Error("the cached plan was modified")
	}
	// The internal accessor is unchanged: the reconciler and its tests still see
	// the real plan.
	raw, err := r.GetClusterPlan("ns", "c")
	if err != nil || raw != plan || *raw.Targets["ns/t1"].Password != "plaintext" {
		t.Errorf("GetClusterPlan changed behaviour: plan=%p err=%v", raw, err)
	}

	if _, err := r.RedactedClusterPlan("ns", "missing"); err == nil {
		t.Error("expected not found")
	}
}
