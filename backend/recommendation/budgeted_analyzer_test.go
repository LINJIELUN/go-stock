package recommendation

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-stock/backend/models"
)

type fakePaidClient struct {
	plan   ModelPlan
	result PaidAnalysisResult
	err    error
	calls  int
}

func (f *fakePaidClient) Plan(context.Context, models.AIAnalysisJob) (ModelPlan, error) {
	return f.plan, nil
}
func (f *fakePaidClient) Analyze(context.Context, models.AIAnalysisJob, ModelPlan) (PaidAnalysisResult, error) {
	f.calls++
	return f.result, f.err
}

func TestBudgetedAnalyzerSettlesSuccessfulPaidRequest(t *testing.T) {
	budget, jobs, now := budgetFixture(t)
	snapshot := validSnapshot(now)
	snapshot.SourceType = SourceAutomatic
	client := &fakePaidClient{plan: ModelPlan{Provider: "provider", Model: "model", EstimatedCostUSD: 2},
		result: PaidAnalysisResult{Snapshot: snapshot, RequestSent: true, ActualCostKnown: true, ActualCostUSD: 1.5, InputTokens: 1000, OutputTokens: 200}}
	analyzer, _ := NewBudgetedAnalyzer(budget, client)
	analyzer.now = func() time.Time { return now }
	result, err := analyzer.Analyze(context.Background(), jobs[0])
	if err != nil || result != snapshot || client.calls != 1 {
		t.Fatalf("paid analysis failed: %+v %v", result, err)
	}
	var usage models.AIModelUsage
	budget.db.First(&usage)
	if usage.Status != UsageSettled || usage.ActualCostUSD == nil || *usage.ActualCostUSD != 1.5 || usage.InputTokens != 1000 {
		t.Fatalf("usage was not settled: %+v", usage)
	}
}

func TestBudgetedAnalyzerReleasesOnlyUnsentFailure(t *testing.T) {
	budget, jobs, now := budgetFixture(t)
	client := &fakePaidClient{plan: ModelPlan{Provider: "provider", Model: "model", EstimatedCostUSD: 2}, err: errors.New("connection setup failed")}
	analyzer, _ := NewBudgetedAnalyzer(budget, client)
	analyzer.now = func() time.Time { return now }
	if _, err := analyzer.Analyze(context.Background(), jobs[0]); err == nil {
		t.Fatal("expected client failure")
	}
	var usage models.AIModelUsage
	budget.db.First(&usage)
	if usage.Status != UsageReleased {
		t.Fatalf("unsent reservation was not released: %+v", usage)
	}
}

func TestBudgetedAnalyzerKeepsAmbiguousChargeCommitted(t *testing.T) {
	budget, jobs, now := budgetFixture(t)
	client := &fakePaidClient{plan: ModelPlan{Provider: "provider", Model: "model", EstimatedCostUSD: 2},
		result: PaidAnalysisResult{RequestSent: true}, err: errors.New("response lost")}
	analyzer, _ := NewBudgetedAnalyzer(budget, client)
	analyzer.now = func() time.Time { return now }
	if _, err := analyzer.Analyze(context.Background(), jobs[0]); err == nil {
		t.Fatal("expected ambiguous request failure")
	}
	var usage models.AIModelUsage
	budget.db.First(&usage)
	if usage.Status != UsageUncertain {
		t.Fatalf("ambiguous cost was incorrectly released: %+v", usage)
	}
}

func TestProcessorDefersHardBudgetWithoutConsumingAttempt(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	if err := database.AutoMigrate(&models.AIModelUsage{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now); err != nil {
		t.Fatal(err)
	}
	budget, _ := NewBudgetStore(database, BudgetPolicy{SoftLimitUSD: 0, HardLimitUSD: 0, Location: time.UTC})
	client := &fakePaidClient{plan: ModelPlan{Provider: "provider", Model: "model", EstimatedCostUSD: 1}}
	analyzer, _ := NewBudgetedAnalyzer(budget, client)
	analyzer.now = func() time.Time { return now }
	processor, _ := NewAnalysisProcessor(jobs, analyzer, time.Minute, time.Minute)
	processor.now = func() time.Time { return now }
	result, err := processor.ProcessNext(context.Background())
	if err != nil || result.Status != JobRetry || result.AnalysisError != ErrHardBudgetExceeded.Error() {
		t.Fatalf("hard budget was not deferred: %+v %v", result, err)
	}
	var job models.AIAnalysisJob
	database.First(&job, result.JobID)
	if job.Attempts != 0 || job.AvailableAt.Month() != time.September || client.calls != 0 {
		t.Fatalf("budget block consumed attempt or called provider: %+v calls=%d", job, client.calls)
	}
}
