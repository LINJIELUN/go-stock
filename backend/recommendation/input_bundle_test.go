package recommendation

import (
	"strings"
	"testing"
	"time"

	"go-stock/backend/models"
)

func inputBundleFixture(t *testing.T) (*InputBundleStore, models.AIAnalysisJob, AnalysisInputDraft, time.Time) {
	t.Helper()
	jobs, _, database := analysisJobStore(t)
	if err := database.AutoMigrate(&models.AIAnalysisInputBundle{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, _, err := jobs.CreateRun(now, StrategyVersion, []AnalysisCandidate{testCandidate()}, 3, now); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimNext(now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := NewInputBundleStore(database)
	draft := AnalysisInputDraft{StockCode: job.StockCode, ValidationBatchID: job.ValidationBatchID, DataAsOf: now,
		BaselinePrice: 10, ScreeningJSON: `{"trend":80,"version":"v1"}`, RiskLabels: []string{"ST", "NEW_STOCK"},
		DailyBars:      []FrozenDailyBar{{TradeDate: now.AddDate(0, 0, -1), Open: 9, High: 10, Low: 9, Close: 10, Volume: 100, Turnover: 1000, Source: "validated"}},
		FinancialFacts: []TimedAnalysisFact{{ID: "f1", Category: "financial", Value: "盈利", Source: "report", PublishedAt: now.Add(-time.Hour)}},
		News:           []TimedAnalysisFact{{ID: "n1", Category: "news", Value: "公告", Source: "exchange", PublishedAt: now.Add(-time.Minute)}},
	}
	return store, *job, draft, now
}

func TestInputBundleStoreFreezesCanonicalIdempotentPayload(t *testing.T) {
	store, job, draft, _ := inputBundleFixture(t)
	first, err := store.Save(job, draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.RiskLabels[0], draft.RiskLabels[1] = draft.RiskLabels[1], draft.RiskLabels[0]
	draft.ScreeningJSON = `{ "version": "v1", "trend": 80 }`
	second, err := store.Save(job, draft)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || first.ID != second.ID || len(first.BundleHash) != 64 || !strings.Contains(first.PayloadJSON, `"schemaVersion"`) {
		t.Fatalf("bundle was not canonical or idempotent: first=%+v second=%+v", first, second)
	}
}

func TestInputBundleStoreRejectsChangedRetryAndFutureEvidence(t *testing.T) {
	store, job, draft, _ := inputBundleFixture(t)
	if _, err := store.Save(job, draft); err != nil {
		t.Fatal(err)
	}
	changed := draft
	changed.BaselinePrice = 11
	if _, err := store.Save(job, changed); err == nil {
		t.Fatal("expected changed retry input rejection")
	}
	otherStore, otherJob, future, _ := inputBundleFixture(t)
	future.News[0].PublishedAt = future.DataAsOf.Add(time.Second)
	if _, err := otherStore.Save(otherJob, future); err == nil {
		t.Fatal("expected future evidence rejection")
	}
}

func TestInputBundleStoreRejectsDuplicateFactsAcrossCategories(t *testing.T) {
	store, job, draft, _ := inputBundleFixture(t)
	draft.Announcements = []TimedAnalysisFact{draft.News[0]}
	if _, err := store.Save(job, draft); err == nil {
		t.Fatal("expected duplicate fact rejection")
	}
}
