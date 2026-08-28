package recommendation

import (
	"testing"
	"time"

	"go-stock/backend/models"
)

func TestShadowReportBuildsReviewAndCostMetricsWithoutPromotionDecision(t *testing.T) {
	_, database := testStore(t)
	if err := database.AutoMigrate(&models.AIAnalysisJob{}, &models.AIModelUsage{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	snapshots := []models.AIRecommendationSnapshot{
		shadowReportSnapshot(1, "600000", StrategyVersion, "test-model", "test-prompt", RecommendationStatusShadow, now.Add(-48*time.Hour)),
		shadowReportSnapshot(2, "600001", StrategyVersion, "test-model", "test-prompt", RecommendationStatusShadow, now.Add(-24*time.Hour)),
		shadowReportSnapshot(3, "600002", StrategyVersion, "test-model", "test-prompt", RecommendationStatusActive, now.Add(-24*time.Hour)),
		shadowReportSnapshot(4, "600003", "older-strategy", "test-model", "test-prompt", RecommendationStatusShadow, now.Add(-24*time.Hour)),
		shadowReportSnapshot(5, "600004", StrategyVersion, "other-model", "test-prompt", RecommendationStatusShadow, now.Add(-24*time.Hour)),
		shadowReportSnapshot(6, "600005", StrategyVersion, "test-model", "other-prompt", RecommendationStatusShadow, now.Add(-24*time.Hour)),
	}
	for index := range snapshots {
		if err := database.Create(&snapshots[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	trueValue, falseValue := true, false
	returnOne, returnTwo, deviationZero, deviationThree := 2.0, -1.0, 0.0, 3.0
	reviews := []models.AIRecommendationReview{
		{RecommendationID: snapshots[0].ID, Status: ReviewStatusCompleted, DirectionHit: &trueValue, RangeHit: &trueValue, ActualReturnPercent: &returnOne, OutsideRangeDeviation: &deviationZero},
		{RecommendationID: snapshots[1].ID, Status: ReviewStatusCompleted, DirectionHit: &falseValue, RangeHit: &falseValue, ActualReturnPercent: &returnTwo, OutsideRangeDeviation: &deviationThree},
	}
	if err := database.Create(&reviews).Error; err != nil {
		t.Fatal(err)
	}
	jobs := []models.AIAnalysisJob{
		{RunID: 1, StockCode: "600000", StockName: "600000", ScreeningJSON: "{}", Status: JobCompleted, MaxAttempts: 1, AvailableAt: now, RecommendationID: &snapshots[0].ID},
		{RunID: 1, StockCode: "600001", StockName: "600001", ScreeningJSON: "{}", Status: JobCompleted, MaxAttempts: 1, AvailableAt: now, RecommendationID: &snapshots[1].ID},
	}
	if err := database.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	actual := 0.25
	usages := []models.AIModelUsage{
		{JobID: jobs[0].ID, Attempt: 1, Provider: "test", ModelName: "test-model", Status: UsageSettled, ActualCostUSD: &actual, EstimatedCostUSD: 0.3, ReservedAt: now},
		{JobID: jobs[1].ID, Attempt: 1, Provider: "test", ModelName: "test-model", Status: UsageUncertain, EstimatedCostUSD: 0.4, ReservedAt: now},
	}
	if err := database.Create(&usages).Error; err != nil {
		t.Fatal(err)
	}
	store, _ := NewShadowReportStore(database)
	report, err := store.Build(now.Add(-72*time.Hour), now, StrategyVersion, "test-model", "test-prompt")
	if err != nil {
		t.Fatal(err)
	}
	if report.StrategyVersion != StrategyVersion || report.ModelVersion != "test-model" || report.PromptVersion != "test-prompt" ||
		report.GeneratedSnapshots != 2 || report.CompletedReviews != 2 || report.PendingReviews != 0 || report.DirectionHitRatePercent == nil ||
		*report.DirectionHitRatePercent != 50 || report.RangeHitRatePercent == nil || *report.RangeHitRatePercent != 50 ||
		report.MeanActualReturnPercent == nil || *report.MeanActualReturnPercent != 0.5 || report.MeanOutsideDeviation == nil ||
		*report.MeanOutsideDeviation != 1.5 || report.SettledModelCostUSD != 0.25 || report.CommittedModelCostUSD != 0.65 || report.UncertainModelUsageCount != 1 {
		t.Fatalf("unexpected shadow report: %+v", report)
	}
	if err := database.Model(&models.AIModelUsage{}).Where("id = ?", usages[0].ID).Update("actual_cost_usd", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Build(now.Add(-72*time.Hour), now, StrategyVersion, "test-model", "test-prompt"); err == nil {
		t.Fatal("settled usage without actual cost must make the report fail closed")
	}
}

func shadowReportSnapshot(batchID uint, stockCode, strategyVersion, modelVersion, promptVersion, status string, completedAt time.Time) models.AIRecommendationSnapshot {
	return models.AIRecommendationSnapshot{
		SourceType:          SourceAutomatic,
		ValidationBatchID:   batchID,
		StockCode:           stockCode,
		StockName:           stockCode,
		CompletedAt:         completedAt,
		DataAsOf:            completedAt,
		BaselinePrice:       10,
		BaselineMarketTime:  completedAt,
		ScoreComponentsJSON: "{}",
		ModelVersion:        modelVersion,
		PromptVersion:       promptVersion,
		StrategyVersion:     strategyVersion,
		ReviewDueDate:       completedAt.AddDate(0, 0, 10),
		Status:              status,
	}
}

func TestShadowReportLeavesRatesUnavailableWithoutCompletedReviews(t *testing.T) {
	_, database := testStore(t)
	store, _ := NewShadowReportStore(database)
	now := time.Now().UTC()
	report, err := store.Build(now.Add(-time.Hour), now, StrategyVersion, "test-model", "test-prompt")
	if err != nil || report.DirectionHitRatePercent != nil || report.RangeHitRatePercent != nil {
		t.Fatalf("empty report must not imply zero hit rate: %+v %v", report, err)
	}
	if _, err := store.Build(now, now, StrategyVersion, "test-model", "test-prompt"); err == nil {
		t.Fatal("expected invalid report window rejection")
	}
	if _, err := store.Build(now.Add(-time.Hour), now, " ", "test-model", "test-prompt"); err == nil {
		t.Fatal("expected incomplete cohort identity rejection")
	}
	if _, err := store.Build(now.Add(-time.Hour), now, StrategyVersion, "test-model", " "); err == nil {
		t.Fatal("expected incomplete cohort identity rejection")
	}
}

func TestShadowReportListsComparableCohorts(t *testing.T) {
	_, database := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	snapshots := []models.AIRecommendationSnapshot{
		shadowReportSnapshot(11, "600010", StrategyVersion, "model-a", "prompt-a", RecommendationStatusShadow, now.Add(-3*time.Hour)),
		shadowReportSnapshot(12, "600011", StrategyVersion, "model-a", "prompt-a", RecommendationStatusShadow, now.Add(-2*time.Hour)),
		shadowReportSnapshot(13, "600012", StrategyVersion, "model-b", "prompt-a", RecommendationStatusShadow, now.Add(-time.Hour)),
		shadowReportSnapshot(14, "600013", StrategyVersion, "model-c", "prompt-a", RecommendationStatusActive, now.Add(-time.Hour)),
	}
	if err := database.Create(&snapshots).Error; err != nil {
		t.Fatal(err)
	}
	store, _ := NewShadowReportStore(database)
	cohorts, err := store.ListCohorts(now.Add(-4*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(cohorts) != 2 || cohorts[0].ModelVersion != "model-b" || cohorts[0].GeneratedSnapshots != 1 ||
		cohorts[1].ModelVersion != "model-a" || cohorts[1].GeneratedSnapshots != 2 ||
		!cohorts[1].FirstCompletedAt.Equal(now.Add(-3*time.Hour)) || !cohorts[1].LastCompletedAt.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("unexpected shadow cohorts: %+v", cohorts)
	}
	if _, err := store.ListCohorts(now, now); err == nil {
		t.Fatal("expected invalid cohort window rejection")
	}
}
