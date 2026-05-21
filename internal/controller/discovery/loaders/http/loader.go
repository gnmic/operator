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
	"k8s.io/apimachinery/pkg/types"

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
		targets, err := l.fetchTargetsFromHTTPEndpoint(ctx, client)
		if err != nil {
			logger.Error(
				err,
				"Failed to fetch targets from HTTP endpoint",
				"url", l.spec.URL,
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
func (l *Loader) buildHTTPClient() (*http.Client, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: l.spec.TLS != nil && l.spec.TLS.InsecureSkipVerify,
	}

	// If a CA bundle is provided, add it to the TLS config
	if l.spec.TLS != nil && len(l.spec.TLS.CABundle) > 0 {
		certPool := x509.NewCertPool()
		if ok := certPool.AppendCertsFromPEM(l.spec.TLS.CABundle); !ok {
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
) ([]core.DiscoveredTarget, error) {
	var allTargets []core.DiscoveredTarget
	currentUrl := l.spec.URL

	for {
		// Create HTTP request with context
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentUrl, nil)
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

		// Decode response into raw map for pagination support
		var raw interface{}
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

// extractTargetsFromResponse extracts items from the response
// and maps each item into a DiscoveredTarget
func (l *Loader) extractTargetsFromResponse(raw interface{}) ([]core.DiscoveredTarget, error) {
	var items []interface{}

	switch v := raw.(type) {
	// Top-level array response
	case []interface{}:
		items = v
	// Object with itemsField containing the array
	case map[string]interface{}:
		if l.spec.Pagination != nil && l.spec.Pagination.ItemsField != "" {
			// Extract items array from response using itemsField
			val, ok := v[l.spec.Pagination.ItemsField]
			if !ok {
				return nil, fmt.Errorf("itemsField '%s' not found", l.spec.Pagination.ItemsField)
			}

			arr, ok := val.([]interface{})
			if !ok {
				return nil, fmt.Errorf("itemsField '%s' is not an array", l.spec.Pagination.ItemsField)
			}

			items = arr
		} else {
			return nil, fmt.Errorf("response is an object but no itemsField specified for TargetSource %s/%s", l.loaderCfg.TargetsourceNN.Namespace, l.loaderCfg.TargetsourceNN.Name)
		}
	default:
		return nil, fmt.Errorf("unexpected response format")
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

func (l *Loader) SecretRefs() []types.NamespacedName {
	var refs []types.NamespacedName

	if authSpec := l.spec.Authorization; authSpec != nil {
		if authSpec.Basic != nil && authSpec.Basic.CredentialsSecretRef != nil {
			refs = append(refs, types.NamespacedName{
				Namespace: l.loaderCfg.TargetsourceNN.Namespace,
				Name:      authSpec.Basic.CredentialsSecretRef.Name,
			})
		}

		if authSpec.Token != nil && authSpec.Token.TokenSecretRef != nil {
			refs = append(refs, types.NamespacedName{
				Namespace: l.loaderCfg.TargetsourceNN.Namespace,
				Name:      authSpec.Token.TokenSecretRef.Name,
			})
		}
	}

	if tlsSpec := l.spec.TLS; tlsSpec != nil && tlsSpec.CABundleSecretRef != nil {
		refs = append(refs, types.NamespacedName{
			Namespace: l.loaderCfg.TargetsourceNN.Namespace,
			Name:      tlsSpec.CABundleSecretRef.Name,
		})
	}

	return refs
}
