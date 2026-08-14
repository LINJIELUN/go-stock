package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProvider struct {
	name   string
	quotes []Quote
	err    error
}

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) Quotes(context.Context, []Instrument) ([]Quote, error) {
	return f.quotes, f.err
}

func TestEvaluatorReportsCoverageDuplicatesAndProvenance(t *testing.T) {
	now := time.Date(2026, 8, 14, 9, 30, 5, 0, time.UTC)
	sh, _ := NormalizeInstrument("600519.SH", SecurityStock)
	sz, _ := NormalizeInstrument("000001.SZ", SecurityStock)
	bj, _ := NormalizeInstrument("830799.BJ", SecurityStock)
	etf, _ := NormalizeInstrument("510300.SH", SecurityETF)
	quote := func(instrument Instrument, source string) Quote {
		return Quote{Instrument: instrument, LastPrice: 10, PreviousClose: 9.9, Status: StatusTrading,
			MarketTime: now.Add(-time.Second), ReceivedAt: now, Source: source}
	}
	evaluator, err := NewEvaluator(fakeProvider{name: "trial-feed", quotes: []Quote{
		quote(sh, "trial-feed"), quote(sh, "trial-feed"), quote(bj, "wrong-feed"), quote(etf, "trial-feed"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	evaluator.now = func() time.Time { return now }
	report := evaluator.Evaluate(context.Background(), []Instrument{sh, sz, bj}, 5*time.Second, time.Second)
	if report.Passed() || report.Valid != 1 || report.Returned != 4 {
		t.Fatalf("unexpected report counts: %+v", report)
	}
	want := map[IssueCode]bool{
		IssueDuplicateQuote: false, IssueMissingQuote: false, IssueUnexpectedQuote: false, IssueProviderMismatch: false,
	}
	for _, issue := range report.Issues {
		if _, exists := want[issue.Code]; exists {
			want[issue.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("missing expected issue %s: %+v", code, report.Issues)
		}
	}
}

func TestEvaluatorKeepsProviderFailureObservable(t *testing.T) {
	evaluator, err := NewEvaluator(fakeProvider{name: "limited-feed", err: errors.New("rate limited")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	evaluator.now = func() time.Time { return now }
	report := evaluator.Evaluate(context.Background(), nil, 5*time.Second, time.Second)
	if len(report.Issues) != 1 || report.Issues[0].Code != IssueProviderError || report.Passed() {
		t.Fatalf("provider failure was hidden: %+v", report)
	}
}

func TestAggregateCalculatesNearestRankPercentilesAndMissingRate(t *testing.T) {
	reports := make([]BatchReport, 100)
	for i := range reports {
		reports[i] = BatchReport{
			Requested: 1, Valid: 1, RequestDuration: time.Duration(i+1) * time.Millisecond,
			ObservedMarketAges: []time.Duration{time.Duration(i+1) * time.Second},
		}
		if i%4 == 0 {
			reports[i].MissingMainNetInflow = 1
		}
	}
	reports[99].Issues = []ConformanceIssue{{Code: IssueInvalidQuote}}
	result := Aggregate(reports)
	if result.PassedSamples != 99 || result.SuccessRate != 0.99 {
		t.Fatalf("unexpected success rate: %+v", result)
	}
	if result.RequestDurationP50 != 50*time.Millisecond || result.RequestDurationP95 != 95*time.Millisecond || result.RequestDurationP99 != 99*time.Millisecond {
		t.Fatalf("unexpected request percentiles: %+v", result)
	}
	if result.MarketAgeP95 != 95*time.Second || result.MissingMainNetInflowRate != 0.25 {
		t.Fatalf("unexpected market metrics: %+v", result)
	}
	if result.IssueCounts[IssueInvalidQuote] != 1 {
		t.Fatalf("issue count missing: %+v", result.IssueCounts)
	}
}
