package provider

import (
	"strings"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func TestParseNativeList(t *testing.T) {
	doc := `[
	  {"name": "leaf1", "address": "10.0.0.1", "port": 57400, "profile": "srl", "labels": {"site": "ams1", "rack": 7}},
	  {"name": "leaf2", "address": "10.0.0.2"},
	  {"name": "leaf3"},
	  {"address": "10.0.0.4"},
	  "not-an-object",
	  {"name": "leaf5", "address": "10.0.0.5", "port": "70000"}
	]`
	devices, failures, err := Parse([]byte(doc), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2: %+v", len(devices), devices)
	}
	if devices[0].Name != "leaf1" || devices[0].Port != 57400 || devices[0].Profile != "srl" || devices[0].Labels["rack"] != "7" {
		t.Errorf("leaf1 = %+v", devices[0])
	}
	if devices[1].Port != 0 {
		t.Errorf("leaf2 port should be unset, got %d", devices[1].Port)
	}
	want := map[string]string{"leaf3": "MissingAddress", "#3": "MissingName", "#4": "NotAnObject", "leaf5": "InvalidPort"}
	if len(failures) != len(want) {
		t.Fatalf("failures = %+v", failures)
	}
	for _, f := range failures {
		if want[f.Name] != f.Reason {
			t.Errorf("failure %s = %s, want %s", f.Name, f.Reason, want[f.Name])
		}
	}
}

func TestParseNativeListYAML(t *testing.T) {
	doc := "- name: leaf1\n  address: 10.0.0.1\n- name: leaf2\n  address: 10.0.0.2\n  port: 57401\n"
	devices, failures, err := Parse([]byte(doc), nil)
	if err != nil || len(failures) != 0 || len(devices) != 2 || devices[1].Port != 57401 {
		t.Fatalf("devices=%+v failures=%+v err=%v", devices, failures, err)
	}
}

func TestParseEmptyAndNotAList(t *testing.T) {
	devices, failures, err := Parse([]byte("  \n"), nil)
	if err != nil || len(devices) != 0 || len(failures) != 0 {
		t.Fatalf("empty document: %v %v %v", devices, failures, err)
	}
	if _, _, err := Parse([]byte(`{"results": []}`), nil); err == nil || !strings.Contains(err.Error(), "must be a list") {
		t.Fatalf("object without mapping should fail, got %v", err)
	}
	if _, _, err := Parse([]byte(`{not json`), nil); err == nil {
		t.Fatal("garbage should fail")
	}
}

func TestParseMapped(t *testing.T) {
	doc := `{"results": [
	  {"hostname": "leaf1", "mgmt_ip": "10.0.0.1", "site": "ams1", "gnmi_port": "57401"},
	  {"hostname": "leaf2", "mgmt_ip": "10.0.0.2", "site": "ams2"},
	  {"hostname": "leaf3", "site": "ams3"}
	]}`
	mapping := &gnmicv1alpha1.MappingSpec{
		Items:   "self.results",
		Name:    "item.hostname",
		Address: "item.mgmt_ip",
		Port:    "has(item.gnmi_port) ? item.gnmi_port : 0",
		Labels:  `{"site": item.site}`,
	}
	devices, failures, err := Parse([]byte(doc), mapping)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %+v", devices)
	}
	if devices[0].Port != 57401 || devices[0].Labels["site"] != "ams1" || devices[1].Port != 0 {
		t.Errorf("devices = %+v", devices)
	}
	if len(failures) != 1 || failures[0].Name != "leaf3" || !strings.HasPrefix(failures[0].Reason, "MappingError(address)") {
		t.Errorf("failures = %+v", failures)
	}
}

func TestParseMappedFailPolicy(t *testing.T) {
	mapping := &gnmicv1alpha1.MappingSpec{Address: "item.mgmt_ip", OnError: gnmicv1alpha1.MappingErrorFail}
	_, _, err := Parse([]byte(`[{"name": "leaf1"}]`), mapping)
	if err == nil || IsSpecError(err) {
		t.Fatalf("Fail policy should return a run error, got %v", err)
	}
}

func TestParseMappedCompileErrorIsSpecError(t *testing.T) {
	mapping := &gnmicv1alpha1.MappingSpec{Items: "self.results["}
	_, _, err := Parse([]byte(`{}`), mapping)
	if !IsSpecError(err) {
		t.Fatalf("compile error should be a spec error, got %v", err)
	}
	if !strings.Contains(err.Error(), "mapping.items") {
		t.Errorf("error should name the field: %v", err)
	}
}

func TestParseMappedItemsMustBeList(t *testing.T) {
	mapping := &gnmicv1alpha1.MappingSpec{Items: "self.results"}
	if _, _, err := Parse([]byte(`{"results": {"a": 1}}`), mapping); err == nil {
		t.Fatal("items evaluating to a map should fail")
	}
}

func TestPortOf(t *testing.T) {
	cases := []struct {
		in   any
		want int32
		ok   bool
	}{
		{nil, 0, true}, {float64(57400), 57400, true}, {float64(57400.5), 0, false}, {float64(0), 0, true}, {int64(0), 0, true},
		{"57400", 57400, true}, {"", 0, true}, {"x", 0, false}, {"65536", 0, false}, {true, 0, false},
	}
	for _, c := range cases {
		got, ok := portOf(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("portOf(%v) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}
