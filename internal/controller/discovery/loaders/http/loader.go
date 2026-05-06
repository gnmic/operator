package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"k8s.io/kube-openapi/pkg/validation/spec"

	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	loaderUtils "github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultTimeoutSeconds = 30
)

// Loader implements the HTTP pull discovery mechanism
type Loader struct {
	commonCfg core.CommonLoaderConfig
	spec      *gnmicv1alpha1.HTTPConfig
}

// New instantiates the http loader with the provided config
func New(cfg core.CommonLoaderConfig, httpConfig gnmicv1alpha1.HTTPConfig) core.Loader {
	return &Loader{commonCfg: cfg, spec: &httpConfig}
}

func (l *Loader) Name() string {
	return "http"
}

func (l *Loader) Run(ctx context.Context, out chan<- []core.DiscoveryMessage) error {
	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", l.commonCfg.TargetsourceNN,
	)

	logger.Info(
		"HTTP loader started",
		"targetsource", l.commonCfg.TargetsourceNN.Name,
		"namespace", l.commonCfg.TargetsourceNN.Namespace,
	)

	logger.Info("HTTP loader started")

	// Input Validation of spec
	if spec. == nil {
		return errors.New("HTTP loader requires spec.provider.http to be set")
	}

	client := &http.Client{
		Timeout: defaultTimeoutSeconds * time.Second,
	}
	interval := defaultPollInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info(
		"HTTP polling discovery started",
		"interval", interval.String(),
		"url", spec.Provider.HTTP.URL,
	)

	// helper function to fetch targets and emit discovery messages
	fetchAndEmit := func() {
		targets, err := l.fetchTargetsFromHTTPEndpoint(
			ctx,
			client,
			spec.Provider.HTTP.URL,
			spec.Provider.HTTP.Token,
		)
		if err != nil {
			logger.Error(
				err,
				"Failed to fetch targets from HTTP endpoint",
				"url", spec.Provider.HTTP.URL,
			)
			return
		}

		snapshotID := fmt.Sprintf("%s-%s-%s", l.commonCfg.TargetsourceNN.Namespace, l.commonCfg.TargetsourceNN.Name, uuid.NewString())
		if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.commonCfg.ChunkSize); err != nil {
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

func (l *Loader) fetchTargetsFromHTTPEndpoint(
	ctx context.Context,
	client *http.Client,
	url string,
	token string,
) ([]core.DiscoveredTarget, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request failed: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}

	var targets []core.DiscoveredTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("failed to decode HTTP response: %w", err)
	}

	return targets, nil
}
