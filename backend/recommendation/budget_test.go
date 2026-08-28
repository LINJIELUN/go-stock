package recommendation

import (
	"math"
	"testing"
	"time"

	"go-stock/backend/models"
)

func budgetFixture(t *testing.T) (*BudgetStore, []models.AIAnalysisJob, time.Time) {
	t.Helper()
	_, _, database := analysisJobStore(t)
	if err := database.AutoMigrate(&models.AIModelUsage{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)
	jobs := []models.AIAnalysisJob{
		{RunID: 90, StockCode: "600001", StockName: "甲", ValidationBatchID: 1, ScreeningJSON: `{}`, Status: JobRunning, Attempts: 1, MaxAttempts: 3, AvailableAt: now},
		{RunID: 90, StockCode: "600002", StockName: "乙", ValidationBatchID: 1, ScreeningJSON: `{}`, Status: JobRunning, Attempts: 1, MaxAttempts: 3, AvailableAt: now},
		{RunID: 90, StockCode: "600003", StockName: "丙", ValidationBatchID: 1, ScreeningJSON: `{}`, Status: JobRunning, Attempts: 1, MaxAttempts: 3, AvailableAt: now},
	}
	if err := database.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewBudgetStore(database, BudgetPolicy{SoftLimitUSD: 50, HardLimitUSD: 100, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	return store, jobs, now
}

func TestBudgetStoreEnforcesSoftAndHardMonthlyLimits(t *testing.T) {
	store, jobs, now := budgetFixture(t)
	first, err := store.Reserve(jobs[0].ID, 1, "provider", "model", 45, now)
	if err != nil || !first.Allowed || first.SoftLimitReached {
		t.Fatalf("first reservation: %+v %v", first, err)
	}
	second, err := store.Reserve(jobs[1].ID, 1, "provider", "model", 10, now)
	if err != nil || !second.Allowed || !second.SoftLimitReached || second.ProjectedUSD != 55 {
		t.Fatalf("soft-limit decision: %+v %v", second, err)
	}
	blocked, err := store.Reserve(jobs[2].ID, 1, "provider", "model", 46, now)
	if err != nil || blocked.Allowed || blocked.ProjectedUSD != 101 {
		t.Fatalf("hard-limit decision: %+v %v", blocked, err)
	}
}

func TestBudgetStoreSettlesIdempotentlyAndUsesActualCost(t *testing.T) {
	store, jobs, now := budgetFixture(t)
	reservation, _ := store.Reserve(jobs[0].ID, 1, "provider", "model", 45, now)
	if err := store.Settle(reservation.UsageID, 40, 1000, 200, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Settle(reservation.UsageID, 40, 1000, 200, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("identical settlement was not idempotent: %v", err)
	}
	second, err := store.Reserve(jobs[1].ID, 1, "provider", "model", 60, now.Add(3*time.Minute))
	if err != nil || !second.Allowed || second.ProjectedUSD != 100 || !second.SoftLimitReached {
		t.Fatalf("actual settled cost was not used: %+v %v", second, err)
	}
}

func TestBudgetStoreReleaseReturnsUnusedReservation(t *testing.T) {
	store, jobs, now := budgetFixture(t)
	reservation, _ := store.Reserve(jobs[0].ID, 1, "provider", "model", 80, now)
	if err := store.Release(reservation.UsageID); err != nil {
		t.Fatal(err)
	}
	decision, err := store.Reserve(jobs[1].ID, 1, "provider", "model", 100, now)
	if err != nil || !decision.Allowed || decision.ProjectedUSD != 100 {
		t.Fatalf("released reservation still consumed budget: %+v %v", decision, err)
	}
}

func TestBudgetStoreRejectsInvalidMoneyAndNonRunningAttempt(t *testing.T) {
	store, jobs, now := budgetFixture(t)
	if _, err := store.Reserve(jobs[0].ID, 1, "provider", "model", math.NaN(), now); err == nil {
		t.Fatal("expected invalid estimate rejection")
	}
	if _, err := store.Reserve(jobs[0].ID, 2, "provider", "model", 1, now); err == nil {
		t.Fatal("expected stale attempt rejection")
	}
}
