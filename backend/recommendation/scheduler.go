package recommendation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

const (
	ReviewStatusCompleted = "completed"
	ReviewStatusDelayed   = "delayed"
)

type CloseObservation struct {
	Available          bool
	SuspensionObserved bool
	TradingDate        time.Time
	MarketTime         time.Time
	ClosePrice         float64
	Source             string
}

// CloseProvider returns the first valid close in [dueDate, asOf]. It must not
// substitute an intraday quote or a close before the original due date.
type CloseProvider interface {
	FirstValidClose(context.Context, string, time.Time, time.Time) (CloseObservation, error)
}

type ReviewScheduler struct {
	db     *gorm.DB
	closes CloseProvider
}

func NewReviewScheduler(db *gorm.DB, closes CloseProvider) (*ReviewScheduler, error) {
	if db == nil || closes == nil {
		return nil, errors.New("review scheduler requires a database and close provider")
	}
	return &ReviewScheduler{db: db, closes: closes}, nil
}

// ProcessDue is idempotent: completed rows are excluded and the final update is
// conditional, so retries cannot overwrite an already completed review.
func (s *ReviewScheduler) ProcessDue(ctx context.Context, asOf time.Time) (int, error) {
	if asOf.IsZero() {
		return 0, errors.New("review cutoff time is required")
	}
	var reviews []models.AIRecommendationReview
	if err := s.db.WithContext(ctx).
		Where("original_due_date <= ? AND status <> ?", asOf, ReviewStatusCompleted).
		Order("original_due_date, id").Find(&reviews).Error; err != nil {
		return 0, fmt.Errorf("list due recommendation reviews: %w", err)
	}

	completed := 0
	for i := range reviews {
		changed, err := s.processOne(ctx, &reviews[i], asOf)
		if err != nil {
			return completed, err
		}
		if changed {
			completed++
		}
	}
	return completed, nil
}

func (s *ReviewScheduler) processOne(ctx context.Context, review *models.AIRecommendationReview, asOf time.Time) (bool, error) {
	var snapshot models.AIRecommendationSnapshot
	if err := s.db.WithContext(ctx).First(&snapshot, review.RecommendationID).Error; err != nil {
		return false, fmt.Errorf("load recommendation %d: %w", review.RecommendationID, err)
	}
	observation, err := s.closes.FirstValidClose(ctx, snapshot.StockCode, review.OriginalDueDate, asOf)
	if err != nil {
		return false, fmt.Errorf("load valid close for recommendation %d: %w", review.RecommendationID, err)
	}
	if !observation.Available {
		reason := "valid close unavailable"
		if observation.SuspensionObserved {
			reason = "suspended on review due date; waiting for first valid close"
		}
		return false, s.db.WithContext(ctx).Model(review).Where("status <> ?", ReviewStatusCompleted).Updates(map[string]any{
			"status": ReviewStatusDelayed, "delay_reason": reason,
		}).Error
	}
	if observation.TradingDate.Before(review.OriginalDueDate) || observation.TradingDate.After(asOf) {
		return false, errors.New("close provider returned a date outside the requested review window")
	}
	if observation.MarketTime.IsZero() || observation.Source == "" {
		return false, errors.New("close observation requires market time and source")
	}
	result, err := CalculateReview(snapshot.BaselinePrice, observation.ClosePrice, snapshot.ReturnRangeLow, snapshot.ReturnRangeHigh)
	if err != nil {
		return false, fmt.Errorf("calculate recommendation %d review: %w", review.RecommendationID, err)
	}
	updates := map[string]any{
		"actual_review_date":      observation.TradingDate,
		"actual_close_price":      observation.ClosePrice,
		"actual_return_percent":   result.ActualReturnPercent,
		"direction_hit":           result.DirectionHit,
		"range_hit":               result.RangeHit,
		"outside_range_deviation": result.OutsideRangeDeviation,
		"market_time":             observation.MarketTime,
		"data_source":             observation.Source,
		"status":                  ReviewStatusCompleted,
		"delay_reason":            "",
	}
	query := s.db.WithContext(ctx).Model(review).Where("status <> ?", ReviewStatusCompleted).Updates(updates)
	return query.RowsAffected == 1, query.Error
}
