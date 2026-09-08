package gnmic

import (
	"encoding/json"
	"strings"
	"testing"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
	"k8s.io/utils/ptr"
)

func redactionFixture() *ApplyPlan {
	return &ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"ns/with-password": {
				Name:     "ns/with-password",
				Address:  "10.0.0.1:57400",
				Username: ptr.To("admin"),
				Password: ptr.To("s3cr3t-password"),
				TLSCert:  ptr.To("/certs/tls.crt"),
				TLSKey:   ptr.To("/certs/tls.key"),
			},
			"ns/with-token": {
				Name:    "ns/with-token",
				Address: "10.0.0.2:57400",
				Token:   ptr.To("s3cr3t-token"),
			},
			"ns/no-creds": {Name: "ns/no-creds", Address: "10.0.0.3:57400"},
			"ns/nil":      nil,
		},
		CurrentTargetAssignment: map[int]map[string]struct{}{0: {"ns/with-password": {}}},
		Subscriptions:           map[string]*gapi.SubscriptionConfig{"ns/p/sub": {Name: "sub"}},
		Outputs:                 map[string]map[string]any{"ns/p/out": {"type": "prometheus"}},
		Inputs:                  map[string]map[string]any{"ns/p/in": {"type": "kafka"}},
		Processors:              map[string]map[string]any{"ns/p/proc": {"event-strings": map[string]any{}}},
		TunnelTargetMatches: map[string]*TunnelTargetMatch{
			"ns/p/policy": {Type: "GNMI_GNOI", ID: ".*", Config: &gapi.TargetConfig{
				Username: ptr.To("tunnel-user"),
				Password: ptr.To("s3cr3t-tunnel"),
			}},
			"ns/p/nil-policy": nil,
		},
		PrometheusPorts: map[string]int32{"ns/p/out": 9804},
	}
}

func TestRedactedMasksSecretsFromTargetsAndTunnelMatches(t *testing.T) {
	plan := redactionFixture()
	got := plan.Redacted()

	if *got.Targets["ns/with-password"].Password != RedactedSecret {
		t.Errorf("password = %q, want masked", *got.Targets["ns/with-password"].Password)
	}
	if *got.Targets["ns/with-token"].Token != RedactedSecret {
		t.Errorf("token = %q, want masked", *got.Targets["ns/with-token"].Token)
	}
	if *got.TunnelTargetMatches["ns/p/policy"].Config.Password != RedactedSecret {
		t.Errorf("tunnel password = %q, want masked", *got.TunnelTargetMatches["ns/p/policy"].Config.Password)
	}

	// Everything that is not a secret survives verbatim.
	tc := got.Targets["ns/with-password"]
	if *tc.Username != "admin" || tc.Address != "10.0.0.1:57400" || *tc.TLSCert != "/certs/tls.crt" || *tc.TLSKey != "/certs/tls.key" {
		t.Errorf("non-secret fields changed: %+v", tc)
	}
	if got.Targets["ns/no-creds"].Password != nil || got.Targets["ns/no-creds"].Token != nil {
		t.Errorf("redaction invented credentials on a target that had none")
	}
	if got.Targets["ns/nil"] != nil || got.TunnelTargetMatches["ns/p/nil-policy"] != nil {
		t.Errorf("nil entries should stay nil")
	}
	if got.TunnelTargetMatches["ns/p/policy"].Type != "GNMI_GNOI" || got.TunnelTargetMatches["ns/p/policy"].ID != ".*" {
		t.Errorf("tunnel match rules changed")
	}
}

func TestRedactedLeavesTheOriginalUntouched(t *testing.T) {
	plan := redactionFixture()
	got := plan.Redacted()

	if *plan.Targets["ns/with-password"].Password != "s3cr3t-password" ||
		*plan.Targets["ns/with-token"].Token != "s3cr3t-token" ||
		*plan.TunnelTargetMatches["ns/p/policy"].Config.Password != "s3cr3t-tunnel" {
		t.Fatal("redaction wrote into the source plan")
	}

	// Distinct containers: mutating the copy cannot reach the cached plan.
	got.Targets["ns/new"] = &gapi.TargetConfig{}
	got.Targets["ns/with-password"].Address = "changed"
	delete(got.Outputs, "ns/p/out")
	got.CurrentTargetAssignment[0]["ns/other"] = struct{}{}
	got.TunnelTargetMatches["ns/p/policy"].ID = "changed"
	if _, leaked := plan.Targets["ns/new"]; leaked {
		t.Error("Targets map is shared")
	}
	if plan.Targets["ns/with-password"].Address != "10.0.0.1:57400" {
		t.Error("TargetConfig is shared")
	}
	if _, ok := plan.Outputs["ns/p/out"]; !ok {
		t.Error("Outputs map is shared")
	}
	if _, leaked := plan.CurrentTargetAssignment[0]["ns/other"]; leaked {
		t.Error("CurrentTargetAssignment is shared")
	}
	if plan.TunnelTargetMatches["ns/p/policy"].ID != ".*" {
		t.Error("TunnelTargetMatch is shared")
	}
}

func TestRedactedKeepsEveryOtherSection(t *testing.T) {
	plan := redactionFixture()
	got := plan.Redacted()

	if len(got.Subscriptions) != 1 || got.Subscriptions["ns/p/sub"] != plan.Subscriptions["ns/p/sub"] {
		t.Error("subscriptions not carried over")
	}
	if len(got.Outputs) != 1 || len(got.Inputs) != 1 || len(got.Processors) != 1 {
		t.Error("outputs/inputs/processors not carried over")
	}
	if got.PrometheusPorts["ns/p/out"] != 9804 {
		t.Error("prometheus ports not carried over")
	}
	if _, ok := got.CurrentTargetAssignment[0]["ns/with-password"]; !ok {
		t.Error("current assignment not carried over")
	}
}

// The property the endpoint actually needs: no Secret value in the wire format.
func TestRedactedJSONContainsNoSecret(t *testing.T) {
	body, err := json.Marshal(redactionFixture().Redacted())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"s3cr3t-password", "s3cr3t-token", "s3cr3t-tunnel"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("payload leaks %q: %s", secret, body)
		}
	}
	if !strings.Contains(string(body), `"password":"****"`) {
		t.Errorf("masked password missing from payload: %s", body)
	}
	if !strings.Contains(string(body), `"username":"admin"`) {
		t.Errorf("username should still be visible: %s", body)
	}
}

func TestRedactedNilAndEmpty(t *testing.T) {
	var nilPlan *ApplyPlan
	if nilPlan.Redacted() != nil {
		t.Error("nil plan should redact to nil")
	}
	empty := (&ApplyPlan{}).Redacted()
	if empty == nil || len(empty.Targets) != 0 || empty.CurrentTargetAssignment != nil {
		t.Errorf("empty plan redacted to %+v", empty)
	}
}
