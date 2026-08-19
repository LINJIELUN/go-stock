package recommendation

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-stock/backend/models"
)

type fixedTradingDay struct{ trading bool }

func (f fixedTradingDay) IsTradingDay(context.Context, time.Time) (bool, error) {
	return f.trading, nil
}

type fixedCandidates struct {
	values []AnalysisCandidate
	calls  int
}

func (f *fixedCandidates) Candidates(context.Context, time.Time) ([]AnalysisCandidate, error) {
	f.calls++
	return f.values, nil
}

type fakeJobAnalyzer struct {
	snapshot *models.AIRecommendationSnapshot
	err      error
	calls    int
}

func (f *fakeJobAnalyzer) Analyze(context.Context, models.AIAnalysisJob) (*models.AIRecommendationSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestPostCloseSchedulerHonorsCutoffTradingDayAndIdempotency(t *testing.T) {
	jobs, _, _ := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	source := &fixedCandidates{values: []AnalysisCandidate{testCandidate()}}
	scheduler, err := NewPostCloseScheduler(jobs, fixedTradingDay{trading: true}, source, location, StrategyVersion, 3)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 8, 17, 15, 29, 0, 0, location)
	if run, created, err := scheduler.Tick(context.Background(), before); err != nil || run != nil || created || source.calls != 0 {
		t.Fatalf("scheduler ran before cutoff: run=%+v created=%v calls=%d err=%v", run, created, source.calls, err)
	}
	after := before.Add(time.Minute)
	first, created, err := scheduler.Tick(context.Background(), after)
	if err != nil || !created || first == nil {
		t.Fatalf("scheduler did not create run: %+v %v %v", first, created, err)
	}
	second, created, err := scheduler.Tick(context.Background(), after.Add(time.Hour))
	if err != nil || created || second.ID != first.ID || source.calls != 1 {
		t.Fatalf("scheduler duplicated or re-fetched run: %+v %v calls=%d %v", second, created, source.calls, err)
	}
}

func TestPostCloseSchedulerSkipsNonTradingDay(t *testing.T) {
	jobs, _, _ := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	source := &fixedCandidates{values: []AnalysisCandidate{testCandidate()}}
	scheduler, _ := NewPostCloseScheduler(jobs, fixedTradingDay{trading: false}, source, location, StrategyVersion, 3)
	now := time.Date(2026, 8, 16, 16, 0, 0, 0, location)
	if run, created, err := scheduler.Tick(context.Background(), now); err != nil || run != nil || created || source.calls != 0 {
		t.Fatalf("non-trading day was scheduled: %+v %v %v", run, created, err)
	}
}

func TestAnalysisProcessorPersistsSnapshotAndCompletesJobAtomically(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now); err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(now)
	snapshot.SourceType = SourceAutomatic
	analyzer := &fakeJobAnalyzer{snapshot: snapshot}
	processor, _ := NewAnalysisProcessor(jobs, analyzer, time.Minute, time.Minute)
	processor.now = func() time.Time { return now }
	result, err := processor.ProcessNext(context.Background())
	if err != nil || result.Status != JobCompleted || result.RecommendationID == 0 {
		t.Fatalf("process result: %+v %v", result, err)
	}
	var snapshots, reviews int64
	database.Model(&models.AIRecommendationSnapshot{}).Count(&snapshots)
	database.Model(&models.AIRecommendationReview{}).Count(&reviews)
	if snapshots != 1 || reviews != 1 || analyzer.calls != 1 {
		t.Fatalf("atomic output missing: snapshots=%d reviews=%d calls=%d", snapshots, reviews, analyzer.calls)
	}
}

func TestAnalysisProcessorRecordsModelFailureForRetry(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 2, now); err != nil {
		t.Fatal(err)
	}
	analyzer := &fakeJobAnalyzer{err: errors.New("model unavailable")}
	processor, _ := NewAnalysisProcessor(jobs, analyzer, time.Minute, 5*time.Minute)
	processor.now = func() time.Time { return now }
	result, err := processor.ProcessNext(context.Background())
	if err != nil || result.Status != JobRetry || result.AnalysisError == "" {
		t.Fatalf("failure was not persisted: %+v %v", result, err)
	}
	var job models.AIAnalysisJob
	database.First(&job, result.JobID)
	if job.LastError != "model unavailable" || !job.AvailableAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("unexpected retry job: %+v", job)
	}
}
