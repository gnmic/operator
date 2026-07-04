package discovery

import (
	"testing"

	"github.com/gnmic/operator/internal/controller/discovery/core"
)

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
