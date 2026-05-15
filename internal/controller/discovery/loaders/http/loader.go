package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

func (l *Loader) applyAuthorization(req *http.Request) {
	auth := l.spec.Authorization
	if auth == nil {
		return
	}

	switch {
	case auth.Basic != nil:
		req.SetBasicAuth(
			auth.Basic.Username,
			auth.Basic.Password,
		)

	case auth.Token != nil:
		req.Header.Set(
			"Authorization",
			fmt.Sprintf("%s %s",
				auth.Token.Scheme,
				auth.Token.Token,
			),
		)

		// case auth.JWT != nil:
		// 	if auth.JWT.Token != "" {
		// 		req.Header.Set(
		// 			"Authorization",
		// 			fmt.Sprintf("Bearer %s", auth.JWT.Token),
		// 		)
		// 	}
	}
}

func (l *Loader) extractTargetsFromResponse(raw map[string]interface{}) ([]core.DiscoveredTarget, error) {
	var targets []core.DiscoveredTarget

	if l.spec.Pagination == nil || l.spec.Pagination.ItemsField == "" {
		// No pagination config, assume entire response is the target list
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		if err := json.Unmarshal(data, &targets); err != nil {
			return nil, fmt.Errorf("failed to decode targets: %w", err)
		}

		return targets, nil
	}

	// Extract from field
	items, ok := raw[l.spec.Pagination.ItemsField]
	if !ok {
		return nil, fmt.Errorf("itemsField '%s' not found in response", l.spec.Pagination.ItemsField)
	}

	data, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("failed to decode targets from itemsField: %w", err)
	}

	return targets, nil
}

func (l *Loader) extractNextPageInfo(raw map[string]interface{}) (string, error) {
	if l.spec.Pagination == nil || l.spec.Pagination.NextField == "" {
		return "", nil
	}

	val, ok := raw[l.spec.Pagination.NextField]
	if !ok {
		return "", fmt.Errorf("nextField '%s' not found in response", l.spec.Pagination.NextField)
	}

	next, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("nextField '%s' is not a string in response", l.spec.Pagination.NextField)
	}

	return next, nil
}

func (l *Loader) buildNextURL(currentURL, nextVal string) (string, error) {
	// nextVal is a full URL -> return as is
	if parsed, err := url.Parse(nextVal); err == nil && parsed.Scheme != "" {
		return nextVal, nil
	}

	// nextVal is a token -> append as query parameter
	parsedURL, err := url.Parse(currentURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse current URL in order to build next URL: %w", err)
	}
	q := parsedURL.Query()
	q.Set(l.spec.Pagination.NextField, nextVal)
	parsedURL.RawQuery = q.Encode()

	return parsedURL.String(), nil
}
