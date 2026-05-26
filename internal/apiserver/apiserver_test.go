package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/gnmic"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

func TestGetClusterPlan(t *testing.T) {
	plan := &gnmic.ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"default/t1": {Name: "default/t1"},
		},
	}
	reconciler := controller.NewClusterReconcilerForTest()
	reconciler.CachePlan("default", "cluster-a", plan)

	srv := New(":0", reconciler)
	ts := httptest.NewServer(srv.Server.Handler)
	defer ts.Close()

	t.Run("found", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/clusters/default/cluster-a/plan")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var got gnmic.ApplyPlan
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Targets) != 1 {
			t.Fatalf("targets = %d, want 1", len(got.Targets))
		}
	})

	t.Run("not found", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/clusters/default/missing/plan")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}
