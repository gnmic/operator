package discovery

import "time"

// DefaultRestartPolicy defines the default restart behavior
// for the discovery components
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		MaxRestarts: 5,
		Backoff:     3 * time.Second,
	}
}
