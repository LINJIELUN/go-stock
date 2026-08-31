package marketdata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type IssueCode string

const (
	IssueProviderError    IssueCode = "provider_error"
	IssueMissingQuote     IssueCode = "missing_quote"
	IssueDuplicateQuote   IssueCode = "duplicate_quote"
	IssueUnexpectedQuote  IssueCode = "unexpected_quote"
	IssueInvalidQuote     IssueCode = "invalid_quote"
	IssueProviderMismatch IssueCode = "provider_mismatch"
	IssueInvalidRequest   IssueCode = "invalid_request"
)

type ConformanceIssue struct {
	Code       IssueCode
	Instrument string
	Message    string
}

type BatchReport struct {
	Provider             string
	StartedAt            time.Time
	FinishedAt           time.Time
	RequestDuration      time.Duration
	Requested            int
	Returned             int
	Valid                int
	MissingTurnoverRate  int
	MissingVolumeRatio   int
	MissingMainNetInflow int
	Issues               []ConformanceIssue
	ObservedMarketAges   []time.Duration
}

func (r BatchReport) Passed() bool { return len(r.Issues) == 0 && r.Valid == r.Requested }

type Evaluator struct {
	provider QuoteProvider
	now      func() time.Time
}

func NewEvaluator(provider QuoteProvider) (*Evaluator, error) {
	if provider == nil || provider.Name() == "" {
		return nil, errors.New("quote evaluator requires a named provider")
	}
	return &Evaluator{provider: provider, now: time.Now}, nil
}

// Evaluate performs one observable provider call and checks exact request/response
// coverage. It does not retry, hide provider errors, or substitute cached quotes.
func (e *Evaluator) Evaluate(ctx context.Context, requested []Instrument, maximumAge, maximumClockSkew time.Duration) BatchReport {
	started := e.now()
	report := BatchReport{Provider: e.provider.Name(), StartedAt: started, Requested: len(requested)}
	wanted := make(map[string]struct{}, len(requested))
	for _, instrument := range requested {
		code := instrument.CanonicalCode()
		if instrument.Code == "" || instrument.Exchange == "" || instrument.SecurityType == "" {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueInvalidRequest, Instrument: code, Message: "requested instrument is incomplete"})
			continue
		}
		if _, duplicate := wanted[code]; duplicate {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueInvalidRequest, Instrument: code, Message: "instrument was requested more than once"})
			continue
		}
		wanted[code] = struct{}{}
	}
	if len(requested) == 0 {
		report.Issues = append(report.Issues, ConformanceIssue{Code: IssueInvalidRequest, Message: "at least one instrument is required"})
	}
	if len(report.Issues) > 0 {
		report.FinishedAt = e.now()
		report.RequestDuration = report.FinishedAt.Sub(started)
		return report
	}
	quotes, err := e.provider.Quotes(ctx, requested)
	finished := e.now()
	report.FinishedAt = finished
	report.RequestDuration = finished.Sub(started)
	if err != nil {
		report.Issues = append(report.Issues, ConformanceIssue{Code: IssueProviderError, Message: err.Error()})
		return report
	}
	report.Returned = len(quotes)

	seen := make(map[string]int, len(quotes))
	for _, quote := range quotes {
		code := quote.Instrument.CanonicalCode()
		seen[code]++
		if _, exists := wanted[code]; !exists {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueUnexpectedQuote, Instrument: code, Message: "provider returned an unrequested instrument"})
			continue
		}
		if seen[code] > 1 {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueDuplicateQuote, Instrument: code, Message: "provider returned the instrument more than once"})
			continue
		}
		if quote.Source != report.Provider {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueProviderMismatch, Instrument: code, Message: fmt.Sprintf("quote source %q does not match provider %q", quote.Source, report.Provider)})
			continue
		}
		if err := ValidateQuote(quote, ValidationPolicy{Now: finished, MaximumAge: maximumAge, MaximumClockSkew: maximumClockSkew}); err != nil {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueInvalidQuote, Instrument: code, Message: err.Error()})
			continue
		}
		report.Valid++
		report.ObservedMarketAges = append(report.ObservedMarketAges, finished.Sub(quote.MarketTime))
		if quote.TurnoverRate == nil {
			report.MissingTurnoverRate++
		}
		if quote.VolumeRatio == nil {
			report.MissingVolumeRatio++
		}
		if quote.MainNetInflow == nil {
			report.MissingMainNetInflow++
		}
	}
	for code := range wanted {
		if seen[code] == 0 {
			report.Issues = append(report.Issues, ConformanceIssue{Code: IssueMissingQuote, Instrument: code, Message: "provider omitted a requested instrument"})
		}
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		if report.Issues[i].Instrument == report.Issues[j].Instrument {
			return report.Issues[i].Code < report.Issues[j].Code
		}
		return report.Issues[i].Instrument < report.Issues[j].Instrument
	})
	return report
}

type AggregateReport struct {
	Samples                  int
	PassedSamples            int
	SuccessRate              float64
	RequestDurationP50       time.Duration
	RequestDurationP95       time.Duration
	RequestDurationP99       time.Duration
	MarketAgeP50             time.Duration
	MarketAgeP95             time.Duration
	MarketAgeP99             time.Duration
	IssueCounts              map[IssueCode]int
	MissingMainNetInflowRate float64
	TradeDateCoverageRate    float64
	CloseMismatchRate        float64
}

func Aggregate(reports []BatchReport) AggregateReport {
	result := AggregateReport{Samples: len(reports), IssueCounts: make(map[IssueCode]int)}
	if len(reports) == 0 {
		return result
	}
	durations := make([]time.Duration, 0, len(reports))
	ages := make([]time.Duration, 0)
	validQuotes, missingMainNetInflow := 0, 0
	for _, report := range reports {
		durations = append(durations, report.RequestDuration)
		ages = append(ages, report.ObservedMarketAges...)
		validQuotes += report.Valid
		missingMainNetInflow += report.MissingMainNetInflow
		if report.Passed() {
			result.PassedSamples++
		}
		for _, issue := range report.Issues {
			result.IssueCounts[issue.Code]++
		}
	}
	result.SuccessRate = float64(result.PassedSamples) / float64(result.Samples)
	if validQuotes > 0 {
		result.MissingMainNetInflowRate = float64(missingMainNetInflow) / float64(validQuotes)
	}
	result.RequestDurationP50, result.RequestDurationP95, result.RequestDurationP99 = percentiles(durations)
	result.MarketAgeP50, result.MarketAgeP95, result.MarketAgeP99 = percentiles(ages)
	return result
}

func percentiles(values []time.Duration) (time.Duration, time.Duration, time.Duration) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	at := func(percent int) time.Duration {
		// Nearest-rank percentile: ceil(p*N)-1.
		index := (percent*len(sorted)+99)/100 - 1
		if index < 0 {
			index = 0
		}
		return sorted[index]
	}
	return at(50), at(95), at(99)
}
