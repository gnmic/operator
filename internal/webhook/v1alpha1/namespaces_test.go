package v1alpha1

import (
	"strings"
	"testing"
)

func TestUnwatchedNamespaceWarning(t *testing.T) {
	t.Cleanup(func() { SetWatchedNamespaces(nil) })

	// Default: all namespaces watched, so never warn.
	SetWatchedNamespaces(nil)
	if w := unwatchedNamespaceWarning("Cluster", "anything"); w != nil {
		t.Fatalf("watching all namespaces should not warn, got %v", w)
	}
	SetWatchedNamespaces([]string{})
	if w := unwatchedNamespaceWarning("Cluster", "anything"); w != nil {
		t.Fatalf("empty slice means all namespaces, got %v", w)
	}

	SetWatchedNamespaces([]string{"beta", "alpha"})

	if w := unwatchedNamespaceWarning("Cluster", "alpha"); w != nil {
		t.Fatalf("watched namespace should not warn, got %v", w)
	}

	w := unwatchedNamespaceWarning("Pipeline", "gamma")
	if len(w) != 1 {
		t.Fatalf("expected one warning, got %v", w)
	}
	// The message must name the offending namespace, the kind, and the watched set —
	// it is the only signal the user gets that the resource will be ignored.
	for _, want := range []string{`"gamma"`, "Pipeline", "alpha, beta", "never reconciled"} {
		if !strings.Contains(w[0], want) {
			t.Errorf("warning %q missing %q", w[0], want)
		}
	}
}
