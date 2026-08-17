package recommendation

import (
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go-stock/backend/models"
	"gorm.io/gorm"
)

func testStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.MarketDataValidationBatch{}, &models.AIRecommendationSnapshot{}, &models.AIRecommendationFavorite{}, &models.AIRecommendationReview{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	batch := models.MarketDataValidationBatch{
		BatchKey: "test-validation-batch", InstrumentCode: "600000.SH", Exchange: "SH",
		SecurityType: "stock", RangeStart: now.AddDate(0, 0, -30), RangeEnd: now.AddDate(0, 0, 30),
		CalendarSource: "calendar", PrimarySource: "primary", ReferenceSource: "reference",
		ValidatedAt: now, Status: "passed", ExpectedDates: 20, ReleasedBars: 20, ReconciliationJSON: `{}`,
	}
	if err := database.Create(&batch).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	return store, database
}

func validSnapshot(now time.Time) *models.AIRecommendationSnapshot {
	return &models.AIRecommendationSnapshot{
		SourceType: SourceManual, ValidationBatchID: 1, StockCode: "600000", StockName: "浦发银行",
		RiskLabelsJSON: "[]", CompletedAt: now, DataAsOf: now, BaselinePrice: 10,
		BaselineMarketTime: now, RiseProbability: 65, ReturnRangeLow: -2,
		ReturnRangeHigh: 8, AIRecommendationIndex: 70, ScoreComponentsJSON: `{ "dataQuality": 80 }`,
		PenaltiesJSON: "[]", ModelVersion: "test-model", StrategyVersion: StrategyVersion,
		ReviewDueDate: now.AddDate(0, 0, 10), Status: "active",
	}
}

func TestCreateSnapshotRejectsUnapprovedOrMismatchedValidation(t *testing.T) {
	store, database := testStore(t)
	now := time.Now().UTC()
	snapshot := validSnapshot(now)
	if err := database.Model(&models.MarketDataValidationBatch{}).Where("id = ?", snapshot.ValidationBatchID).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSnapshot(snapshot); err == nil {
		t.Fatal("expected failed validation batch rejection")
	}
	if err := database.Model(&models.MarketDataValidationBatch{}).Where("id = ?", snapshot.ValidationBatchID).Updates(map[string]any{"status": "passed", "instrument_code": "000001.SZ"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSnapshot(snapshot); err == nil {
		t.Fatal("expected mismatched validation batch rejection")
	}
}

func TestCreateSnapshotRejectsDuplicateBatchStrategyAndSource(t *testing.T) {
	store, _ := testStore(t)
	first := validSnapshot(time.Now().UTC())
	if err := store.CreateSnapshot(first); err != nil {
		t.Fatal(err)
	}
	duplicate := validSnapshot(first.CompletedAt.Add(time.Minute))
	if err := store.CreateSnapshot(duplicate); err == nil {
		t.Fatal("expected duplicate recommendation rejection")
	}
}

func TestCreateSnapshotCreatesPendingReviewAtomically(t *testing.T) {
	store, database := testStore(t)
	snapshot := validSnapshot(time.Now().UTC())
	if err := store.CreateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	var review models.AIRecommendationReview
	if err := database.Where("recommendation_id = ?", snapshot.ID).First(&review).Error; err != nil {
		t.Fatal(err)
	}
	if review.Status != StatusPending || !review.OriginalDueDate.Equal(snapshot.ReviewDueDate) {
		t.Fatalf("unexpected pending review: %+v", review)
	}
}

func TestCreateSnapshotRejectsIncompletePrediction(t *testing.T) {
	store, database := testStore(t)
	snapshot := validSnapshot(time.Now().UTC())
	snapshot.ScoreComponentsJSON = "[]"
	if err := store.CreateSnapshot(snapshot); err == nil {
		t.Fatal("expected invalid score components to be rejected")
	}
	var count int64
	database.Model(&models.AIRecommendationSnapshot{}).Count(&count)
	if count != 0 {
		t.Fatalf("invalid snapshot was persisted")
	}
}

func TestSetFavoritePreservesAndReusesRelationship(t *testing.T) {
	store, database := testStore(t)
	now := time.Now().UTC()
	snapshot := validSnapshot(now)
	if err := store.CreateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.SetFavorite(snapshot.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetFavorite(snapshot.ID, false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetFavorite(snapshot.ID, true, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var relations []models.AIRecommendationFavorite
	if err := database.Where("recommendation_id = ?", snapshot.ID).Find(&relations).Error; err != nil {
		t.Fatal(err)
	}
	if len(relations) != 1 || !relations[0].IsFavorite || relations[0].UnfavoritedAt != nil {
		t.Fatalf("favorite history relation was not reused: %+v", relations)
	}
}

func TestSetFavoriteRejectsUnknownRecommendation(t *testing.T) {
	store, _ := testStore(t)
	if err := store.SetFavorite(99, true, time.Now()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
}
