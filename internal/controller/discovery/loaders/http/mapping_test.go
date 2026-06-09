package http

import (
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
)

func TestExtractTargetsAndMapping(t *testing.T) {
	tests := []struct {
		name     string
		config   gnmicv1alpha1.HTTPConfig
		raw      any
		validate func(t *testing.T, targets []core.DiscoveredTarget)
	}{
		{
			name:   "direct mapping all fields",
			config: gnmicv1alpha1.HTTPConfig{},
			raw:    []any{map[string]any{"name": "t1", "address": "1.1.1.1", "port": "9000", "labels": map[string]any{"env": "prod", "region": "us-east"}, "targetProfile": "edge-profile"}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("direct mapping: expected 1 target, got %d", len(targets))
				}
				tgt := targets[0]
				if tgt.Name != "t1" {
					t.Fatalf("direct mapping Name failed: got %q", tgt.Name)
				}
				if tgt.Address != "1.1.1.1" {
					t.Fatalf("direct mapping Address failed: got %q", tgt.Address)
				}
				if tgt.Port != 9000 {
					t.Fatalf("direct mapping Port failed: got %d", tgt.Port)
				}
				if tgt.Labels["env"] != "prod" || tgt.Labels["region"] != "us-east" {
					t.Fatalf("direct mapping Labels failed: %#v", tgt.Labels)
				}
				if tgt.TargetProfile != "edge-profile" {
					t.Fatalf("direct mapping TargetProfile failed: got %q", tgt.TargetProfile)
				}
			},
		},
		{
			name:   "CEL TargetsField extraction",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{TargetsField: "self.results"}},
			raw:    map[string]any{"results": []any{map[string]any{"name": "t1", "address": "1.1.1.1", "port": float64(22)}}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("TargetsField extraction failed: got %d targets", len(targets))
				}
			},
		},
		{
			name:   "CEL Name mapping",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{Name: "item.hostname"}},
			raw:    []any{map[string]any{"hostname": "host-1", "address": "10.0.0.1", "port": float64(830)}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("Name mapping: expected 1 target, got %d", len(targets))
				}
				if targets[0].Name != "host-1" {
					t.Fatalf("Name mapping failed: got %q", targets[0].Name)
				}
			},
		},
		{
			name:   "CEL Address mapping",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{Address: "item.ip"}},
			raw:    []any{map[string]any{"name": "t1", "ip": "192.168.1.1", "port": float64(830)}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("Address mapping: expected 1 target, got %d", len(targets))
				}
				if targets[0].Address != "192.168.1.1" {
					t.Fatalf("Address mapping failed: got %q", targets[0].Address)
				}
			},
		},
		{
			name:   "CEL Port mapping",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{Port: "item.mgmt_port"}},
			raw:    []any{map[string]any{"name": "t1", "address": "10.0.0.1", "mgmt_port": float64(9000)}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("Port mapping: expected 1 target, got %d", len(targets))
				}
				if targets[0].Port != 9000 {
					t.Fatalf("Port mapping failed: got %d", targets[0].Port)
				}
			},
		},
		{
			name:   "CEL Labels mapping",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{Labels: `{"env": item.environment, "type": item.device_type}`}},
			raw:    []any{map[string]any{"name": "t1", "address": "10.0.0.1", "port": float64(830), "environment": "prod", "device_type": "router"}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("Labels mapping: expected 1 target, got %d", len(targets))
				}
				if targets[0].Labels["env"] != "prod" || targets[0].Labels["type"] != "router" {
					t.Fatalf("Labels mapping failed: %#v", targets[0].Labels)
				}
			},
		},
		{
			name:   "CEL TargetProfile mapping",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{TargetProfile: `item.type == "edge" ? "edge-profile" : "default"`}},
			raw:    []any{map[string]any{"name": "t1", "address": "10.0.0.1", "port": float64(830), "type": "edge"}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("TargetProfile mapping: expected 1 target, got %d", len(targets))
				}
				if targets[0].TargetProfile != "edge-profile" {
					t.Fatalf("TargetProfile mapping failed: got %q", targets[0].TargetProfile)
				}
			},
		},
		{
			name:   "CEL all mapping options combined",
			config: gnmicv1alpha1.HTTPConfig{ResponseMapping: &gnmicv1alpha1.ResponseMappingSpec{TargetsField: "self.results", Name: "item.hostname", Address: "item.ip", Port: "item.port", Labels: `{"env": item.env}`, TargetProfile: `item.type == "edge" ? "edge-profile" : "default"`}},
			raw:    map[string]any{"results": []any{map[string]any{"hostname": "host-1", "ip": "10.0.0.1", "port": float64(830), "env": "prod", "type": "edge"}}},
			validate: func(t *testing.T, targets []core.DiscoveredTarget) {
				if len(targets) != 1 {
					t.Fatalf("combined mapping: expected 1 target, got %d", len(targets))
				}
				tgt := targets[0]
				if tgt.Name != "host-1" || tgt.Address != "10.0.0.1" || tgt.Port != 830 || tgt.Labels["env"] != "prod" || tgt.TargetProfile != "edge-profile" {
					t.Fatalf("combined mapping failed: %#v", tgt)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := makeLoader(tt.config, nil)
			targets, err := loader.extractTargetsFromResponse(tt.raw, logr.Discard())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.validate(t, targets)
		})
	}
}

func TestMapItemsToTargetsSkipsInvalidItems(t *testing.T) {
	loader := makeLoader(gnmicv1alpha1.HTTPConfig{}, nil)
	tgts, err := loader.mapItemsToTargets([]any{"not-a-map", map[string]any{"name": "n", "address": "a"}}, nil, logr.Discard())
	if err != nil {
		t.Fatalf("mapItemsToTargets failed: %v", err)
	}
	if len(tgts) != 1 || tgts[0].Name != "n" {
		t.Fatalf("unexpected targets: %#v", tgts)
	}
}
