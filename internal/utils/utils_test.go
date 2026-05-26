package utils

import "testing"

func TestSplitNN(t *testing.T) {
	tests := []struct {
		name      string
		nn        string
		wantNS    string
		wantName  string
	}{
		{"namespaced", "default/my-target", "default", "my-target"},
		{"cluster scoped", "my-target", "", "my-target"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, name := SplitNN(tt.nn)
			if ns != tt.wantNS || name != tt.wantName {
				t.Fatalf("SplitNN(%q) = (%q, %q), want (%q, %q)", tt.nn, ns, name, tt.wantNS, tt.wantName)
			}
		})
	}
}
