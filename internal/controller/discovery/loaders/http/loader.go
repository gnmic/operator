package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	loaderUtils "github.com/gnmic/operator/internal/controller/discovery/loaders/utils"
	"github.com/google/uuid"
)

const (
	defaultPollInterval = 30 * time.Second
)

// Loader implements the HTTP pull discovery mechanism
type Loader struct {
	cfg core.LoaderConfig
}

// New instantiates the http loader with the provided config
func New(cfg core.LoaderConfig) core.Loader {
	return &Loader{cfg: cfg}
}

func (l *Loader) Name() string {
	return "http"
}

func (l *Loader) Start(
	ctx context.Context,
	targetsourceNN types.NamespacedName,
	spec gnmicv1alpha1.TargetSourceSpec,
	out chan<- []core.DiscoveryMessage,
) error {
	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", targetsourceNN,
	)

	logger.Info(
		"HTTP loader started",
		"targetsource", targetsourceNN.Name,
		"namespace", targetsourceNN.Namespace,
	)

	// Input Validation of spec
	if spec.Provider == nil || spec.Provider.HTTP == nil {
		return errors.New("HTTP loader requires spec.provider.http to be set")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	interval := defaultPollInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("HTTP pull loader started", "interval", interval.String())

	// helper function to fetch targets and emit discovery messages
	fetchAndEmit := func() {
		targets, err := l.fetchTargetsFromHTTPEndpoint(
			ctx,
			client,
			spec.Provider.HTTP.URL,
			spec.Provider.HTTP.Token,
		)
		if err != nil {
			logger.Error(err, "failed to fetch targets from HTTP endpoint")
			return
		}

		snapshotID := fmt.Sprintf("%s-%s-%s", targetsourceNN.Namespace, targetsourceNN.Name, uuid.NewString())
		if err := loaderUtils.SendSnapshot(ctx, out, targets, snapshotID, l.cfg.ChunkSize); err != nil {
			logger.Error(err, "failed to send discovery snapshot")
			return
		}
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
