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

func TestGenerateEventsEmptyList(t *testing.T) {
	e := mockGnmicTargetList(0)
	d := mockDiscoveredTargetList(0)

	if events := generateEvents(e, d); len(events) != 0 {
		t.Errorf("Wanted 0 events, got: %d", len(events))
	}
}

func TestGenerateEventsEmptyExisting(t *testing.T) {
	len_e := 0
	len_d := 5

	e := mockGnmicTargetList(len_e)
	d := mockDiscoveredTargetList(len_d)

	events := generateEvents(e, d)

	if len(events) != len_d {
		t.Errorf("Wanted %d events, got: %d", len_d, len(events))
	}

	for _, event := range events {
		if event.Event != core.EventApply {
			t.Errorf("Wanted event APPLY, got: %s", event.Event.ToString())
		}
	}
}

func TestGenerateEventsEmptyDiscovery(t *testing.T) {
	len_e := 5
	len_d := 0

	e := mockGnmicTargetList(len_e)
	d := mockDiscoveredTargetList(len_d)

	events := generateEvents(e, d)

	if len(events) != len_e {
		t.Errorf("Wanted %d events, got: %d", len_e, len(events))
	}

	for _, event := range events {
		if event.Event != core.EventDelete {
			t.Errorf("Wanted event APPLY, got: %s", event.Event.ToString())
		}
	}
}

func TestGenerateEventsMoreExisting(t *testing.T) {
	len_e := 5
	len_d := 3

	e := mockGnmicTargetList(len_e)
	d := mockDiscoveredTargetList(len_d)

	events := generateEvents(e, d)

	if len(events) != len_e {
		t.Errorf("Wanted %d events, got: %d", len_e, len(events))
	}

	seenApply := false
	numApply := 0
	numDelete := 0

	for _, event := range events {
		if event.Event == core.EventDelete && seenApply == true {
			t.Error("Want delete events before apply events, got inversed")
		} else if event.Event == core.EventDelete {
			numDelete++
		} else if event.Event == core.EventApply {
			seenApply = true
			numApply++
		}
	}

	if numDelete != len_e-len_d {
		t.Errorf("Wanted %d delete events, got: %d", len_e-len_d, numDelete)
	} else if numApply != len_d {
		t.Errorf("Wanted %d apply events, got: %d", len_d, numApply)
	}
}

func TestGenerateEventsMoreDiscovered(t *testing.T) {
	len_e := 3
	len_d := 5

	e := mockGnmicTargetList(len_e)
	d := mockDiscoveredTargetList(len_d)

	events := generateEvents(e, d)

	if len(events) != len_d {
		t.Errorf("Wanted %d events, got: %d", len_e, len(events))
	}

	seenApply := false
	numApply := 0
	numDelete := 0

	for _, event := range events {
		if event.Event == core.EventDelete && seenApply == true {
			t.Error("Want delete events before apply events, got inversed")
		} else if event.Event == core.EventDelete {
			numDelete++
		} else if event.Event == core.EventApply {
			seenApply = true
			numApply++
		}
	}

	if numDelete != 0 {
		t.Errorf("Wanted %d delete events, got: %d", len_e-len_d, numDelete)
	} else if numApply != len_d {
		t.Errorf("Wanted %d apply events, got: %d", len_d, numApply)
	}
}

func TestGenerateEventsNonOverlappingLists(t *testing.T) {
	len_e := 5
	len_d := 5

	e := mockGnmicTargetList(len_e)
	d := mockDiscoveredTargetList(len_e + len_d)[len_e:]

	events := generateEvents(e, d)

	if len(events) != len_e+len_d {
		t.Errorf("Wanted %d events, got: %d", len_e, len(events))
	}

	seenApply := false
	numApply := 0
	numDelete := 0

	for _, event := range events {
		if event.Event == core.EventDelete && seenApply == true {
			t.Error("Want delete events before apply events, got inversed")
		} else if event.Event == core.EventDelete {
			numDelete++
		} else if event.Event == core.EventApply {
			seenApply = true
			numApply++
		}
	}

	if numDelete != len_e {
		t.Errorf("Wanted %d delete events, got: %d", len_e-len_d, numDelete)
	} else if numApply != len_d {
		t.Errorf("Wanted %d apply events, got: %d", len_d, numApply)
	}
}
