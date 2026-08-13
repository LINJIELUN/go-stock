package recommendation

import (
	"context"
	"testing"
	"time"

	"go-stock/backend/models"
)

type fakeCloseProvider struct {
	observation CloseObservation
	calls       int
}

func (f *fakeCloseProvider) FirstValidClose(context.Context, string, time.Time, time.Time) (CloseObservation, error) {
	f.calls++
	return f.observation, nil
}

func TestReviewSchedulerCompletesDueReviewOnce(t *testing.T) {
	store, database := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := validSnapshot(now.AddDate(0, 0, -10))
	snapshot.ReviewDueDate = now.AddDate(0, 0, -1)
	if err := store.CreateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	provider := &fakeCloseProvider{observation: CloseObservation{
		Available: true, TradingDate: now, MarketTime: now.Add(15 * time.Hour), ClosePrice: 10.5, Source: "verified-test-feed",
	}}
	scheduler, err := NewReviewScheduler(database, provider)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := scheduler.ProcessDue(context.Background(), now.AddDate(0, 0, 1))
	if err != nil || completed != 1 {
		t.Fatalf("expected one completion, got %d, %v", completed, err)
	}
	completed, err = scheduler.ProcessDue(context.Background(), now.AddDate(0, 0, 1))
	if err != nil || completed != 0 || provider.calls != 1 {
		t.Fatalf("completed review was processed again: completed=%d calls=%d err=%v", completed, provider.calls, err)
	}
	var review models.AIRecommendationReview
	if err := database.Where("recommendation_id = ?", snapshot.ID).First(&review).Error; err != nil {
		t.Fatal(err)
	}
	if review.Status != ReviewStatusCompleted || review.ActualReturnPercent == nil || *review.ActualReturnPercent != 5 {
		t.Fatalf("unexpected completed review: %+v", review)
	}
}

func TestReviewSchedulerMarksSuspensionAndLaterCompletes(t *testing.T) {
	store, database := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := validSnapshot(now.AddDate(0, 0, -10))
	snapshot.ReviewDueDate = now.AddDate(0, 0, -1)
	if err := store.CreateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	provider := &fakeCloseProvider{observation: CloseObservation{SuspensionObserved: true}}
	scheduler, _ := NewReviewScheduler(database, provider)
	if completed, err := scheduler.ProcessDue(context.Background(), now); err != nil || completed != 0 {
		t.Fatalf("unexpected delayed pass: %d, %v", completed, err)
	}
	var delayed models.AIRecommendationReview
	database.Where("recommendation_id = ?", snapshot.ID).First(&delayed)
	if delayed.Status != ReviewStatusDelayed || delayed.DelayReason == "" {
		t.Fatalf("suspension was not recorded: %+v", delayed)
	}
	provider.observation = CloseObservation{
		Available: true, TradingDate: now.AddDate(0, 0, 2), MarketTime: now.AddDate(0, 0, 2).Add(15 * time.Hour),
		ClosePrice: 9, Source: "verified-test-feed",
	}
	if completed, err := scheduler.ProcessDue(context.Background(), now.AddDate(0, 0, 2)); err != nil || completed != 1 {
		t.Fatalf("expected completion after resumption: %d, %v", completed, err)
	}
}

func TestReviewSchedulerRejectsCloseOutsideWindow(t *testing.T) {
	store, database := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := validSnapshot(now.AddDate(0, 0, -10))
	snapshot.ReviewDueDate = now.AddDate(0, 0, -1)
	if err := store.CreateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	provider := &fakeCloseProvider{observation: CloseObservation{
		Available: true, TradingDate: now.AddDate(0, 0, -2), MarketTime: now, ClosePrice: 10, Source: "bad-feed",
	}}
	scheduler, _ := NewReviewScheduler(database, provider)
	if _, err := scheduler.ProcessDue(context.Background(), now); err == nil {
		t.Fatal("expected out-of-window close to be rejected")
	}
}
