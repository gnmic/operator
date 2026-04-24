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
	Name   string
	Run    func(ctx context.Context) error
	Policy RestartPolicy
}

type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc

	stopped bool
	mu      sync.Mutex

	components []Component
}

func NewSupervisor(parent context.Context) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *Supervisor) AddComponent(c Component) {
	s.components = append(s.components, c)
}

func (s *Supervisor) runComponent(c Component) {
	logger := log.FromContext(s.ctx).WithValues(
		"component", c.Name,
	)

	failures := 0

	for {
		err := c.Run(s.ctx)
		if s.ctx.Err() != nil {
			return
		}

		failures++
		logger.Error(err,
			"Component failed",
			"attempt", failures,
		)

		if failures >= c.Policy.MaxRestarts {
			logger.Error(err,
				"Component exceeded restart limit; stopping discovery pipeline",
				"restarts", failures,
			)
			s.Stop()
			return
		}

		select {
		case <-time.After(c.Policy.Backoff):
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Supervisor) Run() {
	for _, c := range s.components {
		component := c
		go s.runComponent(component)
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
