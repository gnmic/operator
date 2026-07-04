package gnmic

import (
	"testing"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

func TestPlacementStrategyNew(t *testing.T) {
	p := &blrh{}
	if p.String() != string(PlacementStrategyBoundedHashing) {
		t.Fatalf("String() = %q", p.String())
	}

	s := New(PlacementStrategyBoundedHashing)
	targets := map[string]*gapi.TargetConfig{"t1": {Name: "t1"}}
	a := s.distributeTargets(targets, &PlacementStrategyOpts{NumPods: 2})
	if len(a) == 0 {
		t.Fatal("expected assignment")
	}
}

func TestNormalizeOptionsNil(t *testing.T) {
	opts := normalizeOptions(nil)
	if opts.NumPods != 1 || opts.Strategy != PlacementStrategyBoundedHashing {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
}
