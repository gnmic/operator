package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	loaderUtils "github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
)

// Loader implements the HTTP pull discovery mechanism
// It periodically polls an HTTP endpoint, extracts targets from the response,
// and emits discovery snapshots downstream
type Loader struct {
	loaderCfg core.CommonLoaderConfig
	spec      gnmicv1alpha1.HTTPConfig
}

// New creates a new HTTP loader instance with the provided configuration.
// The loader is stateless apart from its config and spec
func New(cfg core.CommonLoaderConfig, httpConfig gnmicv1alpha1.HTTPConfig) core.Loader {
	return &Loader{loaderCfg: cfg, spec: httpConfig}
}

// Name returns the loader's name, used for logging and metrics
func (l *Loader) Name() string {
	return "http"
}

// Run starts the HTTP discovery loop
// It performs an immediate fetch and then continues polling at a fixed interval
func (l *Loader) Run(ctx context.Context, out chan<- []core.DiscoveryMessage) error {
	if l.spec.URL == "" {
		return nil
	}

	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", l.loaderCfg.TargetsourceNN,
	)

	logger.Info(
		"HTTP loader started",
		"targetsource", l.loaderCfg.TargetsourceNN.Name,
		"namespace", l.loaderCfg.TargetsourceNN.Namespace,
	)

	logger.Info("HTTP loader started")

	client, err := l.buildHTTPClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to build HTTP client: %w", err)
	}
	if l.spec.Interval == nil {
		return fmt.Errorf("interval must be configured")
	}
	interval := l.spec.Interval.Duration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info(
		"HTTP polling discovery started",
		"interval", interval.String(),
		"url", l.spec.URL,
	)

	// helper function to fetch targets and emit discovery messages
	fetchAndEmit := func() {
		// Set TargetSource conditions to "Reconciling"
		l.loaderCfg.Updater.UpdateStatus(
			ctx,
			core.StatusUpdate{
				Conditions: []metav1.Condition{
					{
						Type:    core.ConditionTypeReconciling,
						Status:  metav1.ConditionStatus("True"),
						Reason:  string(core.ReasonSyncStarted),
						Message: "Started fetching target source",
					},
				},
			},
		)

		// Fetch targets from HTTP endpoint
		targets, err := l.fetchTargetsFromHTTPEndpoint(ctx, client, logger)
		if err != nil {
			logger.Error(
				err,
				"Failed to fetch targets from HTTP endpoint",
				"url", l.spec.URL,
			)

			// Set TargetSource conditions to "Stalled" if endpoint is not available
			l.loaderCfg.Updater.UpdateStatus(
				ctx,
				core.StatusUpdate{
					Conditions: []metav1.Condition{
						{
							Type:    core.ConditionTypeStalled,
							Status:  metav1.ConditionStatus("True"),
							Reason:  string(core.ReasonSyncFailed),
							Message: "HTTP endpoint not available",
						},
					},
				},
			)
			return
		}

		// Emit discovery snapshot downstream
		snapshotID := fmt.Sprintf("%s-%s-%s", l.loaderCfg.TargetsourceNN.Namespace, l.loaderCfg.TargetsourceNN.Name, uuid.NewString())
		if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.loaderCfg.ChunkSize); err != nil {
			logger.Error(
				err,
				"Failed to send discovery snapshot",
				"snapshotID", snapshotID,
				"targets", len(targets),
			)
			return
		}

		logger.Info(
			"Discovery snapshot sent",
			"snapshotID", snapshotID,
			"targets", len(targets),
		)
	}

	// Immediate fetch on startup
	fetchAndEmit()

	// Periodic fetch
	for {
		select {
		case <-ctx.Done():
			logger.Info("HTTP loader stopped")
			return nil

		case <-ticker.C:
			fetchAndEmit()
		}
	}
}

// buildHTTPClient constructs an HTTP client with optional configuration
func (l *Loader) buildHTTPClient(ctx context.Context) (*http.Client, error) {
	if l.spec.Timeout == nil {
		return nil, fmt.Errorf("timeout must be configured")
	}
	timeout := l.spec.Timeout.Duration
	transport := &http.Transport{}
	// If TLS is configured, build TLS config (may include CA bundle).
	if l.spec.TLS != nil {
		tlsConfig, err := l.buildTLSConfig(ctx)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}

	// Build the HTTP client with the specified timeout and TLS config
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	return client, nil
}

// buildTLSConfig constructs a tls.Config according to the loader spec,
// fetching and parsing a CA bundle if requested.
func (l *Loader) buildTLSConfig(ctx context.Context) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: l.spec.TLS.InsecureSkipVerify,
	}

	if l.spec.TLS.CABundleRef == nil {
		return tlsConfig, nil
	}

	if l.loaderCfg.ResourceFetcher == nil {
		return nil, fmt.Errorf("resource fetcher is not configured")
	}

	ref := l.spec.TLS.CABundleRef
	if ref.Name == "" || ref.Key == "" {
		return nil, fmt.Errorf("CABundleRef must specify both name and key")
	}

	caPEM, err := l.loaderCfg.ResourceFetcher.GetConfigMapKey(ctx, l.loaderCfg.TargetsourceNN.Namespace, l.spec.TLS.CABundleRef)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CA bundle from config map ref: %w", err)
	}

	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM([]byte(caPEM)); !ok {
		return nil, fmt.Errorf("failed to parse CA bundle PEM")
	}
	tlsConfig.RootCAs = certPool

	return tlsConfig, nil
}

// fetchTargetsFromHTTPEndpoint retrieves targets from the configured HTTP endpoint
func (l *Loader) fetchTargetsFromHTTPEndpoint(
	ctx context.Context,
	client *http.Client,
	logger logr.Logger,
) ([]core.DiscoveredTarget, error) {
	var allTargets []core.DiscoveredTarget
	currentURL := l.spec.URL

	seen := make(map[string]struct{})

	for {
		if _, exists := seen[currentURL]; exists {
			logger.Error(fmt.Errorf("pagination loop detected"), "stopping pagination", "url", currentURL)
			break
		}
		seen[currentURL] = struct{}{}

		raw, headers, err := l.fetchPage(ctx, client, currentURL)
		if err != nil {
			return allTargets, err // do not silently drop pages
		}

		// Extract targets
		if targets, err := l.extractTargetsFromResponse(raw, logger); err != nil {
			logger.Error(err, "Failed to extract targets", "url", currentURL)
		} else {
			allTargets = append(allTargets, targets...)
		}

		// Pagination: next page
		nextURL, stop := l.getNextURL(raw, headers, currentURL, logger)
		if stop {
			break
		}
		currentURL = nextURL
	}

	return allTargets, nil
}

// fetchPage performs an HTTP GET request to the specified URL and decodes the JSON response
// and returns the raw response
func (l *Loader) fetchPage(
	ctx context.Context,
	client *http.Client,
	url string,
) (any, http.Header, error) {

	method := l.spec.Method
	if method == "" {
		return nil, nil, fmt.Errorf("method must be configured")
	}

	// Build request body (only for POST)
	var bodyReader *bytes.Reader
	if method == http.MethodPost && l.spec.Body != "" {
		bodyReader = bytes.NewReader([]byte(l.spec.Body))
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	// Build HTTP request
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("creating HTTP request failed: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	// Apply user-defined headers
	for key, val := range l.spec.Headers {
		req.Header.Set(key, val)
	}

	if err := l.applyAuthentication(req); err != nil {
		return nil, nil, err
	}

	// Execute HTTP request
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.Header, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	// Decode HTTP response
	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, resp.Header, err
	}

	return raw, resp.Header, nil
}

// extractTargetsFromResponse extracts items from the response and maps each item into a DiscoveredTarget
func (l *Loader) extractTargetsFromResponse(raw any, logger logr.Logger) ([]core.DiscoveredTarget, error) {
	var items []any
	// If ResponseMapping is configured and TargetsField is provided we treat
	// it as a CEL expression that evaluates against the whole response and
	// must return an array of items.
	if l.spec.ResponseMapping != nil && l.spec.ResponseMapping.TargetsField != "" {
		prog, err := compileCEL(l.spec.ResponseMapping.TargetsField)
		if err != nil {
			return nil, fmt.Errorf("invalid TargetsField CEL expression: %w", err)
		}
		out, _, err := prog.Eval(map[string]any{"self": raw})
		if err != nil {
			return nil, fmt.Errorf("evaluating TargetsField CEL expression failed: %w", err)
		}
		if out == nil {
			return nil, fmt.Errorf("TargetsField expression returned nil")
		}
		array, ok := out.Value().([]any)
		if !ok {
			return nil, fmt.Errorf("invalid HTTP response: targetsField expression must evaluate to an array of objects")
		}
		items = array
	} else {
		//If TargetsField is empty, the raw response is expected to be an array of items.
		array, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid HTTP response: expected a JSON array when itemsField is not set")
		}
		items = array
	}

	// Map items to targets
	var targets []core.DiscoveredTarget
	targets, err := l.mapItemsToTargets(items, raw, logger)
	if err != nil {
		return nil, fmt.Errorf("mapping items to targets failed: %w", err)
	}

	return targets, nil
}

// getNextURL determines the next page URL
// Returns:
// - nextURL: next request
// - stop: whether to terminate loop
func (l *Loader) getNextURL(
	raw any,
	headers http.Header,
	currentURL string,
	logger logr.Logger,
) (string, bool) {
	// Extract pagination info
	// Link header
	if next := extractNextFromLinkHeader(headers); next != "" {
		return next, false
	}

	// Body
	nextPage, err := l.extractNextPageInfo(raw)
	if err != nil {
		logger.Error(err, "pagination extraction failed")
		return "", true
	}

	if nextPage == "" {
		return "", true
	}

	// Build next page URL
	nextURL, err := l.buildNextURL(currentURL, nextPage)
	if err != nil {
		logger.Error(err, "failed to build next URL")
		return "", true
	}

	return nextURL, false
}
