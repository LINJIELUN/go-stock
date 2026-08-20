package recommendation

import (
	"context"
	"testing"
	"time"

	"go-stock/backend/marketdata"
	"go-stock/backend/models"
)

type pipelineUniverse struct {
	items []ScreeningUniverseItem
	calls int
}

func (u *pipelineUniverse) Securities(context.Context, time.Time) ([]ScreeningUniverseItem, error) {
	u.calls++
	return u.items, nil
}

type pipelineSnapshotAnalyzer struct {
	now     time.Time
	bundles *InputBundleStore
	calls   int
}

func (a *pipelineSnapshotAnalyzer) Analyze(_ context.Context, job models.AIAnalysisJob) (*models.AIRecommendationSnapshot, error) {
	a.calls++
	bundle, err := a.bundles.Save(job, AnalysisInputDraft{
		StockCode: job.StockCode, ValidationBatchID: job.ValidationBatchID, DataAsOf: a.now,
		BaselinePrice: 10, ScreeningJSON: job.ScreeningJSON,
		DailyBars: []FrozenDailyBar{{TradeDate: a.now, Open: 10, High: 10.1, Low: 9.9, Close: 10, Volume: 1_000_000, Turnover: 20_000_000, Source: "pipeline-primary"}},
	})
	if err != nil {
		return nil, err
	}
	snapshot := validSnapshot(a.now)
	snapshot.SourceType = SourceAutomatic
	snapshot.Status = RecommendationStatusShadow
	snapshot.ValidationBatchID = job.ValidationBatchID
	snapshot.StockCode = job.StockCode
	snapshot.StockName = job.StockName
	snapshot.InputBundleID = bundle.ID
	snapshot.InputBundleHash = bundle.BundleHash
	snapshot.ReviewDueDate = a.now
	return snapshot, nil
}

func TestOfflinePipelineRunsValidationThroughReviewWithoutRefetch(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	if err := database.AutoMigrate(&models.AIModelUsage{}); err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, location)
	instrument := marketdata.Instrument{Code: "600000", Exchange: marketdata.ExchangeShanghai, SecurityType: marketdata.SecurityStock}
	dates, primaryBars, referenceBars := screeningValidationFixtures(instrument, now, 25)
	validation, err := marketdata.NewEndOfDayValidationService(
		fixtureCalendar{dates}, fixtureDailyBars{"pipeline-primary", withBarSource(primaryBars, "pipeline-primary")},
		fixtureDailyBars{"pipeline-reference", withBarSource(referenceBars, "pipeline-reference")},
		marketdata.ReconciliationPolicy{Location: location, CloseAbsoluteTolerance: 0.001, RequireIndependentSources: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	validationStore, _ := marketdata.NewValidationStore(database)
	universe := &pipelineUniverse{items: []ScreeningUniverseItem{{
		Identity: ScreeningIdentity{StockCode: "600000", StockName: "浦发银行"}, Instrument: instrument,
	}}}
	screening, err := NewValidatedScreeningProvider(universe, validation, validationStore, 45, 2)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := NewQuantitativeCandidateSource(screening, ScreeningPolicy{Target: 10, MinimumLiquidity: 0, MinimumDataQuality: 0})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewPostCloseScheduler(jobs, fixedTradingDay{trading: true}, candidates, testPostClosePolicy(location))
	if err != nil {
		t.Fatal(err)
	}
	bundles, _ := NewInputBundleStore(database)
	analyzer := &pipelineSnapshotAnalyzer{now: now, bundles: bundles}
	processor, _ := NewAnalysisProcessor(jobs, analyzer, time.Minute, time.Minute)
	processor.now = func() time.Time { return now }
	closes := &runtimeCloseProvider{observation: CloseObservation{
		Available: true, TradingDate: now, MarketTime: now, ClosePrice: 10.5, Source: "pipeline-close",
	}}
	reviewScheduler, _ := NewReviewScheduler(database, closes)
	runtime, err := NewShadowRuntime(scheduler, processor, jobs, reviewScheduler, 10)
	if err != nil {
		t.Fatal(err)
	}

	first, err := runtime.Tick(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !first.RunCreated || len(first.ProcessedJobs) != 1 || first.ProcessedJobs[0].Status != JobCompleted || first.CompletedReviews != 1 {
		t.Fatalf("pipeline did not complete: %+v", first)
	}
	var job models.AIAnalysisJob
	if err := database.First(&job, first.ProcessedJobs[0].JobID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := validationStore.RequirePassed(job.ValidationBatchID); err != nil {
		t.Fatalf("pipeline job lost approved validation evidence: %v", err)
	}
	var snapshot models.AIRecommendationSnapshot
	if err := database.First(&snapshot, first.ProcessedJobs[0].RecommendationID).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != RecommendationStatusShadow || snapshot.ValidationBatchID != job.ValidationBatchID {
		t.Fatalf("pipeline snapshot escaped its evidence or shadow boundary: %+v", snapshot)
	}
	reports, _ := NewShadowReportStore(database)
	report, err := reports.Build(now.Add(-time.Hour), now.Add(time.Hour), StrategyVersion, snapshot.ModelVersion, snapshot.PromptVersion)
	if err != nil {
		t.Fatal(err)
	}
	if report.GeneratedSnapshots != 1 || report.CompletedReviews != 1 || report.DirectionHitRatePercent == nil || *report.DirectionHitRatePercent != 100 ||
		report.RangeHitRatePercent == nil || *report.RangeHitRatePercent != 100 {
		t.Fatalf("pipeline result did not reach shadow evidence report: %+v", report)
	}

	second, err := runtime.Tick(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.RunCreated || len(second.ProcessedJobs) != 0 || universe.calls != 1 || analyzer.calls != 1 {
		t.Fatalf("pipeline retry re-fetched or re-analyzed frozen work: %+v universe=%d analyzer=%d", second, universe.calls, analyzer.calls)
	}
}

func withBarSource(bars []marketdata.DailyBar, source string) []marketdata.DailyBar {
	result := append([]marketdata.DailyBar(nil), bars...)
	for index := range result {
		result[index].Source = source
	}
	return result
}
