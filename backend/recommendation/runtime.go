package recommendation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RuntimeTickResult is operational evidence for one bounded shadow-runtime
// iteration. Analysis failures remain in ProcessedJobs because they are durable
// job outcomes, while infrastructure failures are returned as errors.
type RuntimeTickResult struct {
	RunCreated       bool            `json:"runCreated"`
	RunID            uint            `json:"runId"`
	RecoveredLeases  int64           `json:"recoveredLeases"`
	ProcessedJobs    []ProcessResult `json:"processedJobs"`
	CompletedReviews int             `json:"completedReviews"`
}

// ShadowRuntime composes the durable scheduler, worker, lease recovery, and
// review pass. Provider and model construction stays outside this type so real
// credentials and product policy cannot be hidden in library defaults.
type ShadowRuntime struct {
	scheduler   *PostCloseScheduler
	processor   *AnalysisProcessor
	jobs        *JobStore
	reviews     *ReviewScheduler
	maxJobsTick int
}

func NewShadowRuntime(scheduler *PostCloseScheduler, processor *AnalysisProcessor, jobs *JobStore, reviews *ReviewScheduler, maxJobsTick int) (*ShadowRuntime, error) {
	if scheduler == nil || processor == nil || jobs == nil || reviews == nil || maxJobsTick <= 0 {
		return nil, errors.New("shadow runtime requires scheduler, processor, jobs, reviews, and a positive per-tick job limit")
	}
	return &ShadowRuntime{scheduler: scheduler, processor: processor, jobs: jobs, reviews: reviews, maxJobsTick: maxJobsTick}, nil
}

// Tick performs bounded work and is safe to retry. The scheduler/run key,
// leased job claims, atomic snapshot completion, and conditional review update
// provide idempotency at each durable boundary.
func (r *ShadowRuntime) Tick(ctx context.Context, now time.Time) (RuntimeTickResult, error) {
	result := RuntimeTickResult{ProcessedJobs: make([]ProcessResult, 0)}
	if ctx == nil || now.IsZero() {
		return result, errors.New("shadow runtime requires context and current time")
	}
	recovered, err := r.jobs.RecoverExpired(now)
	if err != nil {
		return result, fmt.Errorf("recover shadow worker leases: %w", err)
	}
	result.RecoveredLeases = recovered
	run, created, err := r.scheduler.Tick(ctx, now)
	if err != nil {
		return result, fmt.Errorf("schedule shadow analysis: %w", err)
	}
	result.RunCreated = created
	if run != nil {
		result.RunID = run.ID
	}
	for len(result.ProcessedJobs) < r.maxJobsTick {
		processed, err := r.processor.ProcessNext(ctx)
		if IsNoReadyJob(err) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("process shadow analysis job: %w", err)
		}
		result.ProcessedJobs = append(result.ProcessedJobs, processed)
	}
	completed, err := r.reviews.ProcessDue(ctx, now)
	if err != nil {
		return result, fmt.Errorf("process due shadow reviews: %w", err)
	}
	result.CompletedReviews = completed
	return result, nil
}

// Run executes immediately and then at the configured interval until the
// caller cancels the context. It returns on the first infrastructure error so a
// supervisor can surface the failure instead of silently stopping the pipeline.
func (r *ShadowRuntime) Run(ctx context.Context, interval time.Duration, now func() time.Time) error {
	if ctx == nil || interval <= 0 || now == nil {
		return errors.New("shadow runtime loop requires context, positive interval, and clock")
	}
	if _, err := r.Tick(ctx, now()); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := r.Tick(ctx, now()); err != nil {
				return err
			}
		}
	}
}
