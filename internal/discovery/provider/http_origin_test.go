package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// elsewhere is a second server standing in for a host the inventory endpoint
// points pagination at. It must never be contacted, and certainly never with the
// source's credentials.
func elsewhere(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	var hits atomic.Int32
	var auth atomic.Value
	auth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[{"name":"stolen","address":"9.9.9.9"}]`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &auth
}

func tokenAuth(s *gnmicv1alpha1.HTTPSource) {
	s.Auth = &gnmicv1alpha1.AuthSpec{Token: &gnmicv1alpha1.TokenAuth{
		SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "tok", Key: "token"},
	}}
}

var tokenResolver = fakeResolver{secrets: map[string]map[string][]byte{"tok": {"token": []byte("s3cr3t")}}}

func TestHTTPFetchRejectsCrossOriginLinkHeader(t *testing.T) {
	other, hits, auth := elsewhere(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+other.URL+`/page2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"name":"a","address":"1.1.1.1"}]`))
	}))
	defer srv.Close()

	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL+"/", tokenAuth, tokenResolver))
	if err == nil || !strings.Contains(err.Error(), "not on the source origin") {
		t.Fatalf("expected an origin error, got %v", err)
	}
	if IsSpecError(err) {
		t.Error("a hostile next link is a run failure, not a spec error: the spec is fine")
	}
	if hits.Load() != 0 {
		t.Fatalf("the other host was contacted %d time(s); Authorization sent: %q", hits.Load(), auth.Load())
	}
}

func TestHTTPFetchRejectsCrossOriginNextField(t *testing.T) {
	other, hits, auth := elsewhere(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"name":"a","address":"1.1.1.1"}],"next":"` + other.URL + `/page2"}`))
	}))
	defer srv.Close()

	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		tokenAuth(s)
		s.Mapping = &gnmicv1alpha1.MappingSpec{Items: "self.results"}
		s.Pagination = &gnmicv1alpha1.PaginationSpec{NextField: "self.next"}
	}, tokenResolver))
	if err == nil || !strings.Contains(err.Error(), "not on the source origin") {
		t.Fatalf("expected an origin error, got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("the other host was contacted %d time(s); Authorization sent: %q", hits.Load(), auth.Load())
	}
}

// Same host, different scheme is a different origin: a downgrade to http would
// put the token on the wire in clear.
func TestHTTPFetchRejectsSchemeDowngrade(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<http://`+r.Host+`/page2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"name":"a","address":"1.1.1.1"}]`))
	}))
	defer srv.Close()

	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL+"/", func(s *gnmicv1alpha1.HTTPSource) {
		s.TLS = &gnmicv1alpha1.ClientTLSSpec{InsecureSkipVerify: true}
	}, fakeResolver{}))
	if err == nil || !strings.Contains(err.Error(), "not on the source origin") {
		t.Fatalf("expected an origin error, got %v", err)
	}
}

// An absolute next link back to the same origin -- what NetBox and most REST
// APIs return -- keeps working, with the credentials still attached.
func TestHTTPFetchFollowsAbsoluteSameOriginNext(t *testing.T) {
	var page2Auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Link", `<http://`+r.Host+`/page2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"name":"a","address":"1.1.1.1"}]`))
		case "/page2":
			page2Auth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`[{"name":"b","address":"1.1.1.2"}]`))
		}
	}))
	defer srv.Close()

	res, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL+"/", tokenAuth, tokenResolver))
	if err != nil || len(res.Devices) != 2 || res.Truncated {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if page2Auth != "Bearer s3cr3t" {
		t.Fatalf("page 2 Authorization = %q", page2Auth)
	}
}

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		base, next string
		ok         bool
	}{
		{"http://inv.example/api", "http://inv.example/api?page=2", true},
		{"http://inv.example/api", "http://inv.example:80/api?page=2", true},
		{"https://inv.example/api", "https://inv.example:443/api?page=2", true},
		{"https://INV.example/api", "https://inv.EXAMPLE/other", true},
		{"http://inv.example:8080/api", "http://inv.example:8080/x", true},
		{"http://inv.example:8080/api", "http://inv.example/x", false},
		{"http://inv.example/api", "http://inv.example:8080/x", false},
		{"https://inv.example/api", "http://inv.example/api", false},
		{"http://inv.example/api", "http://attacker.example/", false},
		{"http://inv.example/api", "http://inv.example.evil/", false},
		{"http://inv.example/api", "http://user@inv.example/", true}, // userinfo is not part of the origin
	}
	for _, c := range cases {
		err := sameOrigin(c.base, c.next)
		if (err == nil) != c.ok {
			t.Errorf("sameOrigin(%q, %q) = %v, want ok=%v", c.base, c.next, err, c.ok)
		}
	}
}
