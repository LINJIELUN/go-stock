package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go-stock/backend/models"
	"go-stock/backend/recommendation"
	"gorm.io/gorm"
)

func TestBuildAIShadowReportRequiresExplicitRFC3339Window(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:app-shadow-report?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&models.AIRecommendationSnapshot{},
		&models.AIRecommendationReview{},
		&models.AIAnalysisJob{},
		&models.AIModelUsage{},
	); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	end := start.AddDate(0, 0, 7)
	report, err := buildAIShadowReport(database, start.Format(time.RFC3339), end.Format(time.RFC3339), recommendation.StrategyVersion, "test-model", "test-prompt")
	if err != nil {
		t.Fatal(err)
	}
	if !report.WindowStart.Equal(start) || !report.WindowEnd.Equal(end) || report.StrategyVersion != recommendation.StrategyVersion ||
		report.ModelVersion != "test-model" || report.PromptVersion != "test-prompt" {
		t.Fatalf("unexpected normalized report window: %+v", report)
	}
	if _, err := buildAIShadowReport(database, "2026-08-01", end.Format(time.RFC3339), recommendation.StrategyVersion, "test-model", "test-prompt"); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected explicit RFC3339 start rejection, got %v", err)
	}
	cohorts, err := listAIShadowCohorts(database, start.Format(time.RFC3339), end.Format(time.RFC3339))
	if err != nil || len(cohorts) != 0 {
		t.Fatalf("unexpected empty cohort listing: %+v %v", cohorts, err)
	}
}

func TestBuildAIShadowReportRejectsUnavailableDatabase(t *testing.T) {
	start := time.Now().UTC()
	if _, err := buildAIShadowReport(nil, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339), recommendation.StrategyVersion, "test-model", "test-prompt"); err == nil {
		t.Fatal("expected unavailable database rejection")
	}
}

func TestAIShadowRuntimeIsExplicitlyUnconfigured(t *testing.T) {
	app := NewApp()
	health := app.GetAIShadowRuntimeHealth()
	if health.Configured || health.Running {
		t.Fatalf("unconfigured runtime reported active health: %+v", health)
	}
	if err := app.startAIShadowRuntime(context.Background()); err != nil {
		t.Fatalf("unconfigured startup should be a no-op: %v", err)
	}
	if err := app.stopAIShadowRuntime(context.Background()); err != nil {
		t.Fatalf("unconfigured shutdown should be a no-op: %v", err)
	}
	if err := app.setAIShadowRuntime(nil); err == nil {
		t.Fatal("expected nil runtime configuration rejection")
	}
}

func TestAIShadowRuntimeReadinessIsDisabledWithoutOptIn(t *testing.T) {
	t.Setenv("AI_SHADOW_ENABLED", "false")
	readiness, err := NewApp().GetAIShadowRuntimeReadiness()
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Enabled || readiness.Ready || len(readiness.Issues) != 1 || readiness.Issues[0].Code != "disabled" {
		t.Fatalf("unexpected opt-in readiness: %+v", readiness)
	}
}
