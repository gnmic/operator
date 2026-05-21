package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"

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

	client, err := l.buildHTTPClient()
	if err != nil {
		return fmt.Errorf("failed to build HTTP client: %w", err)
	}
	interval := l.spec.PollInterval.Duration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info(
		"HTTP polling discovery started",
		"interval", interval.String(),
		"url", l.spec.URL,
	)

	// helper function to fetch targets and emit discovery messages
	fetchAndEmit := func() {
		// Fetch targets from HTTP endpoint
		targets, err := l.fetchTargetsFromHTTPEndpoint(ctx, client, logger)
		if err != nil {
			logger.Error(
				err,
				"Failed to fetch targets from HTTP endpoint",
				"url", l.spec.URL,
			)
			return
		}
		// TODO temporary log discovered targets
		for _, t := range targets {
			logger.Info(
				"Discovered target",
				"name", t.Name,
				"ip", t.IP,
				"port", t.Port,
				"labels", t.Labels,
				"profile", t.TargetProfile,
			)
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
func (l *Loader) buildHTTPClient() (*http.Client, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: l.spec.TLS != nil && l.spec.TLS.InsecureSkipVerify,
	}

	// If a CA bundle is provided, add it to the TLS config
	if l.spec.TLS != nil && len(l.spec.TLS.CABundle) > 0 {
		certPool := x509.NewCertPool()
		if ok := certPool.AppendCertsFromPEM([]byte(l.spec.TLS.CABundle)); !ok {
			return nil, fmt.Errorf("Failed to parse CA bundle for TargetSource %s/%s\n", l.loaderCfg.TargetsourceNN.Namespace, l.loaderCfg.TargetsourceNN.Name)
		}
		tlsConfig.RootCAs = certPool
	}

	// Build the HTTP client with the specified timeout and TLS config
	return &http.Client{
		Timeout: l.spec.Timeout.Duration,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

// fetchTargetsFromHTTPEndpoint retrieves targets from the configured HTTP endpoint
func (l *Loader) fetchTargetsFromHTTPEndpoint(
	ctx context.Context,
	client *http.Client,
	logger logr.Logger,
) ([]core.DiscoveredTarget, error) {
	var allTargets []core.DiscoveredTarget
	currentURL := l.spec.URL

	for {
		raw, err := l.fetchPage(ctx, client, currentURL)
		if err != nil {
			logger.Error(err,
				"Failed to fetch page from HTTP endpoint",
				"url", currentURL,
			)
			break
		}

		// Extract targets from response
		if targets, err := l.extractTargetsFromResponse(raw, logger); err != nil {
			logger.Error(err,
				"Failed to extract targets from HTTP response",
				"url", currentURL,
			)
		} else {
			allTargets = append(allTargets, targets...)
		}

		// Pagination
		nextURL, stop := l.getNextURL(raw, currentURL, logger)
		if stop {
			break
		}
		currentURL = nextURL
	}

	return allTargets, nil
}

// fetchPage performs an HTTP GET request to the specified URL and decodes the JSON response
// and returns the raw response as an interface{}
func (l *Loader) fetchPage(ctx context.Context, client *http.Client, url string) (interface{}, error) {
	// Build HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	l.applyAuthorization(req)

	// Execute HTTP request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}

	// Decode HTTP response
	var raw interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode HTTP response: %w", err)
	}

	return raw, nil
}

// extractTargetsFromResponse extracts items from the response
// and maps each item into a DiscoveredTarget
func (l *Loader) extractTargetsFromResponse(raw interface{}, logger logr.Logger) ([]core.DiscoveredTarget, error) {
	var items []interface{}

	if l.spec.TargetsField != "" {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(
				"invalid HTTP response: expected JSON object when itemsField '%s' is configured (e.g. {\"%s\": [...]})",
				l.spec.TargetsField,
				l.spec.TargetsField,
			)
		}

		results, ok := obj[l.spec.TargetsField]
		if !ok {
			return nil, fmt.Errorf(
				"invalid HTTP response: itemsField '%s' not found. ensure the API response contains this field (e.g. {\"%s\": [...]})",
				l.spec.TargetsField,
				l.spec.TargetsField,
			)
		}

		array, ok := results.([]interface{})
		if !ok {
			return nil, fmt.Errorf(
				"invalid HTTP response: itemsField '%s' must be an array of objects (e.g. {\"%s\": [...]})",
				l.spec.TargetsField,
				l.spec.TargetsField,
			)
		}

		items = array
	} else {
		array, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid HTTP response: expected a JSON array because itemsField is not set (e.g. [{...}, {...}])")
		}
		items = array
	}

	// Map items to targets
	var targets []core.DiscoveredTarget
	for _, item := range items {
		obj, ok := item.(map[string]interface{})
		if !ok {
			logger.Error(fmt.Errorf("invalid target format"),
				"Failed to convert target to map",
				"item", item,
			)
			continue
		}

		target, err := l.mapItem(obj)
		if err != nil {
			logger.Error(err,
				"Failed to map target",
				"item", obj,
			)
			continue
		}

		targets = append(targets, target)
	}

	return targets, nil
}

// getNextURL determines the next page URL
// Returns:
// - nextURL: next request
// - stop: whether to terminate loop
func (l *Loader) getNextURL(
	raw interface{},
	currentURL string,
	logger logr.Logger,
) (string, bool) {
	// Extract pagination info
	nextPageInfo, err := l.extractNextPageInfo(raw)
	if err != nil {
		logger.Error(err,
			"Failed to extract next page info from HTTP response",
			"url", currentURL,
		)
		return "", true
	}

	if nextPageInfo == "" {
		return "", true
	}

	// Build next page URL
	nextURL, err := l.buildNextURL(currentURL, nextPageInfo)
	if err != nil {
		logger.Error(err,
			"Failed to build next URL",
			"url", currentURL,
		)
		return "", true
	}

	return nextURL, false
}
