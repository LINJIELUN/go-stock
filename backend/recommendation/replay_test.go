package recommendation

import (
	"context"
	"testing"
	"time"

	"go-stock/backend/marketdata"
)

type fakeWalkForwardProvider struct {
	universe     []HistoricalSecurity
	outcomes     map[string]ForwardObservation
	forwardCalls []string
}

func (f *fakeWalkForwardProvider) Universe(context.Context, time.Time) ([]HistoricalSecurity, error) {
	return f.universe, nil
}
func (f *fakeWalkForwardProvider) ForwardClose(_ context.Context, code string, _ time.Time, _ int) (ForwardObservation, error) {
	f.forwardCalls = append(f.forwardCalls, code)
	return f.outcomes[code], nil
}

func historicalSecurity(t *testing.T, code, name, segment string, rising bool, batchID uint) HistoricalSecurity {
	t.Helper()
	bars := screeningBars(t)
	instrument, err := marketdata.NormalizeInstrument(code, marketdata.SecurityStock)
	if err != nil {
		t.Fatal(err)
	}
	price := 10.0
	for index := range bars {
		bars[index].Instrument = instrument
		bars[index].Close, bars[index].Open, bars[index].High, bars[index].Low = price, price, price, price
		if rising {
			price *= 1.01
		} else {
			price *= 0.995
		}
	}
	cutoff := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	return HistoricalSecurity{Identity: ScreeningIdentity{StockCode: code, StockName: name, ValidationBatchID: batchID},
		Bars: bars, ExpectedObservations: 21, ValidatedAt: cutoff, Segment: segment}
}

func TestWalkForwardReplayRequestsOutcomesOnlyForFrozenSelection(t *testing.T) {
	selectionDate := time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC)
	selected := historicalSecurity(t, "600000.SH", "上涨样本", "main_board", true, 1)
	rejected := historicalSecurity(t, "000001.SZ", "下跌样本", "main_board", false, 2)
	provider := &fakeWalkForwardProvider{universe: []HistoricalSecurity{selected, rejected}, outcomes: map[string]ForwardObservation{
		"600000.SH": {TradingDate: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), Close: 13},
		"000001.SZ": {TradingDate: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), Close: 1},
	}}
	engine, err := NewWalkForwardEngine(provider, ScreeningPolicy{Target: 1, MinimumLiquidity: 0, MinimumDataQuality: 90}, 7)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Run(context.Background(), []time.Time{selectionDate})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.forwardCalls) != 1 || provider.forwardCalls[0] != "600000.SH" {
		t.Fatalf("future outcomes influenced or exceeded frozen selection: %+v", provider.forwardCalls)
	}
	if len(report.Observations) != 1 || report.Overall.SampleCount != 1 || report.Overall.PositiveRate != 100 || report.BySegment["main_board"].SampleCount != 1 {
		t.Fatalf("unexpected replay report: %+v", report)
	}
}

func TestWalkForwardReplayRejectsOutcomeAtOrBeforeSelection(t *testing.T) {
	selectionDate := time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC)
	security := historicalSecurity(t, "600000.SH", "样本", "main_board", true, 1)
	provider := &fakeWalkForwardProvider{universe: []HistoricalSecurity{security}, outcomes: map[string]ForwardObservation{
		"600000.SH": {TradingDate: selectionDate, Close: 13},
	}}
	engine, _ := NewWalkForwardEngine(provider, ScreeningPolicy{Target: 1, MinimumLiquidity: 0, MinimumDataQuality: 90}, 7)
	if _, err := engine.Run(context.Background(), []time.Time{selectionDate}); err == nil {
		t.Fatal("expected invalid non-forward outcome rejection")
	}
}

func TestWalkForwardReplayRejectsDuplicateSelectionDates(t *testing.T) {
	provider := &fakeWalkForwardProvider{}
	engine, _ := NewWalkForwardEngine(provider, ScreeningPolicy{Target: 1, MinimumLiquidity: 0, MinimumDataQuality: 90}, 7)
	date := time.Now().UTC()
	if _, err := engine.Run(context.Background(), []time.Time{date, date.Add(time.Hour)}); err == nil {
		t.Fatal("expected duplicate market date rejection")
	}
}
