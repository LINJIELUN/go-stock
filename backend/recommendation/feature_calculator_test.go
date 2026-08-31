package recommendation

import (
	"math"
	"testing"
	"time"

	"go-stock/backend/marketdata"
)

func screeningBars(t *testing.T) []marketdata.DailyBar {
	t.Helper()
	instrument, err := marketdata.NormalizeInstrument("600000.SH", marketdata.SecurityStock)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]marketdata.DailyBar, 21)
	price := 10.0
	for index := range bars {
		bars[index] = marketdata.DailyBar{Instrument: instrument, TradeDate: start.AddDate(0, 0, index),
			Open: price, High: price, Low: price, Close: price, Volume: 1_000_000, Turnover: 100_000_000,
			Adjustment: marketdata.AdjustmentNone, Source: "verified", FetchedAt: start.AddDate(0, 0, index)}
		price *= 1.01
	}
	return bars
}

func TestBuildScreeningRecordCalculatesVersionedRawFeatures(t *testing.T) {
	bars := screeningBars(t)
	validatedAt := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	record, err := BuildScreeningRecord(ScreeningIdentity{StockCode: "600000", StockName: "浦发银行", ValidationBatchID: 7, IsST: true},
		bars, 21, validatedAt, validatedAt.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if record.RawFeatures == nil || record.RawFeatures.Version != FeatureFormulaVersion || record.RawFeatures.Observations != 21 {
		t.Fatalf("raw feature evidence missing: %+v", record)
	}
	if record.Trend < 80 || record.Trend > 81 || record.Liquidity != 50 || record.RiskSafety != 100 || record.DataQuality != 95 {
		t.Fatalf("unexpected normalized factors: %+v", record)
	}
	if record.RawFeatures.Return20DayPercent < 22 || record.RawFeatures.AnnualizedVolatility > 0.0001 || record.RawFeatures.MedianTurnoverYuan != 100_000_000 {
		t.Fatalf("unexpected raw metrics: %+v", record.RawFeatures)
	}
}

func TestBuildScreeningRecordRejectsInsufficientOrMixedEvidence(t *testing.T) {
	bars := screeningBars(t)
	now := time.Now().UTC()
	identity := ScreeningIdentity{StockCode: "600000", StockName: "浦发银行", ValidationBatchID: 7}
	if _, err := BuildScreeningRecord(identity, bars[:20], 20, now, now); err == nil {
		t.Fatal("expected insufficient history rejection")
	}
	other, _ := marketdata.NormalizeInstrument("000001.SZ", marketdata.SecurityStock)
	bars[10].Instrument = other
	if _, err := BuildScreeningRecord(identity, bars, 21, now, now); err == nil {
		t.Fatal("expected mixed instrument rejection")
	}
}

func TestBuildScreeningRecordRejectsNonFiniteMarketValues(t *testing.T) {
	bars := screeningBars(t)
	bars[20].Close = math.NaN()
	now := time.Now().UTC()
	if _, err := BuildScreeningRecord(ScreeningIdentity{StockCode: "600000", StockName: "浦发银行", ValidationBatchID: 7}, bars, 21, now, now); err == nil {
		t.Fatal("expected non-finite close rejection")
	}
}

func TestBuildScreeningRecordRejectsFutureDataLeakage(t *testing.T) {
	bars := screeningBars(t)
	cutoff := bars[len(bars)-2].TradeDate
	if _, err := BuildScreeningRecord(ScreeningIdentity{StockCode: "600000", StockName: "浦发银行", ValidationBatchID: 7},
		bars, 21, cutoff, cutoff); err == nil {
		t.Fatal("expected bar after evidence cutoff rejection")
	}
}
