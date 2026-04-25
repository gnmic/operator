package discovery

import (
	"context"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type RestartPolicy struct {
	MaxRestarts int
	Backoff     time.Duration
}

type Component struct {
	Name             string
	Run              func(ctx context.Context) error
	Policy           RestartPolicy
	DegradeOnFailure bool
}

type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	mu      sync.Mutex
	stopped bool
}

func NewSupervisor(parent context.Context) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true
	s.cancel()
}

func (s *Supervisor) Done() <-chan struct{} {
	return s.ctx.Done()
}

func (s *Supervisor) Wait() {
	s.wg.Wait()
}

func (s *Supervisor) RunComponent(component Component) {
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
				if component.DegradeOnFailure {
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
