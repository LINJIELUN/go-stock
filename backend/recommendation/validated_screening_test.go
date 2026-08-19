package recommendation

import (
	"context"
	"sync/atomic"
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

type concurrentFixtureBars struct {
	name      string
	byCode    map[string][]marketdata.DailyBar
	active    int32
	maxActive int32
}

func (f *concurrentFixtureBars) Name() string { return f.name }
func (f *concurrentFixtureBars) DailyBars(_ context.Context, instrument marketdata.Instrument, _, _ time.Time, _ marketdata.Adjustment) ([]marketdata.DailyBar, error) {
	active := atomic.AddInt32(&f.active, 1)
	for {
		maximum := atomic.LoadInt32(&f.maxActive)
		if active <= maximum || atomic.CompareAndSwapInt32(&f.maxActive, maximum, active) {
			break
		}
	}
	time.Sleep(10 * time.Millisecond)
	atomic.AddInt32(&f.active, -1)
	return f.byCode[instrument.CanonicalCode()], nil
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
	}}}, service, validationStore, 45, 2)
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
	}}}, service, validationStore, 45, 2)
	if _, err := provider.ScreeningRecords(context.Background(), tradeDate); err == nil {
		t.Fatal("expected source disagreement to stop candidate production")
	}
	var batch models.MarketDataValidationBatch
	if err := database.Order("id DESC").First(&batch).Error; err != nil || batch.Status != marketdata.ValidationStatusFailed || batch.ReleasedBars != 0 {
		t.Fatalf("failed reconciliation evidence was not retained: %+v %v", batch, err)
	}
}

func TestValidatedScreeningUsesExplicitBoundedConcurrency(t *testing.T) {
	_, database := testStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	tradeDate := time.Date(2026, 8, 19, 15, 30, 0, 0, location)
	shanghai := marketdata.Instrument{Code: "600000", Exchange: marketdata.ExchangeShanghai, SecurityType: marketdata.SecurityStock}
	shenzhen := marketdata.Instrument{Code: "000001", Exchange: marketdata.ExchangeShenzhen, SecurityType: marketdata.SecurityStock}
	dates, shPrimary, shReference := screeningValidationFixtures(shanghai, tradeDate, 25)
	_, szPrimary, szReference := screeningValidationFixtures(shenzhen, tradeDate, 25)
	primary := &concurrentFixtureBars{name: "concurrent-primary", byCode: map[string][]marketdata.DailyBar{
		shanghai.CanonicalCode(): withBarSource(shPrimary, "concurrent-primary"), shenzhen.CanonicalCode(): withBarSource(szPrimary, "concurrent-primary"),
	}}
	reference := &concurrentFixtureBars{name: "concurrent-reference", byCode: map[string][]marketdata.DailyBar{
		shanghai.CanonicalCode(): withBarSource(shReference, "concurrent-reference"), shenzhen.CanonicalCode(): withBarSource(szReference, "concurrent-reference"),
	}}
	service, _ := marketdata.NewEndOfDayValidationService(fixtureCalendar{dates}, primary, reference,
		marketdata.ReconciliationPolicy{Location: location, CloseAbsoluteTolerance: 0.001, RequireIndependentSources: true})
	validationStore, _ := marketdata.NewValidationStore(database)
	provider, err := NewValidatedScreeningProvider(fixtureScreeningUniverse{[]ScreeningUniverseItem{
		{Identity: ScreeningIdentity{StockCode: "600000", StockName: "浦发银行"}, Instrument: shanghai},
		{Identity: ScreeningIdentity{StockCode: "000001", StockName: "平安银行"}, Instrument: shenzhen},
	}}, service, validationStore, 45, 2)
	if err != nil {
		t.Fatal(err)
	}
	records, err := provider.ScreeningRecords(context.Background(), tradeDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || atomic.LoadInt32(&primary.maxActive) != 2 || atomic.LoadInt32(&reference.maxActive) != 2 {
		t.Fatalf("bounded validation did not run two instruments concurrently: records=%d primary=%d reference=%d",
			len(records), primary.maxActive, reference.maxActive)
	}
	if _, err := NewValidatedScreeningProvider(fixtureScreeningUniverse{}, service, validationStore, 45, 33); err == nil {
		t.Fatal("expected excessive screening concurrency rejection")
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
