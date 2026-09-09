package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/cel-go/cel"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

const (
	defaultMaxPages = 100
	// maxBodyBytes bounds one page. A device inventory that does not fit is a
	// misconfigured endpoint, not a use case.
	maxBodyBytes = 64 << 20
)

// httpProvider is the general-purpose provider and the escape hatch for
// everything not built in.
type httpProvider struct{}

func init() { Register(httpProvider{}) }

func (httpProvider) Type() gnmicv1alpha1.SourceType { return gnmicv1alpha1.SourceTypeHTTP }

func (p httpProvider) Fetch(ctx context.Context, req Request) (Result, error) {
	src := req.Source.HTTP
	if src == nil {
		return Result{}, Specf("source.http is not set")
	}
	client, err := newHTTPClient(ctx, req, &src.EndpointSpec)
	if err != nil {
		return Result{}, err
	}
	auth, err := newAuthenticator(ctx, req, src.Auth)
	if err != nil {
		return Result{}, err
	}

	maxPages := int32(defaultMaxPages)
	if src.Pagination != nil && src.Pagination.MaxPages > 0 {
		maxPages = src.Pagination.MaxPages
	}
	failOnError := src.Mapping != nil && src.Mapping.OnError == gnmicv1alpha1.MappingErrorFail

	// Compile every expression once per run, not once per page. Both are spec
	// errors, and surface here before a single request goes out.
	extractor, err := NewExtractor(src.Mapping)
	if err != nil {
		return Result{}, err
	}
	var nextProg cel.Program
	if src.Pagination != nil && src.Pagination.NextField != "" {
		nextProg, err = CompileExpression(src.Pagination.NextField)
		if err != nil {
			return Result{}, Specf("pagination.nextField: %w", err)
		}
	}

	var result Result
	seen := make(map[string]struct{})
	pageURL := src.URL
	for page := int32(0); ; page++ {
		if page >= maxPages {
			result.Truncated = true
			break
		}
		if _, dup := seen[pageURL]; dup {
			// The API handed back a page it already served. Following it
			// would loop forever; what has been read so far is incomplete.
			result.Truncated = true
			break
		}
		seen[pageURL] = struct{}{}

		body, headers, err := fetchPage(ctx, client, src, auth, pageURL)
		if err != nil {
			return Result{}, err
		}
		raw, err := Decode(body)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", pageURL, err)
		}
		devices, failures, err := extractor.Extract(raw)
		if err != nil {
			return Result{}, err
		}
		if failOnError && len(failures) > 0 {
			return Result{}, fmt.Errorf("%s: item %s: %s", pageURL, failures[0].Name, failures[0].Reason)
		}
		result.Devices = append(result.Devices, devices...)
		result.Invalid = append(result.Invalid, failures...)

		next, err := nextPage(src.Pagination, nextProg, raw, headers, src.URL, pageURL)
		if err != nil {
			return Result{}, err
		}
		if next == "" {
			break
		}
		pageURL = next
	}
	return result, nil
}

// newHTTPClient builds the client for one run. The deadline comes from the
// context the controller passes, bounded by spec.timeout, so the client itself
// carries no timeout.
func newHTTPClient(ctx context.Context, req Request, ep *gnmicv1alpha1.EndpointSpec) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if ep.TLS != nil {
		cfg, err := tlsConfig(ctx, req, ep.TLS)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = cfg
	}
	return &http.Client{Transport: transport}, nil
}

// tlsConfig resolves the CA bundle and client certificate references. Both
// are spec errors when missing or unparsable: nothing about a retry changes
// what is in the referenced object.
func tlsConfig(ctx context.Context, req Request, spec *gnmicv1alpha1.ClientTLSSpec) (*tls.Config, error) {
	cfg := &tls.Config{
		InsecureSkipVerify: spec.InsecureSkipVerify, //nolint:gosec // explicit user choice
		MinVersion:         tls.VersionTLS12,
	}
	if spec.CABundleRef != nil {
		pem, err := req.Objects.ConfigMapKey(ctx, req.Namespace, spec.CABundleRef.Name, spec.CABundleRef.Key)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, Specf("tls.caBundleRef: %s/%s key %q holds no PEM certificates", req.Namespace, spec.CABundleRef.Name, spec.CABundleRef.Key)
		}
		cfg.RootCAs = pool
	}
	if spec.ClientCertRef != nil {
		data, err := req.Objects.SecretData(ctx, req.Namespace, spec.ClientCertRef.Name)
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair(data["tls.crt"], data["tls.key"])
		if err != nil {
			return nil, Specf("tls.clientCertRef: %s/%s is not a usable kubernetes.io/tls Secret: %w", req.Namespace, spec.ClientCertRef.Name, err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// authenticator sets credentials on a request. Credentials are resolved once
// per run, not once per page.
type authenticator func(*http.Request)

func newAuthenticator(ctx context.Context, req Request, auth *gnmicv1alpha1.AuthSpec) (authenticator, error) {
	if auth == nil {
		return func(*http.Request) {}, nil
	}
	switch {
	case auth.Basic != nil:
		data, err := req.Objects.SecretData(ctx, req.Namespace, auth.Basic.SecretRef.Name)
		if err != nil {
			return nil, err
		}
		userKey, passKey := auth.Basic.UsernameKey, auth.Basic.PasswordKey
		if userKey == "" {
			userKey = "username"
		}
		if passKey == "" {
			passKey = "password"
		}
		user, okU := data[userKey]
		pass, okP := data[passKey]
		if !okU || !okP {
			return nil, Specf("auth.basic: Secret %s/%s lacks key %q or %q", req.Namespace, auth.Basic.SecretRef.Name, userKey, passKey)
		}
		return func(r *http.Request) { r.SetBasicAuth(string(user), string(pass)) }, nil
	case auth.Token != nil:
		token, err := req.Objects.SecretKey(ctx, req.Namespace, auth.Token.SecretRef.Name, auth.Token.SecretRef.Key)
		if err != nil {
			return nil, err
		}
		scheme := auth.Token.Scheme
		if scheme == "" {
			scheme = "Bearer"
		}
		value := scheme + " " + strings.TrimSpace(string(token))
		return func(r *http.Request) { r.Header.Set("Authorization", value) }, nil
	case auth.Header != nil:
		value, err := req.Objects.SecretKey(ctx, req.Namespace, auth.Header.SecretRef.Name, auth.Header.SecretRef.Key)
		if err != nil {
			return nil, err
		}
		name, v := auth.Header.Name, strings.TrimSpace(string(value))
		return func(r *http.Request) { r.Header.Set(name, v) }, nil
	}
	return nil, Specf("auth: no method set")
}

// fetchPage performs one request and returns the body and headers. Anything
// but a 2xx is a run failure: the source may well answer next time.
func fetchPage(ctx context.Context, client *http.Client, src *gnmicv1alpha1.HTTPSource, auth authenticator, pageURL string) ([]byte, http.Header, error) {
	method := src.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if method == http.MethodPost && src.Body != "" {
		body = strings.NewReader(src.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, pageURL, body)
	if err != nil {
		return nil, nil, Specf("url: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json, application/yaml;q=0.9, */*;q=0.1")
	for k, v := range src.Headers {
		httpReq.Header.Set(k, v)
	}
	auth(httpReq)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", pageURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: reading body: %w", pageURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("%s: unexpected HTTP status %d", pageURL, resp.StatusCode)
	}
	if len(data) > maxBodyBytes {
		return nil, nil, fmt.Errorf("%s: response larger than %d bytes", pageURL, maxBodyBytes)
	}
	return data, resp.Header, nil
}

// nextPage returns the URL of the next page, or "" when there is none, and
// refuses one that leaves the origin of the configured source URL.
//
// Every page is sent with the source's credentials attached, and nothing here
// goes through an HTTP redirect -- so the protection Go's http.Client gives
// (dropping Authorization on a cross-host redirect) never applies. Without this
// check a hostile or compromised inventory endpoint could answer with
// `Link: <https://elsewhere/>; rel="next"` and be handed the Secret.
func nextPage(spec *gnmicv1alpha1.PaginationSpec, nextProg cel.Program, raw any, headers http.Header, base, current string) (string, error) {
	next, err := nextPageCandidate(spec, nextProg, raw, headers, current)
	if err != nil || next == "" {
		return next, err
	}
	if err := sameOrigin(base, next); err != nil {
		return "", err
	}
	return next, nil
}

// sameOrigin reports whether next has the scheme and host of base. Host
// comparison is case-insensitive and treats an explicit default port as equal
// to none, so http://inv and http://inv:80 are the same place.
func sameOrigin(base, next string) error {
	b, err := url.Parse(base)
	if err != nil {
		return Specf("url: %w", err)
	}
	n, err := url.Parse(next)
	if err != nil {
		return fmt.Errorf("pagination: invalid next page URL %q: %w", next, err)
	}
	if !strings.EqualFold(b.Scheme, n.Scheme) || !strings.EqualFold(hostWithPort(b), hostWithPort(n)) {
		return fmt.Errorf("pagination: next page %q is not on the source origin %s://%s; pagination never leaves the configured host, so credentials are not sent elsewhere",
			next, b.Scheme, b.Host)
	}
	return nil
}

// hostWithPort returns host:port, filling in the scheme's default port when
// the URL carries none.
func hostWithPort(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return u.Hostname() + ":80"
	case "https":
		return u.Hostname() + ":443"
	}
	return u.Host
}

// nextPageCandidate is the next page as the server describes it, before the
// origin check. A Link header with rel="next" is honoured first; then the
// NextField expression (compiled by the caller, once per run), which may yield
// a full URL or a token for RequestParam.
func nextPageCandidate(spec *gnmicv1alpha1.PaginationSpec, nextProg cel.Program, raw any, headers http.Header, current string) (string, error) {
	if next := nextFromLinkHeader(headers); next != "" {
		return resolveURL(current, next)
	}
	if spec == nil || spec.NextField == "" || nextProg == nil {
		return "", nil
	}
	out, err := evalExpression(nextProg, raw, nil)
	if err != nil {
		return "", fmt.Errorf("pagination.nextField: %w", err)
	}
	if out == nil {
		return "", nil
	}
	next, ok := out.(string)
	if !ok {
		return "", fmt.Errorf("pagination.nextField: must evaluate to a string or null, got %T", out)
	}
	if next == "" {
		return "", nil
	}
	if u, err := url.Parse(next); err == nil && u.Scheme != "" {
		return next, nil
	}
	if spec.RequestParam == "" {
		return "", Specf("pagination.requestParam is required when nextField returns a token")
	}
	u, err := url.Parse(current)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(spec.RequestParam, next)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// nextFromLinkHeader parses `<url>; rel="next"` out of a Link header.
func nextFromLinkHeader(h http.Header) string {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		if !strings.Contains(part, `rel="next"`) && !strings.Contains(part, "rel=next") {
			continue
		}
		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start >= 0 && end > start {
			return part[start+1 : end]
		}
	}
	return ""
}

// resolveURL makes a Link header target absolute against the current page.
func resolveURL(current, next string) (string, error) {
	base, err := url.Parse(current)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("pagination: invalid Link header %q: %w", next, err)
	}
	return base.ResolveReference(ref).String(), nil
}
