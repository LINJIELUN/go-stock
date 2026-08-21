package recommendation

import (
	"testing"
	"time"

	"go-stock/backend/models"
)

func TestRecommendationViewStoreProjectsFavoritesAndReviews(t *testing.T) {
	_, _, database := analysisJobStore(t)
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, time.UTC)
	snapshots := []models.AIRecommendationSnapshot{
		{SourceType: SourceAutomatic, StockCode: "600519", StockName: "贵州茅台", RiskLabelsJSON: `[]`, CompletedAt: now, DataAsOf: now, BaselinePrice: 10, RiseProbability: 68, ReturnRangeLow: 2, ReturnRangeHigh: 7, AIRecommendationIndex: 76, ProbabilityNotice: ProbabilityNotice, ReviewDueDate: now.AddDate(0, 0, 7), Status: RecommendationStatusShadow},
		{SourceType: SourceManual, StockCode: "300750", StockName: "宁德时代", RiskLabelsJSON: `["NEW_STOCK"]`, CompletedAt: now.Add(-time.Hour), DataAsOf: now, BaselinePrice: 20, RiseProbability: 60, ReturnRangeLow: -2, ReturnRangeHigh: 5, AIRecommendationIndex: 64, ProbabilityNotice: ProbabilityNotice, ReviewDueDate: now.AddDate(0, 0, 7), Status: RecommendationStatusShadow},
	}
	for index := range snapshots {
		if err := database.Create(&snapshots[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	actual := 3.82
	direction, interval := true, true
	if err := database.Create(&models.AIRecommendationFavorite{RecommendationID: snapshots[0].ID, IsFavorite: true, FavoritedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.AIRecommendationReview{RecommendationID: snapshots[0].ID, OriginalDueDate: now, ActualReturnPercent: &actual, DirectionHit: &direction, RangeHit: &interval, Status: ReviewStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewRecommendationViewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := store.List(20, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || !cards[0].IsFavorite || cards[0].ActualReturnPercent == nil || *cards[0].ActualReturnPercent != actual || cards[1].RiskLabels[0] != "NEW_STOCK" {
		t.Fatalf("unexpected card projection: %+v", cards)
	}
	favorites, err := store.List(20, true)
	if err != nil || len(favorites) != 1 || favorites[0].ID != snapshots[0].ID {
		t.Fatalf("unexpected favorite projection: %+v %v", favorites, err)
	}
}

func TestRecommendationViewStoreRejectsInvalidLimit(t *testing.T) {
	_, _, database := analysisJobStore(t)
	store, _ := NewRecommendationViewStore(database)
	if _, err := store.List(0, false); err == nil {
		t.Fatal("expected invalid limit rejection")
	}
}
