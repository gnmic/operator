package http

import (
	"net/http"
	"strings"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func TestPaginationHelpersAndNextURL(t *testing.T) {
	loader := makeLoader(
		gnmicv1alpha1.HTTPConfig{
			Pagination: &gnmicv1alpha1.PaginationSpec{
				NextField:    "self.next",
				RequestParam: "next",
			},
		},
		nil,
	)

	next, err := loader.extractNextPageInfo(map[string]any{"next": "token"})
	if err != nil || next != "token" {
		t.Fatalf("extractNextPageInfo failed: %v", err)
	}

	nextURL, err := loader.buildNextURL("https://example.com/path", "token")
	if err != nil || !strings.Contains(nextURL, "next=token") {
		t.Fatalf("buildNextURL failed: %v, %s", err, nextURL)
	}

	nextURL, err = loader.buildNextURL("https://example.com/path", "https://example.com/other")
	if err != nil || nextURL != "https://example.com/other" {
		t.Fatalf("buildNextURL absolute failed: %v, %s", err, nextURL)
	}
}

func TestPagination_ArrayNoPagination(t *testing.T) {
	raw := []any{
		map[string]any{"name": "a"},
	}

	loader := &Loader{
		spec: gnmicv1alpha1.HTTPConfig{},
	}

	next, err := loader.extractNextPageInfo(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != "" {
		t.Fatalf("expected empty next, got %s", next)
	}
}

func TestPagination_NextURL(t *testing.T) {
	raw := map[string]any{
		"next": "http://example.com/page2",
	}

	loader := &Loader{
		spec: gnmicv1alpha1.HTTPConfig{
			Pagination: &gnmicv1alpha1.PaginationSpec{
				NextField: "self.next",
			},
		},
	}

	next, err := loader.extractNextPageInfo(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if next != "http://example.com/page2" {
		t.Fatalf("unexpected next: %s", next)
	}
}

func TestPagination_Token(t *testing.T) {
	raw := map[string]any{
		"next_page_token": "abc",
	}

	loader := &Loader{
		spec: gnmicv1alpha1.HTTPConfig{
			Pagination: &gnmicv1alpha1.PaginationSpec{
				NextField:    "self.next_page_token",
				RequestParam: "page_token",
			},
		},
	}

	next, err := loader.extractNextPageInfo(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if next != "abc" {
		t.Fatalf("unexpected token: %s", next)
	}
}

func TestPagination_LinkHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Link", `<http://example.com/page2>; rel="next"`)

	next := extractNextFromLinkHeader(headers)

	if next != "http://example.com/page2" {
		t.Fatalf("unexpected next link: %s", next)
	}
}
