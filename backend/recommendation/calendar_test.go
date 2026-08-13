package recommendation

import (
	"testing"
	"time"
)

func TestReviewDueDateSkipsCalendarGapsAndStartsAfterT0(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	dates := []time.Time{
		time.Date(2026, 9, 28, 0, 0, 0, 0, location),
		time.Date(2026, 9, 29, 0, 0, 0, 0, location),
		time.Date(2026, 9, 30, 0, 0, 0, 0, location),
		time.Date(2026, 10, 9, 0, 0, 0, 0, location),
		time.Date(2026, 10, 12, 0, 0, 0, 0, location),
		time.Date(2026, 10, 13, 0, 0, 0, 0, location),
		time.Date(2026, 10, 14, 0, 0, 0, 0, location),
		time.Date(2026, 10, 15, 0, 0, 0, 0, location),
	}
	calendar, err := NewDateCalendar(dates, location)
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Date(2026, 9, 28, 15, 31, 0, 0, location)
	due, err := ReviewDueDate(calendar, completed)
	if err != nil {
		t.Fatal(err)
	}
	if got := due.Format(time.DateOnly); got != "2026-10-15" {
		t.Fatalf("expected seventh subsequent trading date, got %s", got)
	}
}

func TestReviewDueDateRejectsIncompleteCalendar(t *testing.T) {
	location := time.UTC
	calendar, err := NewDateCalendar([]time.Time{time.Date(2026, 1, 2, 0, 0, 0, 0, location)}, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewDueDate(calendar, time.Date(2026, 1, 1, 12, 0, 0, 0, location)); err == nil {
		t.Fatal("expected incomplete calendar error")
	}
}
