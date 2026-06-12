package discovery

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

type fakeStatusUpdater struct {
}

func (f fakeStatusUpdater) UpdateStatus(ctx context.Context, update core.StatusUpdate) error {
	return nil
}

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
