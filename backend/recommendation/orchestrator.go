package recommendation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

type TradingDayDecider interface {
	IsTradingDay(context.Context, time.Time) (bool, error)
}

type AnalysisCandidateSource interface {
	Candidates(context.Context, time.Time) ([]AnalysisCandidate, error)
}

type PostCloseScheduler struct {
	jobs        *JobStore
	calendar    TradingDayDecider
	candidates  AnalysisCandidateSource
	location    *time.Location
	startHour   int
	startMinute int
	strategy    string
	maxAttempts int
}

func NewPostCloseScheduler(jobs *JobStore, calendar TradingDayDecider, candidates AnalysisCandidateSource, location *time.Location, strategy string, maxAttempts int) (*PostCloseScheduler, error) {
	if jobs == nil || calendar == nil || candidates == nil || location == nil || strategy == "" || maxAttempts <= 0 {
		return nil, errors.New("post-close scheduler requires jobs, calendar, candidates, location, strategy, and attempts")
	}
	return &PostCloseScheduler{jobs: jobs, calendar: calendar, candidates: candidates, location: location,
		startHour: 15, startMinute: 30, strategy: strategy, maxAttempts: maxAttempts}, nil
}

// Tick is safe to call repeatedly. Before 15:30 or on a non-trading day it is a
// no-op; after the cutoff the JobStore date/strategy key prevents duplicate runs.
func (s *PostCloseScheduler) Tick(ctx context.Context, now time.Time) (*models.AIAnalysisRun, bool, error) {
	if now.IsZero() {
		return nil, false, errors.New("scheduler time is required")
	}
	local := now.In(s.location)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), s.startHour, s.startMinute, 0, 0, s.location)
	if local.Before(cutoff) {
		return nil, false, nil
	}
	trading, err := s.calendar.IsTradingDay(ctx, local)
	if err != nil {
		return nil, false, fmt.Errorf("resolve trading day: %w", err)
	}
	if !trading {
		return nil, false, nil
	}
	// Once a run exists, its jobs are the frozen candidate/evidence set. Avoid
	// calling external providers again on every runtime tick.
	existing, found, err := s.jobs.FindRun(local, s.strategy)
	if err != nil {
		return nil, false, fmt.Errorf("load existing analysis run: %w", err)
	}
	if found {
		return existing, false, nil
	}
	candidates, err := s.candidates.Candidates(ctx, local)
	if err != nil {
		return nil, false, fmt.Errorf("load analysis candidates: %w", err)
	}
	return s.jobs.CreateRun(local, s.strategy, candidates, s.maxAttempts, now)
}

type JobAnalyzer interface {
	Analyze(context.Context, models.AIAnalysisJob) (*models.AIRecommendationSnapshot, error)
}

type ProcessResult struct {
	JobID            uint
	Status           string
	RecommendationID uint
	AnalysisError    string
}

type AnalysisProcessor struct {
	jobs       *JobStore
	analyzer   JobAnalyzer
	lease      time.Duration
	retryDelay time.Duration
	now        func() time.Time
}

func NewAnalysisProcessor(jobs *JobStore, analyzer JobAnalyzer, lease, retryDelay time.Duration) (*AnalysisProcessor, error) {
	if jobs == nil || analyzer == nil || lease <= 0 || retryDelay < 0 {
		return nil, errors.New("analysis processor requires jobs, analyzer, positive lease, and non-negative retry delay")
	}
	return &AnalysisProcessor{jobs: jobs, analyzer: analyzer, lease: lease, retryDelay: retryDelay, now: time.Now}, nil
}

// ProcessNext runs at most one stock. Model failures are persisted as retry or
// terminal job state and returned as data, allowing the worker loop to continue.
func (p *AnalysisProcessor) ProcessNext(ctx context.Context) (ProcessResult, error) {
	now := p.now()
	job, err := p.jobs.ClaimNext(now, p.lease)
	if err != nil {
		return ProcessResult{}, err
	}
	snapshot, analyzeErr := p.analyzer.Analyze(ctx, *job)
	if analyzeErr == nil && snapshot == nil {
		analyzeErr = errors.New("analyzer returned no result")
	}
	finished := p.now()
	if analyzeErr != nil {
		if errors.Is(analyzeErr, ErrHardBudgetExceeded) {
			if err := p.jobs.Defer(job.ID, analyzeErr.Error(), firstDayOfNextMonth(finished), finished); err != nil {
				return ProcessResult{JobID: job.ID}, fmt.Errorf("defer budget-blocked analysis: %w", err)
			}
			return ProcessResult{JobID: job.ID, Status: JobRetry, AnalysisError: analyzeErr.Error()}, nil
		}
		if err := p.jobs.Fail(job.ID, analyzeErr.Error(), finished.Add(p.retryDelay), finished); err != nil {
			return ProcessResult{JobID: job.ID}, fmt.Errorf("persist analysis failure: %w", err)
		}
		var stored models.AIAnalysisJob
		if err := p.jobs.db.First(&stored, job.ID).Error; err != nil {
			return ProcessResult{JobID: job.ID}, err
		}
		return ProcessResult{JobID: job.ID, Status: stored.Status, AnalysisError: analyzeErr.Error()}, nil
	}
	if err := p.jobs.CompleteWithSnapshot(job.ID, snapshot, finished); err != nil {
		return ProcessResult{JobID: job.ID}, fmt.Errorf("persist analysis result: %w", err)
	}
	return ProcessResult{JobID: job.ID, Status: JobCompleted, RecommendationID: snapshot.ID}, nil
}

func firstDayOfNextMonth(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month()+1, 1, 0, 0, 0, 0, value.Location())
}

func IsNoReadyJob(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
