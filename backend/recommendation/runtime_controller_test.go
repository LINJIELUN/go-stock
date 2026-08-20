package recommendation

import (
	"context"
	"testing"
	"time"
)

func testRuntimeController(t *testing.T) (*RuntimeController, *RuntimeSupervisor) {
	t.Helper()
	jobs, _, database := analysisJobStore(t)
	location, _ := time.LoadLocation("Asia/Shanghai")
	scheduler, err := NewPostCloseScheduler(jobs, fixedTradingDay{trading: false}, &fixedCandidates{}, testPostClosePolicy(location))
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
	supervisor, err := NewRuntimeSupervisor(runtime, RuntimeSupervisorPolicy{time.Hour, time.Second, time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewRuntimeController(supervisor, func() time.Time {
		return time.Date(2026, 8, 19, 16, 0, 0, 0, location)
	})
	if err != nil {
		t.Fatal(err)
	}
	return controller, supervisor
}

func TestRuntimeControllerStartsOnceAndStops(t *testing.T) {
	controller, supervisor := testRuntimeController(t)
	waiting := make(chan struct{}, 1)
	supervisor.wait = func(ctx context.Context, _ time.Duration) bool {
		waiting <- struct{}{}
		<-ctx.Done()
		return false
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	if health := controller.Health(); !health.Configured || !health.Running {
		t.Fatalf("unexpected running health: %+v", health)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if health := controller.Health(); health.Running || health.Runtime.Running {
		t.Fatalf("runtime remained active after stop: %+v", health)
	}
	if err := controller.Stop(stopCtx); err != nil {
		t.Fatalf("repeated stop should be safe: %v", err)
	}
}

func TestRuntimeControllerCanRestartAfterStop(t *testing.T) {
	controller, supervisor := testRuntimeController(t)
	supervisor.wait = func(ctx context.Context, _ time.Duration) bool {
		<-ctx.Done()
		return false
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := controller.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for !controller.Health().Running && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := controller.Stop(stopCtx); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
	}
}
