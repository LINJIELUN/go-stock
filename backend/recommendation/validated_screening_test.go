package recommendation

import (
	"context"
	"testing"
	"time"

	"go-stock/backend/marketdata"
	"go-stock/backend/models"
)

type fixtureScreeningUniverse struct{ items []ScreeningUniverseItem }

func (f fixtureScreeningUniverse) Securities(context.Context, time.Time) ([]ScreeningUniverseItem, error) {
	return f.items, nil
}

type fixtureCalendar struct{ dates []time.Time }

func (f fixtureCalendar) Name() string { return "fixture-calendar" }
func (f fixtureCalendar) TradingDates(context.Context, marketdata.Exchange, time.Time, time.Time) ([]time.Time, error) {
	return f.dates, nil
}

type fixtureDailyBars struct {
	name string
	bars []marketdata.DailyBar
}

func (f fixtureDailyBars) Name() string { return f.name }
func (f fixtureDailyBars) DailyBars(context.Context, marketdata.Instrument, time.Time, time.Time, marketdata.Adjustment) ([]marketdata.DailyBar, error) {
	return f.bars, nil
}

func TestValidatedScreeningPersistsEvidenceBeforeProducingCandidates(t *testing.T) {
	_, database := testStore(t)
	if err := database.AutoMigrate(&models.AIAnalysisRun{}, &models.AIAnalysisJob{}); err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	tradeDate := time.Date(2026, 8, 19, 15, 30, 0, 0, location)
	instrument := marketdata.Instrument{Code: "600000", Exchange: marketdata.ExchangeShanghai, SecurityType: marketdata.SecurityStock}
	dates, primaryBars, referenceBars := screeningValidationFixtures(instrument, tradeDate, 25)
	service, err := marketdata.NewEndOfDayValidationService(
		fixtureCalendar{dates}, fixtureDailyBars{"fixture-primary", primaryBars}, fixtureDailyBars{"fixture-reference", referenceBars},
		marketdata.ReconciliationPolicy{Location: location, CloseAbsoluteTolerance: 0.001, RequireIndependentSources: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	validationStore, _ := marketdata.NewValidationStore(database)
	provider, err := NewValidatedScreeningProvider(fixtureScreeningUniverse{[]ScreeningUniverseItem{{
		Identity: ScreeningIdentity{StockCode: "600000", StockName: "浦发银行"}, Instrument: instrument,
	}}}, service, validationStore, 45)
	if err != nil {
		t.Fatal(err)
	}
	candidateSource, err := NewQuantitativeCandidateSource(provider, ScreeningPolicy{Target: 10, MinimumLiquidity: 0, MinimumDataQuality: 0})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := candidateSource.Candidates(context.Background(), tradeDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ValidationBatchID == 0 {
		t.Fatalf("unexpected verified candidates: %+v", candidates)
	}
	batch, err := validationStore.RequirePassed(candidates[0].ValidationBatchID)
	if err != nil || batch.InstrumentCode != instrument.CanonicalCode() || batch.ReleasedBars != 25 {
		t.Fatalf("candidate evidence was not persisted: %+v %v", batch, err)
	}
	jobs, _ := NewJobStore(database)
	if _, created, err := jobs.CreateRun(tradeDate, StrategyVersion, candidates, 3, tradeDate); err != nil || !created {
		t.Fatalf("verified candidate could not enter durable run: created=%v err=%v", created, err)
	}
}

func TestValidatedScreeningStopsWhenSourcesDisagree(t *testing.T) {
	_, database := testStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	tradeDate := time.Date(2026, 8, 19, 15, 30, 0, 0, location)
	instrument := marketdata.Instrument{Code: "600000", Exchange: marketdata.ExchangeShanghai, SecurityType: marketdata.SecurityStock}
	dates, primaryBars, referenceBars := screeningValidationFixtures(instrument, tradeDate, 25)
	last := &referenceBars[len(referenceBars)-1]
	last.Open++
	last.High++
	last.Low++
	last.Close++
	service, _ := marketdata.NewEndOfDayValidationService(
		fixtureCalendar{dates}, fixtureDailyBars{"fixture-primary", primaryBars}, fixtureDailyBars{"fixture-reference", referenceBars},
		marketdata.ReconciliationPolicy{Location: location, CloseAbsoluteTolerance: 0.001, RequireIndependentSources: true},
	)
	validationStore, _ := marketdata.NewValidationStore(database)
	provider, _ := NewValidatedScreeningProvider(fixtureScreeningUniverse{[]ScreeningUniverseItem{{
		Identity: ScreeningIdentity{StockCode: "600000", StockName: "浦发银行"}, Instrument: instrument,
	}}}, service, validationStore, 45)
	if _, err := provider.ScreeningRecords(context.Background(), tradeDate); err == nil {
		t.Fatal("expected source disagreement to stop candidate production")
	}
	var batch models.MarketDataValidationBatch
	if err := database.Order("id DESC").First(&batch).Error; err != nil || batch.Status != marketdata.ValidationStatusFailed || batch.ReleasedBars != 0 {
		t.Fatalf("failed reconciliation evidence was not retained: %+v %v", batch, err)
	}
}

func screeningValidationFixtures(instrument marketdata.Instrument, end time.Time, count int) ([]time.Time, []marketdata.DailyBar, []marketdata.DailyBar) {
	dates := make([]time.Time, count)
	primary := make([]marketdata.DailyBar, count)
	reference := make([]marketdata.DailyBar, count)
	for index := 0; index < count; index++ {
		date := end.AddDate(0, 0, index-count+1)
		dates[index] = date
		closePrice := 10 + float64(index)*0.05
		primary[index] = marketdata.DailyBar{Instrument: instrument, TradeDate: date, Open: closePrice - 0.02, High: closePrice + 0.05,
			Low: closePrice - 0.05, Close: closePrice, Volume: 1_000_000, Turnover: 20_000_000, Adjustment: marketdata.AdjustmentNone,
			Source: "fixture-primary", FetchedAt: end}
		reference[index] = primary[index]
		reference[index].Source = "fixture-reference"
	}
	return dates, primary, reference
}
