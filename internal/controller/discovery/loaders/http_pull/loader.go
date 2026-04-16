package http_pull

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
)

type Loader struct{}

// New instantiates the http_pull loader
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
	logger := log.FromContext(ctx).WithValues("loader", l.Name())

	logger.Info("HTTP pull loader started")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Only for debugging: emit a static snapshot every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("HTTP pull loader stopped")
			return nil

		case <-ticker.C:
			targets, err := l.fetchTargetsFromHTTPEndpoint(ctx, client, spec.Provider.HTTP.URL, spec.Provider.HTTP.Token)
			if err != nil {
				logger.Error(err, "failed to fetch targets from HTTP endpoint")
				continue
			}

			var messages []core.DiscoveryMessage
			for _, target := range targets {
				messages = append(messages, core.DiscoveryMessage{
					Target: target,
					Event:  core.CREATE,
				})
			}

			// Non-blocking context-aware send
			select {
			case out <- messages:
				logger.Info(
					"emitted target snapshot",
					"count", len(messages),
				)
			case <-ctx.Done():
				logger.Info("context cancelled while emitting targets")
				return nil
			}
		}
	}
}

func (l *Loader) fetchTargetsFromHTTPEndpoint(ctx context.Context, client *http.Client, url string, token string) ([]core.DiscoveredTarget, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var targets []core.DiscoveredTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, err
	}

	return targets, nil
}
