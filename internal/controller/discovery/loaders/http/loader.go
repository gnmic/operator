package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	loaderUtils "github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
)

// Loader implements the HTTP pull discovery mechanism
type Loader struct {
	loaderCfg core.CommonLoaderConfig
	spec      gnmicv1alpha1.HTTPConfig
}

// New instantiates the http loader with the provided config
func New(cfg core.CommonLoaderConfig, httpConfig gnmicv1alpha1.HTTPConfig) core.Loader {
	return &Loader{loaderCfg: cfg, spec: httpConfig}
}

func (l *Loader) Name() string {
	return "http"
}

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

	// Input Validation of spec
	// if l.spec.URL == "nil" {
	// 	return errors.New("HTTP loader requires spec.provider.http to be set")
	// }

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
		targets, err := l.fetchTargetsFromHTTPEndpoint(ctx, client)
		if err != nil {
			logger.Error(
				err,
				"Failed to fetch targets from HTTP endpoint",
				"url", l.spec.URL,
			)
			return
		}

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

func (l *Loader) buildHTTPClient() (*http.Client, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: l.spec.TLS != nil && l.spec.TLS.InsecureSkipVerify,
	}

	if l.spec.TLS != nil && len(l.spec.TLS.CABundle) > 0 {
		certPool := x509.NewCertPool()
		if ok := certPool.AppendCertsFromPEM(l.spec.TLS.CABundle); !ok {
			return nil, fmt.Errorf("Failed to parse CA bundle for TargetSource %s/%s\n", l.loaderCfg.TargetsourceNN.Namespace, l.loaderCfg.TargetsourceNN.Name)
		}
		tlsConfig.RootCAs = certPool
	}

	return &http.Client{
		Timeout: l.spec.Timeout.Duration,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

func (l *Loader) fetchTargetsFromHTTPEndpoint(
	ctx context.Context,
	client *http.Client,
) ([]core.DiscoveredTarget, error) {
	var allTargets []core.DiscoveredTarget
	currentUrl := l.spec.URL

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentUrl, nil)
		if err != nil {
			return nil, fmt.Errorf("creating HTTP request failed: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		l.applyAuthorization(req)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
		}

		// Decode response into raw map for pagination support
		var raw map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return nil, fmt.Errorf("failed to decode HTTP response: %w", err)
		}

		// Extract targets from response
		targets, err := l.extractTargetsFromResponse(raw)
		if err != nil {
			return nil, err
		}
		allTargets = append(allTargets, targets...)

		// Check for pagination
		nextPageInfo, err := l.extractNextPageInfo(raw)
		if err != nil {
			return nil, err
		}
		if nextPageInfo == "" {
			break
		}
		nextURL, err := l.buildNextURL(currentUrl, nextPageInfo)
		if err != nil {
			return nil, err
		}
		currentUrl = nextURL
	}

	return allTargets, nil
}

func (l *Loader) extractTargetsFromResponse(raw map[string]interface{}) ([]core.DiscoveredTarget, error) {
	var items []interface{}

	if l.spec.Pagination != nil && l.spec.Pagination.ItemsField != "" {
		// Extract items array from response using itemsField
		val, ok := raw[l.spec.Pagination.ItemsField]
		if !ok {
			return nil, fmt.Errorf("itemsField '%s' not found", l.spec.Pagination.ItemsField)
		}

		arr, ok := val.([]interface{})
		if !ok {
			return nil, fmt.Errorf("itemsField '%s' is not an array", l.spec.Pagination.ItemsField)
		}

		items = arr
	} else {
		// fallback: whole response is array
		data, _ := json.Marshal(raw)
		var out []interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("failed to interpret response as list")
		}
		items = out
	}

	// Map items to targets
	var targets []core.DiscoveredTarget
	for _, item := range items {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		target, err := l.mapItem(obj)
		if err != nil {
			return nil, err
		}

		targets = append(targets, target)
	}

	return targets, nil
}
