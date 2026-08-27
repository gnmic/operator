package gnmic

import (
	"fmt"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func promPipeline(outputs map[string]string) *PipelineData {
	p := NewPipelineData()
	p.Subscriptions["ns/p/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	for nn, rawConfig := range outputs {
		spec := gnmicv1alpha1.OutputSpec{Type: PrometheusOutputType}
		if rawConfig != "" {
			spec.Config = apiextensionsv1.JSON{Raw: []byte(rawConfig)}
		}
		p.Outputs[nn] = spec
	}
	return p
}

func buildPromPlan(t *testing.T, outputs map[string]string) *ApplyPlan {
	t.Helper()
	plan, err := NewPlanBuilder("c", nil).AddPipeline("p", promPipeline(outputs)).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return plan
}

// assertPortsAgree checks the invariant the drift bug broke: the port recorded in
// PrometheusPorts (which the Service is built from) is the port in the output's listen
// address (which the collector binds). Any divergence here is a Service pointing at a
// port nothing is listening on.
func assertPortsAgree(t *testing.T, plan *ApplyPlan) {
	t.Helper()
	for outputNN, cfg := range plan.Outputs {
		if typ, _ := cfg["type"].(string); typ != PrometheusOutputType {
			continue
		}
		listen, _ := cfg["listen"].(string)
		bound, err := ParseListenPort(listen)
		if err != nil {
			t.Fatalf("%s: unparseable listen %q: %v", outputNN, listen, err)
		}
		recorded, ok := plan.PrometheusPorts[outputNN]
		if !ok {
			t.Fatalf("%s: no port recorded in PrometheusPorts (%v)", outputNN, plan.PrometheusPorts)
		}
		if bound != recorded {
			t.Fatalf("%s: collector binds %d but PrometheusPorts says %d (listen=%q)",
				outputNN, bound, recorded, listen)
		}
	}
}

// An explicit listen is the user's choice of port. It used to be overwritten with a
// hash-assigned one, so the collector bound a port the Service never pointed at.
func TestExplicitListenPortIsKept(t *testing.T) {
	plan := buildPromPlan(t, map[string]string{
		"ns/p/pinned": `{"listen": ":9999"}`,
	})

	if got := plan.Outputs["ns/p/pinned"]["listen"]; got != ":9999" {
		t.Errorf("listen = %v, want \":9999\" (the plan overwrote the user's port)", got)
	}
	if got := plan.PrometheusPorts["ns/p/pinned"]; got != 9999 {
		t.Errorf("PrometheusPorts = %d, want 9999", got)
	}
	assertPortsAgree(t, plan)
}

// An explicit listen with a host part is still honoured, and the recorded port is the
// port alone.
func TestExplicitListenWithHostIsKept(t *testing.T) {
	plan := buildPromPlan(t, map[string]string{
		"ns/p/pinned": `{"listen": "0.0.0.0:9123"}`,
	})

	if got := plan.Outputs["ns/p/pinned"]["listen"]; got != "0.0.0.0:9123" {
		t.Errorf("listen = %v, want it untouched", got)
	}
	if got := plan.PrometheusPorts["ns/p/pinned"]; got != 9123 {
		t.Errorf("PrometheusPorts = %d, want 9123", got)
	}
}

// Outputs with no listen still get one from the pool, and it agrees with what is
// recorded for the Service.
func TestAssignedPortIsRecordedAndWritten(t *testing.T) {
	plan := buildPromPlan(t, map[string]string{
		"ns/p/auto1": "",
		"ns/p/auto2": `{"path": "/custom"}`,
	})

	for _, nn := range []string{"ns/p/auto1", "ns/p/auto2"} {
		if plan.PrometheusPorts[nn] == 0 {
			t.Fatalf("%s: no port assigned (%v)", nn, plan.PrometheusPorts)
		}
	}
	if plan.PrometheusPorts["ns/p/auto1"] == plan.PrometheusPorts["ns/p/auto2"] {
		t.Fatalf("two outputs share port %d", plan.PrometheusPorts["ns/p/auto1"])
	}
	// a listen-less config keeps its other keys
	if got := plan.Outputs["ns/p/auto2"]["path"]; got != "/custom" {
		t.Errorf("path = %v, want /custom", got)
	}
	assertPortsAgree(t, plan)
}

// An empty listen means "not set" rather than "bind :0".
func TestEmptyListenIsTreatedAsUnset(t *testing.T) {
	plan := buildPromPlan(t, map[string]string{"ns/p/blank": `{"listen": ""}`})

	if plan.PrometheusPorts["ns/p/blank"] == 0 {
		t.Fatalf("no port assigned for an empty listen: %v", plan.PrometheusPorts)
	}
	assertPortsAgree(t, plan)
}

// A pinned port is not ours to hand out: an output that would have hashed onto it has
// to move, or two Prometheus outputs in one collector process fight over one port.
func TestAssignedPortAvoidsPinnedPort(t *testing.T) {
	// First learn where "ns/p/auto" lands with nothing reserved.
	solo := buildPromPlan(t, map[string]string{"ns/p/auto": ""})
	preferred := solo.PrometheusPorts["ns/p/auto"]
	if preferred == 0 {
		t.Fatal("no port assigned in the solo case")
	}

	// Now pin exactly that port on a different output and rebuild.
	plan := buildPromPlan(t, map[string]string{
		"ns/p/auto":   "",
		"ns/p/pinned": fmt.Sprintf(`{"listen": ":%d"}`, preferred),
	})

	if got := plan.PrometheusPorts["ns/p/pinned"]; got != preferred {
		t.Errorf("pinned output moved to %d, want %d", got, preferred)
	}
	if got := plan.PrometheusPorts["ns/p/auto"]; got == preferred {
		t.Errorf("assigned output landed on the pinned port %d", got)
	}
	assertPortsAgree(t, plan)
}

// A listen the collector could never bind is a configuration error, not something to
// paper over by assigning a different port behind the user's back.
func TestMalformedListenIsAnError(t *testing.T) {
	for _, raw := range []string{`{"listen": "no-colon"}`, `{"listen": ":not-a-port"}`, `{"listen": 9804}`} {
		_, err := NewPlanBuilder("c", nil).
			AddPipeline("p", promPipeline(map[string]string{"ns/p/bad": raw})).
			Build()
		if err == nil {
			t.Errorf("config %s: expected an error", raw)
		}
	}
}

func TestParseListenPort(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    int32
		wantErr bool
	}{
		{":9804", 9804, false},
		{"0.0.0.0:9804", 9804, false},
		{"localhost:9804", 9804, false},
		{"[::1]:9804", 9804, false},
		{"  :9804  ", 9804, false},
		{"", 0, false},
		{"9804", 0, true},
		{":abc", 0, true},
	} {
		got, err := ParseListenPort(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseListenPort(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseListenPort(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestAssignPortsRespectsReserved(t *testing.T) {
	// Reserving arbitrary ports proves little in a 1009-slot pool -- the hash almost
	// never lands on them. Reserve the port "a" actually wants, so the test fails if
	// reservations are ignored.
	free, err := assignPorts([]string{"a"}, PrometheusDefaultPort, PrmetheusPortPoolSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	reserved := map[int32]struct{}{free["a"]: {}}

	ports, err := assignPorts([]string{"a", "b", "c"}, PrometheusDefaultPort, PrmetheusPortPoolSize, reserved)
	if err != nil {
		t.Fatal(err)
	}
	if ports["a"] == free["a"] {
		t.Errorf("a was assigned the reserved port %d", ports["a"])
	}
	for name, p := range ports {
		if _, taken := reserved[p]; taken {
			t.Errorf("%s was assigned reserved port %d", name, p)
		}
	}

	// A reserved port outside the pool cannot collide and must not consume a slot.
	if _, err := assignPorts([]string{"a"}, PrometheusDefaultPort, PrmetheusPortPoolSize,
		map[int32]struct{}{80: {}}); err != nil {
		t.Fatalf("out-of-range reservation: %v", err)
	}

	// rangeSize 1 degenerates the probe step to a division by zero.
	if _, err := assignPorts([]string{"a"}, 9000, 1, nil); err == nil {
		t.Fatal("expected an error for rangeSize 1")
	}
}
