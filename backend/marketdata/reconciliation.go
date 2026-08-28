package marketdata

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

type Adjustment string

const (
	AdjustmentNone     Adjustment = "none"
	AdjustmentForward  Adjustment = "forward"
	AdjustmentBackward Adjustment = "backward"
)

type DailyBar struct {
	Instrument Instrument
	TradeDate  time.Time
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Volume     float64
	Turnover   float64
	Adjustment Adjustment
	Source     string
	FetchedAt  time.Time
}

type ReconciliationPolicy struct {
	Location                  *time.Location
	CloseAbsoluteTolerance    float64
	RequireIndependentSources bool
}

type ReconciliationIssue struct {
	Code      string
	TradeDate string
	Source    string
	Message   string
}

type ReconciliationResult struct {
	ExpectedDates         int
	PrimaryCoveredDates   int
	ReferenceCoveredDates int
	ComparedDates         int
	CloseMismatches       int
	TradeDateCoverageRate float64
	CloseMismatchRate     float64
	Issues                []ReconciliationIssue
}

func (r ReconciliationResult) Passed() bool {
	return r.ExpectedDates > 0 && r.TradeDateCoverageRate == 1 && r.CloseMismatchRate == 0 && len(r.Issues) == 0
}

// ReconcileDailyBars aligns two independent sources against an exchange-date list.
// It compares raw values only when adjustment modes match; it never auto-converts or
// fills a missing trading date with the previous close.
func ReconcileDailyBars(expectedDates []time.Time, primary, reference []DailyBar, policy ReconciliationPolicy) (ReconciliationResult, error) {
	if policy.Location == nil || policy.CloseAbsoluteTolerance < 0 {
		return ReconciliationResult{}, errors.New("reconciliation location and non-negative tolerance are required")
	}
	expected, err := normalizedDateSet(expectedDates, policy.Location)
	if err != nil || len(expected) == 0 {
		return ReconciliationResult{}, errors.New("at least one valid expected trading date is required")
	}
	result := ReconciliationResult{ExpectedDates: len(expected)}
	primaryByDate := indexDailyBars(primary, expected, policy, "primary", &result)
	referenceByDate := indexDailyBars(reference, expected, policy, "reference", &result)
	result.PrimaryCoveredDates = len(primaryByDate)
	result.ReferenceCoveredDates = len(referenceByDate)
	verifiedCoveredDates := min(result.PrimaryCoveredDates, result.ReferenceCoveredDates)
	result.TradeDateCoverageRate = float64(verifiedCoveredDates) / float64(result.ExpectedDates)

	for date := range expected {
		left, leftOK := primaryByDate[date]
		right, rightOK := referenceByDate[date]
		if !leftOK {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "missing_primary", TradeDate: date, Message: "primary source omitted an expected trading date"})
		}
		if !rightOK {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "missing_reference", TradeDate: date, Message: "reference source omitted an expected trading date"})
		}
		if !leftOK || !rightOK {
			continue
		}
		if policy.RequireIndependentSources && left.Source == right.Source {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "same_source", TradeDate: date, Source: left.Source, Message: "primary and reference evidence are not independent"})
			continue
		}
		if left.Adjustment != right.Adjustment {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "adjustment_mismatch", TradeDate: date, Message: "daily bars use different adjustment modes"})
			continue
		}
		result.ComparedDates++
		if math.Abs(left.Close-right.Close) > policy.CloseAbsoluteTolerance {
			result.CloseMismatches++
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "close_mismatch", TradeDate: date, Message: fmt.Sprintf("close differs: primary %.6f reference %.6f", left.Close, right.Close)})
		}
	}
	if result.ComparedDates > 0 {
		result.CloseMismatchRate = float64(result.CloseMismatches) / float64(result.ComparedDates)
	} else {
		result.CloseMismatchRate = 1
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].TradeDate == result.Issues[j].TradeDate {
			return result.Issues[i].Code < result.Issues[j].Code
		}
		return result.Issues[i].TradeDate < result.Issues[j].TradeDate
	})
	return result, nil
}

func ApplyReconciliation(report AggregateReport, result ReconciliationResult) AggregateReport {
	report.TradeDateCoverageRate = result.TradeDateCoverageRate
	report.CloseMismatchRate = result.CloseMismatchRate
	return report
}

func normalizedDateSet(dates []time.Time, location *time.Location) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(dates))
	for _, value := range dates {
		if value.IsZero() {
			return nil, errors.New("expected trading date cannot be zero")
		}
		result[marketDateKey(value, location)] = struct{}{}
	}
	return result, nil
}

func indexDailyBars(bars []DailyBar, expected map[string]struct{}, policy ReconciliationPolicy, role string, result *ReconciliationResult) map[string]DailyBar {
	indexed := make(map[string]DailyBar, len(bars))
	seen := make(map[string]bool, len(bars))
	for _, bar := range bars {
		date := marketDateKey(bar.TradeDate, policy.Location)
		if err := validateDailyBar(bar); err != nil {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "invalid_" + role, TradeDate: date, Source: bar.Source, Message: err.Error()})
			continue
		}
		if _, wanted := expected[date]; !wanted {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "unexpected_" + role + "_date", TradeDate: date, Source: bar.Source, Message: "bar is not in the expected exchange calendar"})
			continue
		}
		if seen[date] {
			result.Issues = append(result.Issues, ReconciliationIssue{Code: "duplicate_" + role, TradeDate: date, Source: bar.Source, Message: "source returned more than one bar for the date"})
			delete(indexed, date)
			continue
		}
		seen[date] = true
		indexed[date] = bar
	}
	return indexed
}

func validateDailyBar(bar DailyBar) error {
	if bar.Instrument.Code == "" || bar.TradeDate.IsZero() || bar.Source == "" || bar.FetchedAt.IsZero() {
		return errors.New("instrument, trade date, source, and fetch time are required")
	}
	if bar.Adjustment != AdjustmentNone && bar.Adjustment != AdjustmentForward && bar.Adjustment != AdjustmentBackward {
		return errors.New("unsupported adjustment mode")
	}
	values := []float64{bar.Open, bar.High, bar.Low, bar.Close}
	for _, value := range values {
		if !finite(value) || value <= 0 {
			return errors.New("OHLC prices must be positive and finite")
		}
	}
	if !finiteNonNegative(bar.Volume) || !finiteNonNegative(bar.Turnover) {
		return errors.New("volume and turnover must be finite and non-negative")
	}
	if bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) || bar.High < bar.Low {
		return errors.New("OHLC price relationships are inconsistent")
	}
	return nil
}

func marketDateKey(value time.Time, location *time.Location) string {
	return value.In(location).Format(time.DateOnly)
}
