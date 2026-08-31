package recommendation

import (
	"errors"
	"testing"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

func analysisJobStore(t *testing.T) (*JobStore, *Store, *gorm.DB) {
	t.Helper()
	recommendations, database := testStore(t)
	if err := database.AutoMigrate(&models.AIAnalysisRun{}, &models.AIAnalysisJob{}); err != nil {
		t.Fatal(err)
	}
	jobs, err := NewJobStore(database)
	if err != nil {
		t.Fatal(err)
	}
	return jobs, recommendations, database
}

func testCandidate() AnalysisCandidate {
	return AnalysisCandidate{StockCode: "600000", StockName: "浦发银行", ValidationBatchID: 1, ScreeningScore: 80, ScreeningJSON: `{}`}
}

func TestAnalysisRunCreationIsAtomicAndIdempotent(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	first, created, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now)
	if err != nil || !created {
		t.Fatalf("create run: created=%v err=%v", created, err)
	}
	second, created, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now)
	if err != nil || created || first.ID != second.ID {
		t.Fatalf("idempotent create failed: run=%+v created=%v err=%v", second, created, err)
	}
	changed := testCandidate()
	changed.ValidationBatchID = 999
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{changed}, 3, now); err == nil {
		t.Fatal("existing run accepted a different candidate evidence set")
	}
	var count int64
	if err := database.Model(&models.AIAnalysisJob{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("unexpected job count %d: %v", count, err)
	}
}

func TestAnalysisRunRejectsUnapprovedCandidateAtomically(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	now := time.Now().UTC()
	if err := database.Model(&models.MarketDataValidationBatch{}).Where("id = 1").Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now); err == nil {
		t.Fatal("expected unapproved candidate rejection")
	}
	var count int64
	database.Model(&models.AIAnalysisRun{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid run was partially persisted")
	}
}

func TestAnalysisJobCompletesWithMatchingRecommendation(t *testing.T) {
	jobs, recommendations, database := analysisJobStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	run, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimNext(now, time.Minute)
	if err != nil || job.Attempts != 1 || job.Status != JobRunning {
		t.Fatalf("claim job: %+v %v", job, err)
	}
	snapshot := validSnapshot(now)
	if err := recommendations.CreateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Complete(job.ID, snapshot.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var storedRun models.AIAnalysisRun
	if err := database.First(&storedRun, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != RunCompleted || storedRun.CompletedJobs != 1 || storedRun.CompletedAt == nil {
		t.Fatalf("run progress not completed: %+v", storedRun)
	}
	if err := jobs.Complete(job.ID, snapshot.ID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("idempotent completion failed: %v", err)
	}
}

func TestAnalysisJobRetriesThenExhaustsExpiredLease(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 2, now); err != nil {
		t.Fatal(err)
	}
	first, _ := jobs.ClaimNext(now, time.Minute)
	if err := jobs.Fail(first.ID, "temporary model error", now.Add(time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.ClaimNext(now, time.Minute); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("retry became available too early: %v", err)
	}
	second, err := jobs.ClaimNext(now.Add(time.Minute), time.Minute)
	if err != nil || second.Attempts != 2 {
		t.Fatalf("retry claim failed: %+v %v", second, err)
	}
	recovered, err := jobs.RecoverExpired(now.Add(2 * time.Minute))
	if err != nil || recovered != 1 {
		t.Fatalf("recover expired job: %d %v", recovered, err)
	}
	var stored models.AIAnalysisJob
	database.First(&stored, second.ID)
	if stored.Status != JobFailed || stored.LeaseExpiresAt != nil {
		t.Fatalf("final attempt was not exhausted: %+v", stored)
	}
}
