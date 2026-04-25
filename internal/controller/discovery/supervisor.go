package discovery

import (
	"context"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Supervisor coordinates the runtime lifecycle of pipeline components
//
// Guarantees:
// - Each component is restarted independently
// - Permanent failure escalates according to policy
// - Stop() cancels all components
// - Wait() blocks until all goroutines exit
type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	mu      sync.Mutex
	stopped bool
}

// RestartPolicy defines the restart behavior for a component
type RestartPolicy struct {
	MaxRestarts int
	Backoff     time.Duration
}

type ComponentSpec struct {
	Name   string
	Run    func(ctx context.Context) error
	Policy RestartPolicy
	// EscalatesOnFailure indicates whether a permanent failure of this component should shut down the entire pipeline
	EscalatesOnFailure bool
}

// NewSupervisor creates a new Supervisor with a cancellable context
func NewSupervisor(parentCtx context.Context) *Supervisor {
	ctx, cancel := context.WithCancel(parentCtx)
	return &Supervisor{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Stop signals all supervised components to stop by canceling the context
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true
	s.cancel()
}

// Done returns a channel that is closed when the pipeline is stopped
func (s *Supervisor) Done() <-chan struct{} { return s.ctx.Done() }

// Wait blocks until all supervised components have exited
func (s *Supervisor) Wait() { s.wg.Wait() }

// StartSupervisedComponent starts and supervises a component
func (s *Supervisor) StartSupervisedComponent(component ComponentSpec) {
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		logger := log.FromContext(s.ctx).WithValues("component", component.Name)
		failures := 0

		for {
			logger.Info("starting component")
			err := component.Run(s.ctx)

			if s.ctx.Err() != nil {
				logger.Info("component stopped due to pipeline shutdown")
				return
			}

			failures++
			logger.Error(err,
				"component failed to run",
				"attempt", failures,
				"max", component.Policy.MaxRestarts,
			)

			if failures >= component.Policy.MaxRestarts {
				if component.EscalatesOnFailure {
					logger.Error(err,
						"component permanently failed; shutting down pipeline",
					)
					s.Stop()
				} else {
					logger.Info(
						"optional component permanently failed; continuing without it",
					)
				}
				return
			}

			select {
			case <-time.After(component.Policy.Backoff):
			case <-s.ctx.Done():
				return
			}
		}
	}()
}
