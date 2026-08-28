package recommendation

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RuntimeController owns exactly one supervisor goroutine and provides an
// idempotent lifecycle boundary for desktop startup and shutdown hooks.
type RuntimeController struct {
	supervisor *RuntimeSupervisor
	now        func() time.Time

	mu       sync.RWMutex
	cancel   context.CancelFunc
	done     chan struct{}
	lastExit error
}

func NewRuntimeController(supervisor *RuntimeSupervisor, now func() time.Time) (*RuntimeController, error) {
	if supervisor == nil || now == nil {
		return nil, errors.New("runtime controller requires supervisor and clock")
	}
	return &RuntimeController{supervisor: supervisor, now: now}, nil
}

// Start is idempotent while the controller is running. A controller that has
// stopped may be started again; durable leases and run keys handle recovery.
func (c *RuntimeController) Start(parent context.Context) error {
	if parent == nil {
		return errors.New("runtime controller requires parent context")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done != nil {
		select {
		case <-c.done:
			c.done = nil
			c.cancel = nil
		default:
			return nil
		}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	c.lastExit = nil
	go func() {
		err := c.supervisor.Run(ctx, c.now)
		c.mu.Lock()
		c.lastExit = err
		close(done)
		c.mu.Unlock()
	}()
	return nil
}

// Stop requests cancellation and waits for the supervisor to leave its tick or
// retry wait. It is safe to call repeatedly.
func (c *RuntimeController) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("runtime controller stop requires context")
	}
	c.mu.RLock()
	cancel, done := c.cancel, c.done
	c.mu.RUnlock()
	if done == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RuntimeControllerHealth combines supervisor evidence with lifecycle state.
type RuntimeControllerHealth struct {
	Configured bool          `json:"configured"`
	Running    bool          `json:"running"`
	LastExit   string        `json:"lastExit"`
	Runtime    RuntimeHealth `json:"runtime"`
}

func (c *RuntimeController) Health() RuntimeControllerHealth {
	c.mu.RLock()
	done, lastExit := c.done, c.lastExit
	c.mu.RUnlock()
	running := false
	if done != nil {
		select {
		case <-done:
		default:
			running = true
		}
	}
	health := RuntimeControllerHealth{Configured: true, Running: running, Runtime: c.supervisor.Health()}
	if lastExit != nil {
		health.LastExit = lastExit.Error()
	}
	return health
}
