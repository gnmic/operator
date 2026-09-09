package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// #34: expressions are compiled once per run, before any request. A compile
// error is therefore a spec error raised with zero requests made, where it used
// to surface only after page 1 had been fetched (nextField) or per page
// (mapping).
func TestHTTPFetchCompileErrorsAreSpecErrorsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"results":[{"name":"a","address":"1.1.1.1"}],"next":null}`))
	}))
	defer srv.Close()

	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Mapping = &gnmicv1alpha1.MappingSpec{Items: "self.results"}
		s.Pagination = &gnmicv1alpha1.PaginationSpec{NextField: "self.next ==== broken"}
	}, fakeResolver{}))
	if err == nil || !IsSpecError(err) {
		t.Fatalf("bad nextField: err=%v, want a spec error", err)
	}
	_, err = httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Mapping = &gnmicv1alpha1.MappingSpec{Items: "self.results", Address: "item.((("}
	}, fakeResolver{}))
	if err == nil || !IsSpecError(err) {
		t.Fatalf("bad mapping: err=%v, want a spec error", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("%d request(s) were made for a source that cannot be evaluated", hits.Load())
	}
}

// The compiled Extractor reads several documents with one compile and agrees
// with the one-shot Extract.
func TestExtractorReusesCompiledMapping(t *testing.T) {
	mapping := &gnmicv1alpha1.MappingSpec{Items: "self.results", Name: "item.hostname", Address: "item.ip"}
	e, err := NewExtractor(mapping)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{
		`{"results":[{"hostname":"a","ip":"1.1.1.1"}]}`,
		`{"results":[{"hostname":"b","ip":"1.1.1.2"},{"hostname":"c","ip":"1.1.1.3"}]}`,
	} {
		raw, err := Decode([]byte(page))
		if err != nil {
			t.Fatal(err)
		}
		fromExtractor, _, err := e.Extract(raw)
		if err != nil {
			t.Fatal(err)
		}
		oneShot, _, err := Extract(raw, mapping)
		if err != nil {
			t.Fatal(err)
		}
		if len(fromExtractor) != len(oneShot) || fromExtractor[0].Name != oneShot[0].Name || fromExtractor[0].Address != oneShot[0].Address {
			t.Fatalf("extractor %+v != one-shot %+v", fromExtractor, oneShot)
		}
	}
	if _, err := NewExtractor(&gnmicv1alpha1.MappingSpec{Items: "self.("}); err == nil || !IsSpecError(err) {
		t.Fatalf("compile error should be a spec error, got %v", err)
	}
}
