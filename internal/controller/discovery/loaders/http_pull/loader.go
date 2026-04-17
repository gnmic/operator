package http_pull

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/google/uuid"
)

const (
	chunkSize           = 100
	defaultPollInterval = 30 * time.Second
)

// Loader implements the HTTP pull discovery mechanism
type Loader struct{}

// New returns a new http_pull loader instance
func New() core.Loader {
	return &Loader{}
}

func (l *Loader) Name() string {
	return "http_pull"
}

func (l *Loader) Start(
	ctx context.Context,
	targetsourceName string,
	spec gnmicv1alpha1.TargetSourceSpec,
	out chan<- []core.DiscoveryMessage,
) error {
	logger := log.FromContext(ctx).WithValues(
		"component", "loader",
		"name", l.Name(),
		"targetsource", targetsourceName,
	)
	logger.Info("HTTP pull loader started")

	// Input Validation of spec
	if spec.Provider == nil || spec.Provider.HTTP == nil {
		return errors.New("http_pull loader requires spec.provider.http to be set")
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

		snapshotID := fmt.Sprintf("snapshot-%s-%s", targetsourceName, uuid.NewString())
		if err := core.SendSnapshot(ctx, out, targets, snapshotID, chunkSize); err != nil {
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
			logger.Info("HTTP pull loader stopped")
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
