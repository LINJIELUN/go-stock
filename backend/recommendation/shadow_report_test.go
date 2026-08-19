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
		shadowReportSnapshot(1, "600000", RecommendationStatusShadow, now.Add(-48*time.Hour)),
		shadowReportSnapshot(2, "600001", RecommendationStatusShadow, now.Add(-24*time.Hour)),
		shadowReportSnapshot(3, "600002", RecommendationStatusActive, now.Add(-24*time.Hour)),
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
	report, err := store.Build(now.Add(-72*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if report.GeneratedSnapshots != 2 || report.CompletedReviews != 2 || report.PendingReviews != 0 || report.DirectionHitRatePercent == nil ||
		*report.DirectionHitRatePercent != 50 || report.RangeHitRatePercent == nil || *report.RangeHitRatePercent != 50 ||
		report.MeanActualReturnPercent == nil || *report.MeanActualReturnPercent != 0.5 || report.MeanOutsideDeviation == nil ||
		*report.MeanOutsideDeviation != 1.5 || report.SettledModelCostUSD != 0.25 || report.CommittedModelCostUSD != 0.65 || report.UncertainModelUsageCount != 1 {
		t.Fatalf("unexpected shadow report: %+v", report)
	}
	if err := database.Model(&models.AIModelUsage{}).Where("id = ?", usages[0].ID).Update("actual_cost_usd", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Build(now.Add(-72*time.Hour), now); err == nil {
		t.Fatal("settled usage without actual cost must make the report fail closed")
	}
}

func shadowReportSnapshot(batchID uint, stockCode, status string, completedAt time.Time) models.AIRecommendationSnapshot {
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
		ModelVersion:        "test-model",
		StrategyVersion:     StrategyVersion,
		ReviewDueDate:       completedAt.AddDate(0, 0, 10),
		Status:              status,
	}
}

func TestShadowReportLeavesRatesUnavailableWithoutCompletedReviews(t *testing.T) {
	_, database := testStore(t)
	store, _ := NewShadowReportStore(database)
	now := time.Now().UTC()
	report, err := store.Build(now.Add(-time.Hour), now)
	if err != nil || report.DirectionHitRatePercent != nil || report.RangeHitRatePercent != nil {
		t.Fatalf("empty report must not imply zero hit rate: %+v %v", report, err)
	}
	if _, err := store.Build(now, now); err == nil {
		t.Fatal("expected invalid report window rejection")
	}
}
