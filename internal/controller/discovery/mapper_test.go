package discovery

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

func mockDiscoveredTargetList(len int) []core.DiscoveredTarget {
	targets := make([]core.DiscoveredTarget, len)

	if len > 100 {
		len = 100
	}

	for i := range len {
		targets[i] = core.DiscoveredTarget{
			Address: fmt.Sprintf("192.168.1.%d", i+1),
			Name:    fmt.Sprintf("router%d", i+1),
		}
	}

	return targets
}

func mockDiscoveryTarget(opts ...func(*core.DiscoveredTarget)) core.DiscoveredTarget {
	t := core.DiscoveredTarget{
		Name:    "target1",
		Address: "10.0.0.1",
		Labels:  map[string]string{},
	}

	for _, opt := range opts {
		opt(&t)
	}

	return t
}

func withDiscoveredTargetName(name string) func(*core.DiscoveredTarget) {
	return func(t *core.DiscoveredTarget) {
		t.Name = name
	}
}

func withDiscoveredTargetAddress(address string) func(*core.DiscoveredTarget) {
	return func(t *core.DiscoveredTarget) {
		t.Address = address
	}
}

func withDiscoveredTargetLabels(labels map[string]string) func(*core.DiscoveredTarget) {
	return func(t *core.DiscoveredTarget) {
		t.Labels = labels
	}
}

func mockTargetSource(opts ...func(*gnmicv1alpha1.TargetSource)) gnmicv1alpha1.TargetSource {
	ts := gnmicv1alpha1.TargetSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ts1",
			Namespace: "default",
		},
		Spec: gnmicv1alpha1.TargetSourceSpec{
			TargetProfile: "default",
			TargetLabels:  map[string]string{},
		},
	}

	for _, opt := range opts {
		opt(&ts)
	}

	return ts
}

func withTargetSourceName(name string) func(*gnmicv1alpha1.TargetSource) {
	return func(ts *gnmicv1alpha1.TargetSource) {
		ts.ObjectMeta.Name = name
	}
}

func withTargetSourceNamespace(namespace string) func(*gnmicv1alpha1.TargetSource) {
	return func(ts *gnmicv1alpha1.TargetSource) {
		ts.ObjectMeta.Namespace = namespace
	}
}

func withTargetSourceTargetProfile(profile string) func(*gnmicv1alpha1.TargetSource) {
	return func(ts *gnmicv1alpha1.TargetSource) {
		ts.Spec.TargetProfile = profile
	}
}

func withTargetSourceTargetLabels(labels map[string]string) func(*gnmicv1alpha1.TargetSource) {
	return func(ts *gnmicv1alpha1.TargetSource) {
		ts.Spec.TargetLabels = labels
	}
}

func mockGnmicTargetList(len int) []gnmicv1alpha1.Target {
	targets := make([]gnmicv1alpha1.Target, len)

	if len > 100 {
		len = 100
	}

	for i := range len {
		targets[i] = gnmicv1alpha1.Target{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("router%d", i+1),
				Namespace: "default",
			},
			Spec: gnmicv1alpha1.TargetSpec{
				Address: fmt.Sprintf("192.168.1.%d", i+1),
				Profile: "default",
			},
		}
	}

	return targets
}

func TestGenerateEvents_EmptyLists(t *testing.T) {
	events := generateEvents(
		mockGnmicTargetList(0),
		mockDiscoveredTargetList(0),
	)

	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestGenerateEvents_AllDiscoveredTargetsBecomeApplyEvents(t *testing.T) {
	discovered := mockDiscoveredTargetList(5)

	events := generateEvents(
		mockGnmicTargetList(0),
		discovered,
	)

	if len(events) != len(discovered) {
		t.Fatalf("expected %d events, got %d", len(discovered), len(events))
	}

	for _, event := range events {
		if event.Event != core.EventApply {
			t.Fatalf(
				"expected all events to be %s, got %s",
				core.EventApply.String(),
				event.Event.String(),
			)
		}
	}
}

func TestGenerateEvents_AllExistingTargetsBecomeDeleteEvents(t *testing.T) {
	existing := mockGnmicTargetList(5)

	events := generateEvents(
		existing,
		mockDiscoveredTargetList(0),
	)

	if len(events) != len(existing) {
		t.Fatalf("expected %d events, got %d", len(existing), len(events))
	}

	for _, event := range events {
		if event.Event != core.EventDelete {
			t.Fatalf(
				"expected all events to be %s, got %s",
				core.EventDelete.String(),
				event.Event.String(),
			)
		}
	}
}

func TestGenerateEvents_GeneratesDeleteThenApplyEvents(t *testing.T) {
	existing := mockGnmicTargetList(5)
	discovered := mockDiscoveredTargetList(3)

	events := generateEvents(existing, discovered)

	var (
		numDelete int
		numApply  int
		seenApply bool
	)

	for _, event := range events {
		switch event.Event {
		case core.EventDelete:
			if seenApply {
				t.Fatalf("expected delete events before apply events")
			}
			numDelete++

		case core.EventApply:
			seenApply = true
			numApply++
		}
	}

	if numDelete != 2 {
		t.Fatalf("expected 2 delete events, got %d", numDelete)
	}

	if numApply != 3 {
		t.Fatalf("expected 3 apply events, got %d", numApply)
	}
}

func TestGenerateEvents_OnlyApplyEventsAreGeneratedForNewTargets(t *testing.T) {
	existing := mockGnmicTargetList(3)
	discovered := mockDiscoveredTargetList(5)

	events := generateEvents(existing, discovered)

	var (
		numDelete int
		numApply  int
	)

	for _, event := range events {
		switch event.Event {
		case core.EventDelete:
			numDelete++

		case core.EventApply:
			numApply++
		}
	}

	if numDelete != 0 {
		t.Fatalf("expected 0 delete events, got %d", numDelete)
	}

	if numApply != 5 {
		t.Fatalf("expected 5 apply events, got %d", numApply)
	}
}

func TestGenerateEvents_NonOverlappingListsGenerateDeleteAndApplyEvents(t *testing.T) {
	existing := mockGnmicTargetList(5)

	discovered := mockDiscoveredTargetList(10)[5:]

	events := generateEvents(existing, discovered)

	var (
		numDelete int
		numApply  int
		seenApply bool
	)

	for _, event := range events {
		switch event.Event {
		case core.EventDelete:
			if seenApply {
				t.Fatalf("expected delete events before apply events")
			}
			numDelete++

		case core.EventApply:
			seenApply = true
			numApply++
		}
	}

	if numDelete != 5 {
		t.Fatalf("expected 5 delete events, got %d", numDelete)
	}

	if numApply != 5 {
		t.Fatalf("expected 5 apply events, got %d", numApply)
	}
}

func TestGenerateTargetResource_SetsTargetSourceNameLabel(t *testing.T) {
	ts := mockTargetSource()
	d := mockDiscoveryTarget()

	target := generateTargetResource(d, &ts)

	if got := target.Labels[LabelTargetSourceName]; got != ts.Name {
		t.Fatalf(
			"expected %s=%q, got %q",
			LabelTargetSourceName,
			ts.Name,
			got,
		)
	}
}

func TestGenerateTargetResource_CopiesDiscoveredLabels(t *testing.T) {
	d := mockDiscoveryTarget(
		withDiscoveredTargetLabels(map[string]string{
			"discoveredLabel1": "discoveredValue1",
			"discoveredLabel2": "discoveredValue2",
		}),
	)

	ts := mockTargetSource()

	target := generateTargetResource(d, &ts)

	tests := map[string]string{
		"discoveredLabel1": "discoveredValue1",
		"discoveredLabel2": "discoveredValue2",
	}

	for k, want := range tests {
		if got := target.Labels[k]; got != want {
			t.Fatalf("expected label %s=%q, got %q", k, want, got)
		}
	}
}

func TestGenerateTargetResource_CopiesTargetSourceLabels(t *testing.T) {
	ts := mockTargetSource(
		withTargetSourceTargetLabels(map[string]string{
			"targetSourceLabel1": "targetSourceValue1",
			"targetSourceLabel2": "targetSourceValue2",
		}),
	)

	d := mockDiscoveryTarget()

	target := generateTargetResource(d, &ts)

	tests := map[string]string{
		"targetSourceLabel1": "targetSourceValue1",
		"targetSourceLabel2": "targetSourceValue2",
	}

	for k, want := range tests {
		if got := target.Labels[k]; got != want {
			t.Fatalf("expected label %s=%q, got %q", k, want, got)
		}
	}
}

func TestGenerateTargetResource_OverridesReservedTargetSourceNameLabel(t *testing.T) {
	ts := mockTargetSource(
		withTargetSourceTargetLabels(map[string]string{
			LabelTargetSourceName: "wrong-value",
		}),
	)

	d := mockDiscoveryTarget(
		withDiscoveredTargetLabels(map[string]string{
			LabelTargetSourceName: "another-wrong-value",
		}),
	)

	target := generateTargetResource(d, &ts)

	if got := target.Labels[LabelTargetSourceName]; got != ts.Name {
		t.Fatalf(
			"expected reserved label %s=%q, got %q",
			LabelTargetSourceName,
			ts.Name,
			got,
		)
	}
}

func TestGenerateTargetResource_DiscoveredLabelsOverrideTargetSourceLabels(t *testing.T) {
	ts := mockTargetSource(
		withTargetSourceTargetLabels(map[string]string{
			"sharedLabel": "targetSourceValue",
		}),
	)

	d := mockDiscoveryTarget(
		withDiscoveredTargetLabels(map[string]string{
			"sharedLabel": "discoveredValue",
		}),
	)

	target := generateTargetResource(d, &ts)

	if got := target.Labels["sharedLabel"]; got != "discoveredValue" {
		t.Fatalf(
			"expected target source label to override discovered label, got %q",
			got,
		)
	}
}

func TestNormalizeTarget_PrefixesTargetName(t *testing.T) {
	target := mockDiscoveryTarget(
		withDiscoveredTargetName("router1"),
	)

	normalized := normalizeTarget(target, "ts1")

	if got := normalized.Name; got != "ts1-router1" {
		t.Fatalf(
			"expected normalized name %q, got %q",
			"ts1-router1",
			got,
		)
	}
}

func TestNormalizeTarget_PreservesTargetAddress(t *testing.T) {
	target := mockDiscoveryTarget(
		withDiscoveredTargetAddress("192.168.1.10"),
	)

	normalized := normalizeTarget(target, "ts1")

	if got := normalized.Address; got != "192.168.1.10" {
		t.Fatalf(
			"expected address %q, got %q",
			"192.168.1.10",
			got,
		)
	}
}

func TestNormalizeTarget_PreservesTargetLabels(t *testing.T) {
	labels := map[string]string{
		"env":  "prod",
		"role": "leaf",
	}

	target := mockDiscoveryTarget(
		withDiscoveredTargetLabels(labels),
	)

	normalized := normalizeTarget(target, "ts1")

	for k, want := range labels {
		if got := normalized.Labels[k]; got != want {
			t.Fatalf(
				"expected label %s=%q, got %q",
				k,
				want,
				got,
			)
		}
	}
}
