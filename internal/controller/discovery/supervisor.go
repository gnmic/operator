package discovery

import (
	"context"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ComponentExit struct {
	Name string
	Err  error
}

type RestartPolicy struct {
	MaxRestarts int
	Backoff     time.Duration
}

type Supervisor struct {
	ctx      context.Context
	cancel   context.CancelFunc
	policy   RestartPolicy
	failures int
	exits    chan ComponentExit
	wg       sync.WaitGroup
	stopped  bool
	stopMu   sync.Mutex
}

func NewSupervisor(parentCtx context.Context, policy RestartPolicy) *Supervisor {
	ctx, cancel := context.WithCancel(parentCtx)
	return &Supervisor{
		ctx:      ctx,
		cancel:   cancel,
		policy:   policy,
		exits:    make(chan ComponentExit, 4),
		failures: 0,
	}
}

func (s *Supervisor) Context() context.Context {
	return s.ctx
}

func (s *Supervisor) Stop() {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()

	if s.stopped {
		return
	}

	s.stopped = true
	s.cancel()
}

func (s *Supervisor) Run(
	start func(ctx context.Context, exits chan<- ComponentExit),
) error {
	logger := log.FromContext(s.ctx).WithName("discovery-supervisor")

	for {
		if s.failures > 0 {
			logger.Info("Restarting pipeline",
				"attempt", s.failures,
				"maxAttempts", s.policy.MaxRestarts,
			)

			runtimeCtx, cancel := context.WithCancel(s.ctx)
			s.wg = sync.WaitGroup{}
			start(runtimeCtx, s.exits)
			exit := <-s.exits // first failure wins

			logger.Error(exit.Err,
				"Pipeline component crashed",
				"component", exit.Name,
			)

			cancel()
			s.wg.Wait()

			s.failures++
			if s.failures >= s.policy.MaxRestarts {
				logger.Error(exit.Err,
					"Pipeline exceeded maximum restart attempts; waiting for next reconciliation to restart",
					"restarts", s.failures,
				)
				s.Stop()
				return exit.Err
			}

			select {
			case <-time.After(s.policy.Backoff):
				// continue to restart
			case <-s.ctx.Done():
				// Supervisor context canceled during backoff
				return s.ctx.Err()
			}
		}
	}
}

func (s *Supervisor) Go(name string, fn func(ctx context.Context) error) {
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		err := fn(s.ctx)
		if err == nil {
			err = context.Canceled // treat normal exit as cancellation
		}

		select {
		case s.exits <- ComponentExit{Name: name, Err: err}:
			// exit reported successfully
		case <-s.ctx.Done():
			// Supervisor context canceled before reporting exit
		}
	}()
}
