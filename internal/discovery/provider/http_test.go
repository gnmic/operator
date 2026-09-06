package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// fakeResolver serves Secrets and ConfigMaps from maps, returning the same
// spec-wrapped NotFoundError the controller's resolver does.
type fakeResolver struct {
	secrets    map[string]map[string][]byte
	configMaps map[string]map[string][]byte
}

func (f fakeResolver) SecretData(_ context.Context, ns, name string) (map[string][]byte, error) {
	s, ok := f.secrets[name]
	if !ok {
		return nil, Spec(&NotFoundError{Kind: "Secret", Namespace: ns, Name: name})
	}
	return s, nil
}

func (f fakeResolver) SecretKey(ctx context.Context, ns, name, key string) ([]byte, error) {
	s, err := f.SecretData(ctx, ns, name)
	if err != nil {
		return nil, err
	}
	v, ok := s[key]
	if !ok {
		return nil, Spec(&NotFoundError{Kind: "Secret", Namespace: ns, Name: name, Key: key})
	}
	return v, nil
}

func (f fakeResolver) ConfigMapData(_ context.Context, ns, name string) (map[string][]byte, error) {
	c, ok := f.configMaps[name]
	if !ok {
		return nil, Spec(&NotFoundError{Kind: "ConfigMap", Namespace: ns, Name: name})
	}
	return c, nil
}

func (f fakeResolver) ConfigMapKey(ctx context.Context, ns, name, key string) ([]byte, error) {
	c, err := f.ConfigMapData(ctx, ns, name)
	if err != nil {
		return nil, err
	}
	v, ok := c[key]
	if !ok {
		return nil, Spec(&NotFoundError{Kind: "ConfigMap", Namespace: ns, Name: name, Key: key})
	}
	return v, nil
}

func httpRequest(url string, mutate func(*gnmicv1alpha1.HTTPSource), resolver Resolver) Request {
	src := &gnmicv1alpha1.HTTPSource{EndpointSpec: gnmicv1alpha1.EndpointSpec{URL: url}}
	if mutate != nil {
		mutate(src)
	}
	return Request{
		Source:    gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeHTTP, HTTP: src},
		Namespace: "default",
		Objects:   resolver,
	}
}

func TestHTTPFetchNativeList(t *testing.T) {
	var gotAuth, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-Team")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "leaf1", "address": "10.0.0.1"}})
	}))
	defer srv.Close()

	resolver := fakeResolver{secrets: map[string]map[string][]byte{"inv": {"token": []byte("s3cret\n")}}}
	req := httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Headers = map[string]string{"X-Team": "netops"}
		s.Auth = &gnmicv1alpha1.AuthSpec{Token: &gnmicv1alpha1.TokenAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "inv", Key: "token"}}}
	}, resolver)
	res, err := httpProvider{}.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 1 || res.Devices[0].Name != "leaf1" || res.Truncated {
		t.Fatalf("result = %+v", res)
	}
	if gotAuth != "Bearer s3cret" || gotHeader != "netops" {
		t.Errorf("headers: auth=%q team=%q", gotAuth, gotHeader)
	}
}

func TestHTTPFetchBasicAndHeaderAuth(t *testing.T) {
	var user, pass, hdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ = r.BasicAuth()
		hdr = r.Header.Get("X-Consul-Token")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	resolver := fakeResolver{secrets: map[string]map[string][]byte{
		"basic":  {"user": []byte("alice"), "pw": []byte("pw")},
		"consul": {"token": []byte("tok")},
	}}
	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Auth = &gnmicv1alpha1.AuthSpec{Basic: &gnmicv1alpha1.BasicAuth{SecretRef: gnmicv1alpha1.LocalSecretReference{Name: "basic"}, UsernameKey: "user", PasswordKey: "pw"}}
	}, resolver))
	if err != nil || user != "alice" || pass != "pw" {
		t.Fatalf("basic auth: err=%v user=%q pass=%q", err, user, pass)
	}
	_, err = httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Auth = &gnmicv1alpha1.AuthSpec{Header: &gnmicv1alpha1.HeaderAuth{Name: "X-Consul-Token", SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "consul", Key: "token"}}}
	}, resolver))
	if err != nil || hdr != "tok" {
		t.Fatalf("header auth: err=%v hdr=%q", err, hdr)
	}
}

func TestHTTPFetchMissingSecretIsSpecError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer srv.Close()
	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Auth = &gnmicv1alpha1.AuthSpec{Token: &gnmicv1alpha1.TokenAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "missing", Key: "token"}}}
	}, fakeResolver{}))
	if !IsSpecError(err) {
		t.Fatalf("want spec error, got %v", err)
	}
}

func TestHTTPFetchStatusErrorIsRunError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, nil, fakeResolver{}))
	if err == nil || IsSpecError(err) || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want run error naming the status, got %v", err)
	}
}

func TestHTTPFetchPaginationLinkHeader(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Link", `</page2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"name":"a","address":"1.1.1.1"}]`))
		case "/page2":
			_, _ = w.Write([]byte(`[{"name":"b","address":"1.1.1.2"}]`))
		}
	}))
	defer srv.Close()
	res, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL+"/", nil, fakeResolver{}))
	if err != nil || len(res.Devices) != 2 || res.Truncated {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestHTTPFetchPaginationToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page_token") {
		case "":
			_, _ = w.Write([]byte(`{"results":[{"name":"a","address":"1.1.1.1"}],"next":"t2"}`))
		case "t2":
			_, _ = w.Write([]byte(`{"results":[{"name":"b","address":"1.1.1.2"}],"next":null}`))
		default:
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()
	res, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Mapping = &gnmicv1alpha1.MappingSpec{Items: "self.results"}
		s.Pagination = &gnmicv1alpha1.PaginationSpec{NextField: "self.next", RequestParam: "page_token"}
	}, fakeResolver{}))
	if err != nil || len(res.Devices) != 2 || res.Truncated {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestHTTPFetchMaxPagesTruncates(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		fmt.Fprintf(w, `{"results":[{"name":"d%d","address":"1.1.1.%d"}],"next":"%s/?p=%d"}`, page, page, "http://"+r.Host, page)
	}))
	defer srv.Close()
	res, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Mapping = &gnmicv1alpha1.MappingSpec{Items: "self.results"}
		s.Pagination = &gnmicv1alpha1.PaginationSpec{NextField: "self.next", MaxPages: 3}
	}, fakeResolver{}))
	if err != nil || !res.Truncated || len(res.Devices) != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestHTTPFetchPaginationLoopTruncates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+"http://"+r.Host+r.URL.Path+`>; rel="next"`)
		_, _ = w.Write([]byte(`[{"name":"a","address":"1.1.1.1"}]`))
	}))
	defer srv.Close()
	res, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL+"/x", nil, fakeResolver{}))
	if err != nil || !res.Truncated || len(res.Devices) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestHTTPFetchPageFailureIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Link", `</boom>; rel="next"`)
			_, _ = w.Write([]byte(`[{"name":"a","address":"1.1.1.1"}]`))
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL+"/", nil, fakeResolver{}))
	if err == nil {
		t.Fatal("a failed page must fail the run, not return a partial set")
	}
}

func TestHTTPFetchMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{{{`)) }))
	defer srv.Close()
	if _, err := (httpProvider{}).Fetch(context.Background(), httpRequest(srv.URL, nil, fakeResolver{})); err == nil {
		t.Fatal("malformed body should fail")
	}
}

func TestHTTPFetchRespectsDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := httpProvider{}.Fetch(ctx, httpRequest(srv.URL, nil, fakeResolver{}))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("expected a prompt deadline error, got %v after %s", err, time.Since(start))
	}
}

func TestHTTPFetchPOSTBody(t *testing.T) {
	var method, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		body = string(readAllBytes(r.Body))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	_, err := httpProvider{}.Fetch(context.Background(), httpRequest(srv.URL, func(s *gnmicv1alpha1.HTTPSource) {
		s.Method = "POST"
		s.Body = `{"limit": 10}`
	}, fakeResolver{}))
	if err != nil || method != "POST" || body != `{"limit": 10}` {
		t.Fatalf("err=%v method=%s body=%s", err, method, body)
	}
}

func TestHTTPFetchCABundleMustBePEM(t *testing.T) {
	resolver := fakeResolver{configMaps: map[string]map[string][]byte{"ca": {"ca.crt": []byte("not pem")}}}
	_, err := httpProvider{}.Fetch(context.Background(), httpRequest("https://example.invalid", func(s *gnmicv1alpha1.HTTPSource) {
		s.TLS = &gnmicv1alpha1.ClientTLSSpec{CABundleRef: &gnmicv1alpha1.ConfigMapKeyReference{Name: "ca", Key: "ca.crt"}}
	}, resolver))
	if !IsSpecError(err) {
		t.Fatalf("want spec error, got %v", err)
	}
}

func readAllBytes(r io.Reader) []byte {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.Bytes()
}
