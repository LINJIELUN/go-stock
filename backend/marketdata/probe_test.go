package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sequenceProvider struct {
	name       string
	now        time.Time
	calls      int
	failOnCall int
}

func (p *sequenceProvider) Name() string { return p.name }
func (p *sequenceProvider) Quotes(_ context.Context, instruments []Instrument) ([]Quote, error) {
	p.calls++
	if p.calls == p.failOnCall {
		return nil, errors.New("trial quota exhausted")
	}
	quotes := make([]Quote, 0, len(instruments))
	for _, instrument := range instruments {
		quotes = append(quotes, Quote{Instrument: instrument, LastPrice: 10, PreviousClose: 9,
			Status: StatusTrading, MarketTime: p.now, ReceivedAt: p.now, Source: p.name})
	}
	return quotes, nil
}

func TestProbeRunnerUsesExactSampleBudgetWithoutRetry(t *testing.T) {
	now := time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)
	provider := &sequenceProvider{name: "trial-feed", now: now, failOnCall: 2}
	runner, err := NewProbeRunner(provider)
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return now }
	runner.evaluator.now = func() time.Time { return now }
	runner.wait = func(context.Context, time.Duration) error { return nil }
	instrument, _ := NormalizeInstrument("600519.SH", SecurityStock)
	result, err := runner.Run(context.Background(), []Instrument{instrument}, ProbeConfig{
		Samples: 3, RequestTimeout: time.Second, MaximumQuoteAge: 5 * time.Second, MaximumClockSkew: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 || result.Completed != 3 || result.Aggregate.PassedSamples != 2 {
		t.Fatalf("probe retried or miscounted samples: calls=%d result=%+v", provider.calls, result)
	}
	if result.Aggregate.IssueCounts[IssueProviderError] != 1 {
		t.Fatalf("provider failure was not measured: %+v", result.Aggregate)
	}
}

func TestProbeRunnerStopsOnCancellationDuringInterval(t *testing.T) {
	now := time.Now().UTC()
	provider := &sequenceProvider{name: "trial-feed", now: now}
	runner, _ := NewProbeRunner(provider)
	runner.now = func() time.Time { return now }
	runner.evaluator.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	runner.wait = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	instrument, _ := NormalizeInstrument("000001.SZ", SecurityStock)
	result, err := runner.Run(ctx, []Instrument{instrument}, ProbeConfig{
		Samples: 5, Interval: time.Second, RequestTimeout: time.Second, MaximumQuoteAge: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Interrupted || result.Completed != 1 || provider.calls != 1 {
		t.Fatalf("cancelled probe continued: %+v calls=%d", result, provider.calls)
	}
}

func TestProbeRunnerRejectsUnsafeObservationVolume(t *testing.T) {
	provider := &sequenceProvider{name: "trial-feed"}
	runner, _ := NewProbeRunner(provider)
	instruments := make([]Instrument, 101)
	for i := range instruments {
		instruments[i] = Instrument{Code: "600519", Exchange: ExchangeShanghai, SecurityType: SecurityStock}
	}
	_, err := runner.Run(context.Background(), instruments, ProbeConfig{Samples: 100, RequestTimeout: time.Second})
	if err == nil || provider.calls != 0 {
		t.Fatalf("unsafe probe was executed: err=%v calls=%d", err, provider.calls)
	}
}
