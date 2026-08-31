package recommendation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type recoveringTradingDay struct{ fail bool }

func (d *recoveringTradingDay) IsTradingDay(context.Context, time.Time) (bool, error) {
	if d.fail {
		return false, errors.New("calendar unavailable")
	}
	return false, nil
}

func TestRetryDelayIsExponentiallyBounded(t *testing.T) {
	initial, maximum := time.Second, 5*time.Second
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i, expected := range want {
		if got := retryDelay(initial, maximum, i+1); got != expected {
			t.Fatalf("failure %d: got %s want %s", i+1, got, expected)
		}
	}
}

func TestRuntimeSupervisorRetriesAndRecoversHealth(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	current := time.Date(2026, 8, 19, 16, 0, 0, 0, location)
	calendar := &recoveringTradingDay{fail: true}
	scheduler, err := NewPostCloseScheduler(jobs, calendar, &fixedCandidates{}, testPostClosePolicy(location))
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewAnalysisProcessor(jobs, &fakeJobAnalyzer{}, time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := NewReviewScheduler(database, &runtimeCloseProvider{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewShadowRuntime(scheduler, processor, jobs, reviews, 1)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewRuntimeSupervisor(runtime, RuntimeSupervisorPolicy{time.Hour, time.Second, 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	waits := make([]time.Duration, 0, 2)
	supervisor.wait = func(_ context.Context, delay time.Duration) bool {
		waits = append(waits, delay)
		if len(waits) == 1 {
			calendar.fail = false
			return true
		}
		cancel()
		return false
	}
	if err := supervisor.Run(ctx, func() time.Time { current = current.Add(time.Millisecond); return current }); err != nil {
		t.Fatal(err)
	}
	health := supervisor.Health()
	if health.Running || health.ConsecutiveFailures != 0 || health.LastError != "" {
		t.Fatalf("expected recovered stopped health, got %+v", health)
	}
	if len(waits) != 2 {
		t.Fatalf("expected failure retry and normal interval waits, got %d", len(waits))
	}
	if waits[0] != time.Second || waits[1] != time.Hour {
		t.Fatalf("unexpected retry schedule: %v", waits)
	}
}

func TestRuntimeSupervisorRecordsFailureWithoutDroppingPartialResult(t *testing.T) {
	jobs, _, database := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, location)
	scheduler, _ := NewPostCloseScheduler(jobs, &failingTradingDay{}, &fixedCandidates{}, testPostClosePolicy(location))
	processor, _ := NewAnalysisProcessor(jobs, &fakeJobAnalyzer{}, time.Minute, time.Minute)
	reviews, _ := NewReviewScheduler(database, &runtimeCloseProvider{})
	runtime, _ := NewShadowRuntime(scheduler, processor, jobs, reviews, 1)
	supervisor, _ := NewRuntimeSupervisor(runtime, RuntimeSupervisorPolicy{time.Hour, time.Second, time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	supervisor.wait = func(context.Context, time.Duration) bool { cancel(); return false }
	if err := supervisor.Run(ctx, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	health := supervisor.Health()
	if health.ConsecutiveFailures != 1 || !strings.Contains(health.LastError, "calendar unavailable") {
		t.Fatalf("unexpected failed health: %+v", health)
	}
}
