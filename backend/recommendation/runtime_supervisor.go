package recommendation

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RuntimeSupervisorPolicy controls how the application retries transient
// infrastructure failures without spinning or silently abandoning durable work.
type RuntimeSupervisorPolicy struct {
	TickInterval      time.Duration
	InitialRetryDelay time.Duration
	MaximumRetryDelay time.Duration
}

// RuntimeHealth is an immutable snapshot suitable for logs or a read-only UI.
// LastError is cleared only after a successful tick.
type RuntimeHealth struct {
	Running             bool              `json:"running"`
	LastTickStartedAt   time.Time         `json:"lastTickStartedAt"`
	LastTickCompletedAt time.Time         `json:"lastTickCompletedAt"`
	ConsecutiveFailures int               `json:"consecutiveFailures"`
	LastError           string            `json:"lastError"`
	LastResult          RuntimeTickResult `json:"lastResult"`
}

// RuntimeSupervisor keeps the shadow runtime alive across transient provider
// outages. The runtime's durable leases and idempotency keys remain the source
// of truth; this type only controls retry timing and observable health.
type RuntimeSupervisor struct {
	runtime *ShadowRuntime
	policy  RuntimeSupervisorPolicy

	mu     sync.RWMutex
	health RuntimeHealth
	wait   func(context.Context, time.Duration) bool
}

func NewRuntimeSupervisor(runtime *ShadowRuntime, policy RuntimeSupervisorPolicy) (*RuntimeSupervisor, error) {
	if runtime == nil || policy.TickInterval <= 0 || policy.InitialRetryDelay <= 0 || policy.MaximumRetryDelay < policy.InitialRetryDelay {
		return nil, errors.New("runtime supervisor requires runtime, positive intervals, and maximum retry delay not below initial delay")
	}
	return &RuntimeSupervisor{
		runtime: runtime,
		policy:  policy,
		wait: func(ctx context.Context, delay time.Duration) bool {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-timer.C:
				return true
			}
		},
	}, nil
}

// Health returns a copy so observers cannot mutate supervisor state.
func (s *RuntimeSupervisor) Health() RuntimeHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health
}

// Run executes immediately, retries infrastructure failures with bounded
// exponential backoff, and returns only when the context is cancelled.
func (s *RuntimeSupervisor) Run(ctx context.Context, now func() time.Time) error {
	if ctx == nil || now == nil {
		return errors.New("runtime supervisor requires context and clock")
	}
	s.setRunning(true)
	defer s.setRunning(false)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		started := now()
		result, tickErr := s.runtime.Tick(ctx, started)
		completed := now()
		failures := s.recordTick(started, completed, result, tickErr)

		delay := s.policy.TickInterval
		if tickErr != nil {
			delay = retryDelay(s.policy.InitialRetryDelay, s.policy.MaximumRetryDelay, failures)
		}
		if !s.wait(ctx, delay) {
			return nil
		}
	}
}

func (s *RuntimeSupervisor) setRunning(running bool) {
	s.mu.Lock()
	s.health.Running = running
	s.mu.Unlock()
}

func (s *RuntimeSupervisor) recordTick(started, completed time.Time, result RuntimeTickResult, err error) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.LastTickStartedAt = started
	s.health.LastTickCompletedAt = completed
	s.health.LastResult = result
	if err == nil {
		s.health.ConsecutiveFailures = 0
		s.health.LastError = ""
	} else {
		s.health.ConsecutiveFailures++
		s.health.LastError = err.Error()
	}
	return s.health.ConsecutiveFailures
}

func retryDelay(initial, maximum time.Duration, failures int) time.Duration {
	if failures <= 1 {
		return initial
	}
	delay := initial
	for i := 1; i < failures; i++ {
		if delay >= maximum || delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
