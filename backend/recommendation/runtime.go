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

// RuntimeTickObserver receives every completed iteration, including partial
// results paired with an infrastructure error. Implementations should return
// quickly; the runtime invokes the observer synchronously to preserve ordering.
type RuntimeTickObserver func(RuntimeTickResult, error)

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
	var stageErrors []error
	run, created, scheduleErr := r.scheduler.Tick(ctx, now)
	if scheduleErr != nil {
		stageErrors = append(stageErrors, fmt.Errorf("schedule shadow analysis: %w", scheduleErr))
	} else {
		result.RunCreated = created
		if run != nil {
			result.RunID = run.ID
		}
	}
	for len(result.ProcessedJobs) < r.maxJobsTick {
		processed, err := r.processor.ProcessNext(ctx)
		if IsNoReadyJob(err) {
			break
		}
		if err != nil {
			stageErrors = append(stageErrors, fmt.Errorf("process shadow analysis job: %w", err))
			break
		}
		result.ProcessedJobs = append(result.ProcessedJobs, processed)
	}
	completed, err := r.reviews.ProcessDue(ctx, now)
	if err != nil {
		stageErrors = append(stageErrors, fmt.Errorf("process due shadow reviews: %w", err))
	} else {
		result.CompletedReviews = completed
	}
	return result, errors.Join(stageErrors...)
}

// Run executes immediately and then at the configured interval until the
// caller cancels the context. It returns on the first infrastructure error so a
// supervisor can surface the failure instead of silently stopping the pipeline.
func (r *ShadowRuntime) Run(ctx context.Context, interval time.Duration, now func() time.Time) error {
	return r.RunObserved(ctx, interval, now, nil)
}

// RunObserved behaves like Run and publishes each tick before deciding whether
// to stop on an infrastructure error. This gives an application supervisor the
// evidence needed to expose runtime health without weakening fail-closed exits.
func (r *ShadowRuntime) RunObserved(ctx context.Context, interval time.Duration, now func() time.Time, observe RuntimeTickObserver) error {
	if ctx == nil || interval <= 0 || now == nil {
		return errors.New("shadow runtime loop requires context, positive interval, and clock")
	}
	if result, err := r.Tick(ctx, now()); err != nil {
		if observe != nil {
			observe(result, err)
		}
		return err
	} else if observe != nil {
		observe(result, nil)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			result, err := r.Tick(ctx, now())
			if observe != nil {
				observe(result, err)
			}
			if err != nil {
				return err
			}
		}
	}
}
