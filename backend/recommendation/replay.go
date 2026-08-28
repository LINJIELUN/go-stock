package recommendation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"go-stock/backend/marketdata"
)

const ReplayVersion = "screening-walk-forward-v0.1"

type HistoricalSecurity struct {
	Identity             ScreeningIdentity
	Bars                 []marketdata.DailyBar
	ExpectedObservations int
	ValidatedAt          time.Time
	Segment              string
}

type ForwardObservation struct {
	TradingDate time.Time
	Close       float64
}

// WalkForwardProvider deliberately separates as-of inputs from future outcomes.
// The engine asks for forward closes only after selection has finished.
type WalkForwardProvider interface {
	Universe(context.Context, time.Time) ([]HistoricalSecurity, error)
	ForwardClose(context.Context, string, time.Time, int) (ForwardObservation, error)
}

type ReplayObservation struct {
	SelectionDate  time.Time `json:"selectionDate"`
	OutcomeDate    time.Time `json:"outcomeDate"`
	StockCode      string    `json:"stockCode"`
	Segment        string    `json:"segment"`
	Rank           int       `json:"rank"`
	ScreeningScore float64   `json:"screeningScore"`
	BaselineClose  float64   `json:"baselineClose"`
	OutcomeClose   float64   `json:"outcomeClose"`
	ReturnPercent  float64   `json:"returnPercent"`
}

type ReplaySummary struct {
	Version             string  `json:"version"`
	SampleCount         int     `json:"sampleCount"`
	PositiveCount       int     `json:"positiveCount"`
	PositiveRate        float64 `json:"positiveRate"`
	MeanReturnPercent   float64 `json:"meanReturnPercent"`
	MedianReturnPercent float64 `json:"medianReturnPercent"`
	WorstReturnPercent  float64 `json:"worstReturnPercent"`
}

type ReplayReport struct {
	Version      string                   `json:"version"`
	Observations []ReplayObservation      `json:"observations"`
	Overall      ReplaySummary            `json:"overall"`
	BySegment    map[string]ReplaySummary `json:"bySegment"`
}

type WalkForwardEngine struct {
	provider WalkForwardProvider
	policy   ScreeningPolicy
	horizon  int
}

func NewWalkForwardEngine(provider WalkForwardProvider, policy ScreeningPolicy, horizon int) (*WalkForwardEngine, error) {
	if provider == nil || horizon <= 0 {
		return nil, errors.New("walk-forward engine requires provider and positive horizon")
	}
	if _, err := NewQuantitativeCandidateSource(replayRecordProvider{}, policy); err != nil {
		return nil, err
	}
	return &WalkForwardEngine{provider: provider, policy: policy, horizon: horizon}, nil
}

func (e *WalkForwardEngine) Run(ctx context.Context, selectionDates []time.Time) (ReplayReport, error) {
	if len(selectionDates) == 0 {
		return ReplayReport{}, errors.New("walk-forward replay requires selection dates")
	}
	report := ReplayReport{Version: ReplayVersion, BySegment: make(map[string]ReplaySummary)}
	seenDates := make(map[string]bool, len(selectionDates))
	for _, selectionDate := range selectionDates {
		if selectionDate.IsZero() {
			return ReplayReport{}, errors.New("replay selection date cannot be zero")
		}
		dateKey := selectionDate.Format(time.DateOnly)
		if seenDates[dateKey] {
			return ReplayReport{}, fmt.Errorf("duplicate replay selection date %s", dateKey)
		}
		seenDates[dateKey] = true
		universe, err := e.provider.Universe(ctx, selectionDate)
		if err != nil {
			return ReplayReport{}, fmt.Errorf("load universe for %s: %w", dateKey, err)
		}
		records := make([]ScreeningRecord, 0, len(universe))
		securities := make(map[string]HistoricalSecurity, len(universe))
		for _, security := range universe {
			record, err := BuildScreeningRecord(security.Identity, security.Bars, security.ExpectedObservations, security.ValidatedAt, selectionDate)
			if err != nil {
				return ReplayReport{}, fmt.Errorf("build historical features for %s on %s: %w", security.Identity.StockCode, dateKey, err)
			}
			records = append(records, record)
			securities[security.Identity.StockCode] = security
		}
		source, _ := NewQuantitativeCandidateSource(replayRecordProvider{records: records}, e.policy)
		selected, err := source.Candidates(ctx, selectionDate)
		if err != nil {
			return ReplayReport{}, err
		}
		// Future data is requested only after the selected list is frozen above.
		for rank, candidate := range selected {
			security := securities[candidate.StockCode]
			lastBar := latestBar(security.Bars)
			outcome, err := e.provider.ForwardClose(ctx, candidate.StockCode, selectionDate, e.horizon)
			if err != nil {
				return ReplayReport{}, fmt.Errorf("load forward close for %s: %w", candidate.StockCode, err)
			}
			if !outcome.TradingDate.After(selectionDate) || !positiveFinite(outcome.Close) || !positiveFinite(lastBar.Close) {
				return ReplayReport{}, fmt.Errorf("invalid forward observation for %s", candidate.StockCode)
			}
			segment := security.Segment
			if segment == "" {
				segment = "unclassified"
			}
			report.Observations = append(report.Observations, ReplayObservation{SelectionDate: selectionDate, OutcomeDate: outcome.TradingDate,
				StockCode: candidate.StockCode, Segment: segment, Rank: rank + 1, ScreeningScore: candidate.ScreeningScore,
				BaselineClose: lastBar.Close, OutcomeClose: outcome.Close, ReturnPercent: round(percentChange(lastBar.Close, outcome.Close), 4)})
		}
	}
	report.Overall = summarizeReplay(report.Observations)
	grouped := make(map[string][]ReplayObservation)
	for _, observation := range report.Observations {
		grouped[observation.Segment] = append(grouped[observation.Segment], observation)
	}
	for segment, observations := range grouped {
		report.BySegment[segment] = summarizeReplay(observations)
	}
	return report, nil
}

type replayRecordProvider struct{ records []ScreeningRecord }

func (p replayRecordProvider) ScreeningRecords(context.Context, time.Time) ([]ScreeningRecord, error) {
	return p.records, nil
}

func latestBar(bars []marketdata.DailyBar) marketdata.DailyBar {
	return *maxBy(bars, func(a, b marketdata.DailyBar) bool { return a.TradeDate.Before(b.TradeDate) })
}

func summarizeReplay(observations []ReplayObservation) ReplaySummary {
	summary := ReplaySummary{Version: ReplayVersion, SampleCount: len(observations)}
	if len(observations) == 0 {
		return summary
	}
	returns := make([]float64, len(observations))
	summary.WorstReturnPercent = math.Inf(1)
	for index, observation := range observations {
		returns[index] = observation.ReturnPercent
		summary.MeanReturnPercent += observation.ReturnPercent
		if observation.ReturnPercent > 0 {
			summary.PositiveCount++
		}
		summary.WorstReturnPercent = math.Min(summary.WorstReturnPercent, observation.ReturnPercent)
	}
	sort.Float64s(returns)
	summary.MeanReturnPercent = round(summary.MeanReturnPercent/float64(len(returns)), 4)
	summary.PositiveRate = round(float64(summary.PositiveCount)/float64(len(returns))*100, 4)
	summary.MedianReturnPercent = round(median(returns), 4)
	summary.WorstReturnPercent = round(summary.WorstReturnPercent, 4)
	return summary
}

func maxBy[T any](values []T, less func(T, T) bool) *T {
	best := &values[0]
	for index := 1; index < len(values); index++ {
		if less(*best, values[index]) {
			best = &values[index]
		}
	}
	return best
}
