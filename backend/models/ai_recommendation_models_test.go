package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAIRecommendationModelsMigrateWithRequiredIndexes(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models := []any{
		&AIRecommendationSnapshot{},
		&AIRecommendationFavorite{},
		&AIRecommendationReview{},
		&MarketDataValidationBatch{},
		&AIAnalysisRun{},
		&AIAnalysisJob{},
	}
	if err := database.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if !database.Migrator().HasTable(model) {
			t.Fatalf("missing migrated table for %T", model)
		}
	}
	if !database.Migrator().HasIndex(&AIRecommendationFavorite{}, "idx_ai_recommendation_favorites_recommendation_id") {
		t.Fatal("favorite recommendation unique index was not created")
	}
	if !database.Migrator().HasIndex(&AIRecommendationReview{}, "idx_ai_recommendation_reviews_recommendation_id") {
		t.Fatal("review recommendation unique index was not created")
	}
	if !database.Migrator().HasIndex(&AIRecommendationSnapshot{}, "idx_recommendation_validation_strategy") {
		t.Fatal("recommendation validation/strategy idempotency index was not created")
	}
	if !database.Migrator().HasIndex(&MarketDataValidationBatch{}, "idx_market_data_validation_batches_batch_key") {
		t.Fatal("market data validation batch unique index was not created")
	}
	if !database.Migrator().HasIndex(&AIAnalysisRun{}, "idx_analysis_run_date_strategy") {
		t.Fatal("analysis run date/strategy idempotency index was not created")
	}
	if !database.Migrator().HasIndex(&AIAnalysisJob{}, "idx_analysis_run_stock") {
		t.Fatal("analysis run/stock unique index was not created")
	}
}
