package recommendation

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// TradingCalendar deliberately contains no holiday guesses. Production callers must
// populate it from an exchange calendar and retain that source/version separately.
type TradingCalendar interface {
	NextTradingDay(after time.Time) (time.Time, error)
}

// DateCalendar is a deterministic calendar useful for a verified exchange-date feed
// and for tests. Dates are normalized in the supplied market location.
type DateCalendar struct {
	dates    []time.Time
	location *time.Location
}

func NewDateCalendar(dates []time.Time, location *time.Location) (*DateCalendar, error) {
	if location == nil {
		return nil, errors.New("market location is required")
	}
	normalized := make([]time.Time, 0, len(dates))
	seen := make(map[string]struct{}, len(dates))
	for _, value := range dates {
		if value.IsZero() {
			return nil, errors.New("trading date cannot be zero")
		}
		date := marketDate(value, location)
		key := date.Format(time.DateOnly)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, date)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Before(normalized[j]) })
	return &DateCalendar{dates: normalized, location: location}, nil
}

func (c *DateCalendar) NextTradingDay(after time.Time) (time.Time, error) {
	if after.IsZero() {
		return time.Time{}, errors.New("reference time is required")
	}
	reference := marketDate(after, c.location)
	index := sort.Search(len(c.dates), func(i int) bool { return c.dates[i].After(reference) })
	if index == len(c.dates) {
		return time.Time{}, fmt.Errorf("calendar has no trading date after %s", reference.Format(time.DateOnly))
	}
	return c.dates[index], nil
}

// ReviewDueDate counts from the trading day after recommendation completion; T0
// is never included. It therefore needs exactly seven successful calendar advances.
func ReviewDueDate(calendar TradingCalendar, completedAt time.Time) (time.Time, error) {
	if calendar == nil {
		return time.Time{}, errors.New("trading calendar is required")
	}
	date := completedAt
	var err error
	for range 7 {
		date, err = calendar.NextTradingDay(date)
		if err != nil {
			return time.Time{}, fmt.Errorf("resolve seven-trading-day review date: %w", err)
		}
	}
	return date, nil
}

func marketDate(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}
