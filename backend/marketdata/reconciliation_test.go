package marketdata

import (
	"testing"
	"time"
)

func dailyBar(instrument Instrument, date time.Time, close float64, source string) DailyBar {
	return DailyBar{
		Instrument: instrument, TradeDate: date, Open: close - 0.1, High: close + 0.2,
		Low: close - 0.2, Close: close, Volume: 1000, Turnover: 10000,
		Adjustment: AdjustmentNone, Source: source, FetchedAt: date.Add(18 * time.Hour),
	}
}

func TestReconcileDailyBarsAcceptsCompleteIndependentMatchingData(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Shanghai")
	instrument, _ := NormalizeInstrument("600519.SH", SecurityStock)
	dates := []time.Time{
		time.Date(2026, 8, 13, 0, 0, 0, 0, location),
		time.Date(2026, 8, 14, 0, 0, 0, 0, location),
	}
	primary := []DailyBar{dailyBar(instrument, dates[0], 1400, "source-a"), dailyBar(instrument, dates[1], 1410, "source-a")}
	reference := []DailyBar{dailyBar(instrument, dates[0], 1400.001, "source-b"), dailyBar(instrument, dates[1], 1410, "source-b")}
	result, err := ReconcileDailyBars(dates, primary, reference, ReconciliationPolicy{
		Location: location, CloseAbsoluteTolerance: 0.0011, RequireIndependentSources: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed() || result.TradeDateCoverageRate != 1 || result.CloseMismatchRate != 0 || result.ComparedDates != 2 {
		t.Fatalf("unexpected reconciliation: %+v", result)
	}
}

func TestReconcileDailyBarsReportsMissingAndMismatchedClose(t *testing.T) {
	location := time.UTC
	instrument, _ := NormalizeInstrument("000001.SZ", SecurityStock)
	dates := []time.Time{
		time.Date(2026, 8, 13, 0, 0, 0, 0, location),
		time.Date(2026, 8, 14, 0, 0, 0, 0, location),
	}
	primary := []DailyBar{dailyBar(instrument, dates[0], 10, "source-a")}
	reference := []DailyBar{dailyBar(instrument, dates[0], 10.02, "source-b"), dailyBar(instrument, dates[1], 10.1, "source-b")}
	result, err := ReconcileDailyBars(dates, primary, reference, ReconciliationPolicy{Location: location, CloseAbsoluteTolerance: 0.001, RequireIndependentSources: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeDateCoverageRate != 0.5 || result.CloseMismatchRate != 1 || result.CloseMismatches != 1 {
		t.Fatalf("missing/mismatch metrics incorrect: %+v", result)
	}
	want := map[string]bool{"missing_primary": false, "close_mismatch": false}
	for _, issue := range result.Issues {
		if _, exists := want[issue.Code]; exists {
			want[issue.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("missing issue %s: %+v", code, result.Issues)
		}
	}
}

func TestReconcileDailyBarsRejectsAdjustmentMismatchAndDuplicate(t *testing.T) {
	location := time.UTC
	instrument, _ := NormalizeInstrument("510300.SH", SecurityETF)
	date := time.Date(2026, 8, 14, 0, 0, 0, 0, location)
	left := dailyBar(instrument, date, 4, "source-a")
	right := dailyBar(instrument, date, 4, "source-b")
	right.Adjustment = AdjustmentForward
	policy := ReconciliationPolicy{Location: location, RequireIndependentSources: true}
	adjustmentResult, err := ReconcileDailyBars([]time.Time{date}, []DailyBar{left}, []DailyBar{right}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if adjustmentResult.ComparedDates != 0 || len(adjustmentResult.Issues) != 1 || adjustmentResult.Issues[0].Code != "adjustment_mismatch" {
		t.Fatalf("adjustment mismatch was accepted: %+v", adjustmentResult)
	}
	result, err := ReconcileDailyBars([]time.Time{date}, []DailyBar{left, left, left}, []DailyBar{dailyBar(instrument, date, 4, "source-b")}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrimaryCoveredDates != 0 || result.ComparedDates != 0 || result.CloseMismatchRate != 1 {
		t.Fatalf("duplicate source data was accepted: %+v", result)
	}
}

func TestApplyReconciliationFeedsQualificationMetrics(t *testing.T) {
	report := ApplyReconciliation(AggregateReport{Samples: 100}, ReconciliationResult{TradeDateCoverageRate: 0.98, CloseMismatchRate: 0.02})
	if report.TradeDateCoverageRate != 0.98 || report.CloseMismatchRate != 0.02 {
		t.Fatalf("reconciliation evidence was not applied: %+v", report)
	}
}
