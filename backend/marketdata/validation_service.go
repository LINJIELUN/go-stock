package marketdata

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type EndOfDayValidationResult struct {
	Instrument      Instrument
	Start           time.Time
	End             time.Time
	CalendarSource  string
	PrimarySource   string
	ReferenceSource string
	ValidatedAt     time.Time
	Reconciliation  ReconciliationResult
	ValidatedBars   []DailyBar
}

func (r EndOfDayValidationResult) Passed() bool {
	return r.Reconciliation.Passed() && len(r.ValidatedBars) == r.Reconciliation.ExpectedDates
}

type EndOfDayValidationService struct {
	calendar  TradeCalendarProvider
	primary   DailyBarProvider
	reference DailyBarProvider
	policy    ReconciliationPolicy
	now       func() time.Time
}

func NewEndOfDayValidationService(calendar TradeCalendarProvider, primary, reference DailyBarProvider, policy ReconciliationPolicy) (*EndOfDayValidationService, error) {
	if calendar == nil || primary == nil || reference == nil {
		return nil, errors.New("calendar, primary, and reference providers are required")
	}
	if calendar.Name() == "" || primary.Name() == "" || reference.Name() == "" {
		return nil, errors.New("all end-of-day providers must have names")
	}
	if primary.Name() == reference.Name() {
		return nil, errors.New("primary and reference daily providers must be independent")
	}
	if policy.Location == nil || policy.CloseAbsoluteTolerance < 0 || !policy.RequireIndependentSources {
		return nil, errors.New("end-of-day validation requires a market location, non-negative tolerance, and independent sources")
	}
	return &EndOfDayValidationService{
		calendar: calendar, primary: primary, reference: reference, policy: policy, now: time.Now,
	}, nil
}

// Validate fetches the exchange calendar and both unadjusted sources, reconciles
// them, and releases primary bars only when every expected date is verified.
func (s *EndOfDayValidationService) Validate(ctx context.Context, instrument Instrument, start, end time.Time) (EndOfDayValidationResult, error) {
	result := EndOfDayValidationResult{
		Instrument: instrument, Start: start, End: end,
		CalendarSource: s.calendar.Name(), PrimarySource: s.primary.Name(), ReferenceSource: s.reference.Name(),
	}
	if start.IsZero() || end.IsZero() || start.After(end) {
		return result, errors.New("end-of-day validation date range is invalid")
	}
	if instrument.Code == "" || instrument.Exchange == "" || instrument.SecurityType == "" {
		return result, errors.New("end-of-day validation instrument is incomplete")
	}
	dates, err := s.calendar.TradingDates(ctx, instrument.Exchange, start, end)
	if err != nil {
		return result, fmt.Errorf("load %s trading calendar: %w", s.calendar.Name(), err)
	}
	if len(dates) == 0 {
		return result, errors.New("calendar returned no trading dates in the requested interval")
	}
	primaryBars, err := s.primary.DailyBars(ctx, instrument, start, end, AdjustmentNone)
	if err != nil {
		return result, fmt.Errorf("load primary daily bars from %s: %w", s.primary.Name(), err)
	}
	if err := validateProviderBars(s.primary.Name(), instrument, primaryBars); err != nil {
		return result, fmt.Errorf("validate primary daily bars: %w", err)
	}
	referenceBars, err := s.reference.DailyBars(ctx, instrument, start, end, AdjustmentNone)
	if err != nil {
		return result, fmt.Errorf("load reference daily bars from %s: %w", s.reference.Name(), err)
	}
	if err := validateProviderBars(s.reference.Name(), instrument, referenceBars); err != nil {
		return result, fmt.Errorf("validate reference daily bars: %w", err)
	}
	reconciliation, err := ReconcileDailyBars(dates, primaryBars, referenceBars, s.policy)
	if err != nil {
		return result, fmt.Errorf("reconcile end-of-day bars: %w", err)
	}
	result.ValidatedAt = s.now()
	result.Reconciliation = reconciliation
	if !reconciliation.Passed() {
		return result, nil
	}
	// Copy so callers cannot mutate a provider-owned slice that may be cached.
	result.ValidatedBars = append([]DailyBar(nil), primaryBars...)
	return result, nil
}

func validateProviderBars(providerName string, instrument Instrument, bars []DailyBar) error {
	for index, bar := range bars {
		if bar.Source != providerName {
			return fmt.Errorf("bar %d source %q does not match provider %q", index, bar.Source, providerName)
		}
		if bar.Instrument != instrument {
			return fmt.Errorf("bar %d instrument %q does not match request %q", index, bar.Instrument.CanonicalCode(), instrument.CanonicalCode())
		}
		if bar.Adjustment != AdjustmentNone {
			return fmt.Errorf("bar %d adjustment %q is not the requested unadjusted mode", index, bar.Adjustment)
		}
	}
	return nil
}
