package main

import (
	"reflect"
	"testing"
)

func TestParseWatchNamespaces(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  []string
	}{
		// nil means "watch everything" — the flag being unset must not silently
		// scope the cache to nothing.
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"commas only", ",,", nil},
		{"single", "alpha", []string{"alpha"}},
		{"multiple", "alpha,beta", []string{"alpha", "beta"}},
		{"trims spaces", " alpha , beta ", []string{"alpha", "beta"}},
		{"drops empties", "alpha,,beta,", []string{"alpha", "beta"}},
		{"dedupes, keeps order", "beta,alpha,beta", []string{"beta", "alpha"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseWatchNamespaces(tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseWatchNamespaces(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
