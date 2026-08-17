package marketdata

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go-stock/backend/models"
	"gorm.io/gorm"
)

func validationTestStore(t *testing.T) *ValidationStore {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.MarketDataValidationBatch{}); err != nil {
		t.Fatal(err)
	}
	store, err := NewValidationStore(database)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func persistedValidationResult(t *testing.T, passed bool) EndOfDayValidationResult {
	t.Helper()
	instrument, dates, primary, _, _ := validationFixture(t)
	reconciliation := ReconciliationResult{
		ExpectedDates: 2, PrimaryCoveredDates: 2, ReferenceCoveredDates: 2,
		ComparedDates: 2, TradeDateCoverageRate: 1,
	}
	validatedBars := primary
	if !passed {
		reconciliation.CloseMismatches = 1
		reconciliation.CloseMismatchRate = 0.5
		reconciliation.Issues = []ReconciliationIssue{{Code: "close_mismatch", TradeDate: "2026-08-14"}}
		validatedBars = nil
	}
	return EndOfDayValidationResult{
		Instrument: instrument, Start: dates[0], End: dates[1],
		CalendarSource: "calendar", PrimarySource: "primary", ReferenceSource: "reference",
		ValidatedAt:    time.Date(2026, 8, 14, 18, 30, 0, 0, time.UTC),
		Reconciliation: reconciliation, ValidatedBars: validatedBars,
	}
}

func TestValidationStorePersistsPassedEvidenceIdempotently(t *testing.T) {
	store := validationTestStore(t)
	result := persistedValidationResult(t, true)
	first, err := store.Save(result)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(result)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || first.ID != second.ID || first.Status != ValidationStatusPassed || first.ReleasedBars != 2 {
		t.Fatalf("unexpected persisted validation batches: first=%+v second=%+v", first, second)
	}
	approved, err := store.RequirePassed(first.ID)
	if err != nil || approved.ID != first.ID {
		t.Fatalf("passed batch was not approved: batch=%+v err=%v", approved, err)
	}
}

func TestValidationStorePersistsFailureButBlocksAnalysis(t *testing.T) {
	store := validationTestStore(t)
	batch, err := store.Save(persistedValidationResult(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != ValidationStatusFailed || batch.ReleasedBars != 0 || batch.ReconciliationJSON == "" {
		t.Fatalf("unexpected failed batch: %+v", batch)
	}
	if _, err := store.RequirePassed(batch.ID); err == nil {
		t.Fatal("failed validation batch was approved for analysis")
	}
}

func TestValidationStoreRejectsReleasedBarsFromFailedEvidence(t *testing.T) {
	store := validationTestStore(t)
	result := persistedValidationResult(t, false)
	result.ValidatedBars = []DailyBar{{}}
	if _, err := store.Save(result); err == nil {
		t.Fatal("expected inconsistent failed result rejection")
	}
}
