package recommendation

import (
	"context"
	"strings"
	"testing"
	"time"

	"go-stock/backend/models"
)

type runtimeCloseProvider struct {
	observation CloseObservation
	calls       int
}

func (p *runtimeCloseProvider) FirstValidClose(context.Context, string, time.Time, time.Time) (CloseObservation, error) {
	p.calls++
	return p.observation, nil
}

func TestShadowRuntimeCompletesOfflineScheduleAnalyzeReviewCycle(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, location)
	candidates := &fixedCandidates{values: []AnalysisCandidate{testCandidate()}}
	scheduler, err := NewPostCloseScheduler(jobs, fixedTradingDay{trading: true}, candidates, testPostClosePolicy(location))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(now)
	snapshot.SourceType = SourceAutomatic
	snapshot.Status = RecommendationStatusShadow
	snapshot.ReviewDueDate = now
	analyzer := &fakeJobAnalyzer{snapshot: snapshot}
	processor, err := NewAnalysisProcessor(jobs, analyzer, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	processor.now = func() time.Time { return now }
	closes := &runtimeCloseProvider{observation: CloseObservation{
		Available: true, TradingDate: now, MarketTime: now, ClosePrice: 10.5, Source: "fixture-close",
	}}
	reviews, err := NewReviewScheduler(database, closes)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewShadowRuntime(scheduler, processor, jobs, reviews, 10)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Tick(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RunCreated || result.RunID == 0 || len(result.ProcessedJobs) != 1 || result.ProcessedJobs[0].Status != JobCompleted || result.CompletedReviews != 1 {
		t.Fatalf("unexpected runtime result: %+v", result)
	}
	var storedSnapshot models.AIRecommendationSnapshot
	if err := database.First(&storedSnapshot, result.ProcessedJobs[0].RecommendationID).Error; err != nil {
		t.Fatal(err)
	}
	if storedSnapshot.Status != RecommendationStatusShadow {
		t.Fatalf("automatic runtime result escaped shadow mode: %+v", storedSnapshot)
	}
	var review models.AIRecommendationReview
	if err := database.Where("recommendation_id = ?", storedSnapshot.ID).First(&review).Error; err != nil {
		t.Fatal(err)
	}
	if review.Status != ReviewStatusCompleted || review.ActualReturnPercent == nil || *review.ActualReturnPercent != 5 || closes.calls != 1 {
		t.Fatalf("review was not completed from fixture close: %+v calls=%d", review, closes.calls)
	}

	second, err := runtime.Tick(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.RunCreated || len(second.ProcessedJobs) != 0 || second.CompletedReviews != 0 || analyzer.calls != 1 {
		t.Fatalf("runtime retry was not idempotent: %+v analyzer calls=%d", second, analyzer.calls)
	}

	loopContext, cancelLoop := context.WithCancel(context.Background())
	observations := 0
	if err := runtime.RunObserved(loopContext, time.Hour, func() time.Time { return now.Add(2 * time.Minute) }, func(observed RuntimeTickResult, tickErr error) {
		observations++
		if tickErr != nil || observed.RunID != result.RunID {
			t.Errorf("unexpected observed tick: result=%+v err=%v", observed, tickErr)
		}
		cancelLoop()
	}); err != nil {
		t.Fatal(err)
	}
	if observations != 1 {
		t.Fatalf("expected one immediate runtime observation, got %d", observations)
	}
}

func TestShadowRuntimeRequiresCompleteComposition(t *testing.T) {
	if _, err := NewShadowRuntime(nil, nil, nil, nil, 0); err == nil {
		t.Fatal("expected incomplete runtime rejection")
	}
}

func TestShadowRuntimeProcessesDurableJobsWhenSchedulingFails(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	queuedAt := time.Date(2026, 8, 18, 16, 0, 0, 0, location)
	if _, created, err := jobs.CreateRun(queuedAt, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, queuedAt); err != nil || !created {
		t.Fatalf("create durable work: created=%v err=%v", created, err)
	}

	tickAt := queuedAt.Add(24 * time.Hour)
	calendar := &failingTradingDay{}
	scheduler, err := NewPostCloseScheduler(jobs, calendar, &fixedCandidates{values: []AnalysisCandidate{testCandidate()}}, testPostClosePolicy(location))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(tickAt)
	snapshot.SourceType = SourceAutomatic
	analyzer := &fakeJobAnalyzer{snapshot: snapshot}
	processor, err := NewAnalysisProcessor(jobs, analyzer, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	processor.now = func() time.Time { return tickAt }
	reviews, err := NewReviewScheduler(database, &runtimeCloseProvider{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewShadowRuntime(scheduler, processor, jobs, reviews, 10)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runtime.Tick(context.Background(), tickAt)
	if err == nil || !strings.Contains(err.Error(), "calendar unavailable") {
		t.Fatalf("expected surfaced scheduling failure, got %v", err)
	}
	if len(result.ProcessedJobs) != 1 || result.ProcessedJobs[0].Status != JobCompleted || analyzer.calls != 1 {
		t.Fatalf("durable job was blocked by scheduling failure: result=%+v calls=%d", result, analyzer.calls)
	}
}
