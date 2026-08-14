package marketdata

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ProbeConfig struct {
	Samples          int
	Interval         time.Duration
	RequestTimeout   time.Duration
	MaximumQuoteAge  time.Duration
	MaximumClockSkew time.Duration
}

type ProbeResult struct {
	Provider    string
	StartedAt   time.Time
	FinishedAt  time.Time
	Requested   int
	Completed   int
	Interrupted bool
	Samples     []BatchReport
	Aggregate   AggregateReport
}

type ProbeRunner struct {
	evaluator *Evaluator
	now       func() time.Time
	wait      func(context.Context, time.Duration) error
}

func NewProbeRunner(provider QuoteProvider) (*ProbeRunner, error) {
	evaluator, err := NewEvaluator(provider)
	if err != nil {
		return nil, err
	}
	return &ProbeRunner{evaluator: evaluator, now: time.Now, wait: waitContext}, nil
}

// Run performs exactly config.Samples calls unless its context is cancelled.
// It never retries a failed sample, so rate limits and outages remain measurable.
func (r *ProbeRunner) Run(ctx context.Context, instruments []Instrument, config ProbeConfig) (ProbeResult, error) {
	if err := validateProbeConfig(instruments, config); err != nil {
		return ProbeResult{}, err
	}
	result := ProbeResult{
		Provider: r.evaluator.provider.Name(), StartedAt: r.now(), Requested: config.Samples,
		Samples: make([]BatchReport, 0, config.Samples),
	}
	for sample := 0; sample < config.Samples; sample++ {
		if err := ctx.Err(); err != nil {
			result.Interrupted = true
			break
		}
		requestCtx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
		report := r.evaluator.Evaluate(requestCtx, instruments, config.MaximumQuoteAge, config.MaximumClockSkew)
		cancel()
		result.Samples = append(result.Samples, report)
		result.Completed++

		if sample+1 < config.Samples {
			if err := r.wait(ctx, config.Interval); err != nil {
				result.Interrupted = true
				break
			}
		}
	}
	result.FinishedAt = r.now()
	result.Aggregate = Aggregate(result.Samples)
	return result, nil
}

func validateProbeConfig(instruments []Instrument, config ProbeConfig) error {
	if len(instruments) == 0 {
		return errors.New("probe requires at least one instrument")
	}
	if config.Samples < 1 || config.Samples > 10_000 {
		return errors.New("probe samples must be between 1 and 10000")
	}
	if config.Interval < 0 || config.RequestTimeout <= 0 || config.MaximumQuoteAge < 0 || config.MaximumClockSkew < 0 {
		return errors.New("probe durations are invalid")
	}
	// This explicit estimate is returned before any provider call, preventing an
	// accidental configuration from hiding its intended quote volume.
	if len(instruments) > 10_000/config.Samples {
		return fmt.Errorf("probe would exceed the 10000 instrument-observation safety cap")
	}
	return nil
}

func waitContext(ctx context.Context, duration time.Duration) error {
	if duration == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
