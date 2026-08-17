package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCalendarProvider struct {
	name  string
	dates []time.Time
	err   error
}

func (p fakeCalendarProvider) Name() string { return p.name }
func (p fakeCalendarProvider) TradingDates(context.Context, Exchange, time.Time, time.Time) ([]time.Time, error) {
	return p.dates, p.err
}

type fakeDailyProvider struct {
	name  string
	bars  []DailyBar
	err   error
	calls int
}

func (p *fakeDailyProvider) Name() string { return p.name }
func (p *fakeDailyProvider) DailyBars(context.Context, Instrument, time.Time, time.Time, Adjustment) ([]DailyBar, error) {
	p.calls++
	return p.bars, p.err
}

func validationFixture(t *testing.T) (Instrument, []time.Time, []DailyBar, []DailyBar, ReconciliationPolicy) {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	instrument, _ := NormalizeInstrument("600519.SH", SecurityStock)
	dates := []time.Time{
		time.Date(2026, 8, 13, 0, 0, 0, 0, location),
		time.Date(2026, 8, 14, 0, 0, 0, 0, location),
	}
	primary := []DailyBar{dailyBar(instrument, dates[0], 1400, "primary"), dailyBar(instrument, dates[1], 1410, "primary")}
	reference := []DailyBar{dailyBar(instrument, dates[0], 1400, "reference"), dailyBar(instrument, dates[1], 1410, "reference")}
	policy := ReconciliationPolicy{Location: location, CloseAbsoluteTolerance: 0.001, RequireIndependentSources: true}
	return instrument, dates, primary, reference, policy
}

func TestEndOfDayValidationReleasesOnlyFullyVerifiedBars(t *testing.T) {
	instrument, dates, primaryBars, referenceBars, policy := validationFixture(t)
	primary := &fakeDailyProvider{name: "primary", bars: primaryBars}
	reference := &fakeDailyProvider{name: "reference", bars: referenceBars}
	service, err := NewEndOfDayValidationService(fakeCalendarProvider{name: "calendar", dates: dates}, primary, reference, policy)
	if err != nil {
		t.Fatal(err)
	}
	validatedAt := time.Date(2026, 8, 14, 18, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return validatedAt }
	result, err := service.Validate(context.Background(), instrument, dates[0], dates[1])
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed() || len(result.ValidatedBars) != 2 || result.ValidatedAt != validatedAt {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	result.ValidatedBars[0].Close = 1
	if primaryBars[0].Close != 1400 {
		t.Fatal("validated result aliases provider-owned bars")
	}
}

func TestEndOfDayValidationWithholdsBarsOnMismatch(t *testing.T) {
	instrument, dates, primaryBars, referenceBars, policy := validationFixture(t)
	referenceBars[1].Close = 1411
	referenceBars[1].High = 1412
	service, _ := NewEndOfDayValidationService(
		fakeCalendarProvider{name: "calendar", dates: dates},
		&fakeDailyProvider{name: "primary", bars: primaryBars},
		&fakeDailyProvider{name: "reference", bars: referenceBars}, policy,
	)
	result, err := service.Validate(context.Background(), instrument, dates[0], dates[1])
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() || len(result.ValidatedBars) != 0 || result.Reconciliation.CloseMismatches != 1 {
		t.Fatalf("mismatched bars were released: %+v", result)
	}
}

func TestEndOfDayValidationStopsBeforeReferenceWhenPrimaryFails(t *testing.T) {
	instrument, dates, _, _, policy := validationFixture(t)
	primary := &fakeDailyProvider{name: "primary", err: errors.New("quota exhausted")}
	reference := &fakeDailyProvider{name: "reference"}
	service, _ := NewEndOfDayValidationService(fakeCalendarProvider{name: "calendar", dates: dates}, primary, reference, policy)
	if _, err := service.Validate(context.Background(), instrument, dates[0], dates[1]); err == nil {
		t.Fatal("expected primary provider error")
	}
	if primary.calls != 1 || reference.calls != 0 {
		t.Fatalf("validation continued after primary failure: primary=%d reference=%d", primary.calls, reference.calls)
	}
}

func TestEndOfDayValidationRejectsSameSourceConfiguration(t *testing.T) {
	_, _, _, _, policy := validationFixture(t)
	if _, err := NewEndOfDayValidationService(
		fakeCalendarProvider{name: "calendar"},
		&fakeDailyProvider{name: "same"}, &fakeDailyProvider{name: "same"}, policy,
	); err == nil {
		t.Fatal("expected same-source configuration rejection")
	}
}

func TestEndOfDayValidationRejectsProviderProvenanceMismatch(t *testing.T) {
	instrument, dates, primaryBars, referenceBars, policy := validationFixture(t)
	primaryBars[0].Source = "spoofed-source"
	service, _ := NewEndOfDayValidationService(
		fakeCalendarProvider{name: "calendar", dates: dates},
		&fakeDailyProvider{name: "primary", bars: primaryBars},
		&fakeDailyProvider{name: "reference", bars: referenceBars}, policy,
	)
	if _, err := service.Validate(context.Background(), instrument, dates[0], dates[1]); err == nil {
		t.Fatal("expected provider provenance mismatch rejection")
	}
}
