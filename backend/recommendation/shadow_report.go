package recommendation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

// ShadowReport exposes measured evidence without deciding whether automatic
// recommendations may be promoted. Promotion thresholds remain a product and
// risk decision after enough seven-trading-day outcomes exist.
type ShadowReport struct {
	WindowStart              time.Time `json:"windowStart"`
	WindowEnd                time.Time `json:"windowEnd"`
	StrategyVersion          string    `json:"strategyVersion"`
	GeneratedSnapshots       int       `json:"generatedSnapshots"`
	CompletedReviews         int       `json:"completedReviews"`
	DelayedReviews           int       `json:"delayedReviews"`
	PendingReviews           int       `json:"pendingReviews"`
	DirectionHitRatePercent  *float64  `json:"directionHitRatePercent"`
	RangeHitRatePercent      *float64  `json:"rangeHitRatePercent"`
	MeanActualReturnPercent  *float64  `json:"meanActualReturnPercent"`
	MeanOutsideDeviation     *float64  `json:"meanOutsideDeviation"`
	SettledModelCostUSD      float64   `json:"settledModelCostUsd"`
	CommittedModelCostUSD    float64   `json:"committedModelCostUsd"`
	UncertainModelUsageCount int       `json:"uncertainModelUsageCount"`
}

type ShadowReportStore struct{ db *gorm.DB }

func NewShadowReportStore(db *gorm.DB) (*ShadowReportStore, error) {
	if db == nil {
		return nil, errors.New("shadow report store requires a database")
	}
	return &ShadowReportStore{db: db}, nil
}

func (s *ShadowReportStore) Build(start, end time.Time, strategyVersion string) (ShadowReport, error) {
	strategyVersion = strings.TrimSpace(strategyVersion)
	report := ShadowReport{WindowStart: start, WindowEnd: end, StrategyVersion: strategyVersion}
	if start.IsZero() || end.IsZero() || !start.Before(end) || strategyVersion == "" {
		return report, errors.New("shadow report requires an ordered non-empty time window and strategy version")
	}
	var snapshots []models.AIRecommendationSnapshot
	if err := s.db.Where("source_type = ? AND status = ? AND strategy_version = ? AND completed_at >= ? AND completed_at < ?",
		SourceAutomatic, RecommendationStatusShadow, strategyVersion, start, end).Find(&snapshots).Error; err != nil {
		return report, fmt.Errorf("load shadow snapshots: %w", err)
	}
	report.GeneratedSnapshots = len(snapshots)
	if len(snapshots) == 0 {
		return report, nil
	}
	ids := make([]uint, len(snapshots))
	for index := range snapshots {
		ids[index] = snapshots[index].ID
	}
	var reviews []models.AIRecommendationReview
	if err := s.db.Where("recommendation_id IN ?", ids).Find(&reviews).Error; err != nil {
		return report, fmt.Errorf("load shadow reviews: %w", err)
	}
	directionHits, rangeHits := 0, 0
	actualReturnTotal, outsideDeviationTotal := 0.0, 0.0
	for _, review := range reviews {
		switch review.Status {
		case ReviewStatusCompleted:
			if review.DirectionHit == nil || review.RangeHit == nil || review.ActualReturnPercent == nil || review.OutsideRangeDeviation == nil {
				return report, errors.New("completed shadow review is missing outcome metrics")
			}
			if !finite(*review.ActualReturnPercent) || !finite(*review.OutsideRangeDeviation) || *review.OutsideRangeDeviation < 0 {
				return report, errors.New("completed shadow review has invalid outcome metrics")
			}
			report.CompletedReviews++
			if *review.DirectionHit {
				directionHits++
			}
			if *review.RangeHit {
				rangeHits++
			}
			actualReturnTotal += *review.ActualReturnPercent
			outsideDeviationTotal += *review.OutsideRangeDeviation
		case ReviewStatusDelayed:
			report.DelayedReviews++
		}
	}
	report.PendingReviews = max(0, report.GeneratedSnapshots-report.CompletedReviews-report.DelayedReviews)
	if report.CompletedReviews > 0 {
		report.DirectionHitRatePercent = reportValue(float64(directionHits) / float64(report.CompletedReviews) * 100)
		report.RangeHitRatePercent = reportValue(float64(rangeHits) / float64(report.CompletedReviews) * 100)
		report.MeanActualReturnPercent = reportValue(actualReturnTotal / float64(report.CompletedReviews))
		report.MeanOutsideDeviation = reportValue(outsideDeviationTotal / float64(report.CompletedReviews))
	}
	if err := s.addUsageMetrics(&report, ids); err != nil {
		return report, err
	}
	return report, nil
}

func (s *ShadowReportStore) addUsageMetrics(report *ShadowReport, recommendationIDs []uint) error {
	var jobs []models.AIAnalysisJob
	if err := s.db.Where("recommendation_id IN ?", recommendationIDs).Find(&jobs).Error; err != nil {
		return fmt.Errorf("load shadow jobs: %w", err)
	}
	jobIDs := make([]uint, len(jobs))
	for index := range jobs {
		jobIDs[index] = jobs[index].ID
	}
	if len(jobIDs) == 0 {
		return nil
	}
	var usages []models.AIModelUsage
	if err := s.db.Where("job_id IN ?", jobIDs).Find(&usages).Error; err != nil {
		return fmt.Errorf("load shadow model usage: %w", err)
	}
	for _, usage := range usages {
		if !finite(usage.EstimatedCostUSD) || usage.EstimatedCostUSD < 0 ||
			(usage.ActualCostUSD != nil && (!finite(*usage.ActualCostUSD) || *usage.ActualCostUSD < 0)) {
			return errors.New("shadow model usage has invalid cost metrics")
		}
		switch usage.Status {
		case UsageSettled:
			if usage.ActualCostUSD == nil {
				return errors.New("settled shadow model usage is missing actual cost")
			}
			report.SettledModelCostUSD += *usage.ActualCostUSD
			report.CommittedModelCostUSD += *usage.ActualCostUSD
		case UsageReserved, UsageUncertain:
			report.CommittedModelCostUSD += usage.EstimatedCostUSD
			if usage.Status == UsageUncertain {
				report.UncertainModelUsageCount++
			}
		case UsageReleased:
		default:
			return fmt.Errorf("shadow model usage has unsupported status %q", usage.Status)
		}
	}
	report.SettledModelCostUSD = round(report.SettledModelCostUSD, 6)
	report.CommittedModelCostUSD = round(report.CommittedModelCostUSD, 6)
	return nil
}

func reportValue(value float64) *float64 {
	rounded := round(value, 4)
	return &rounded
}
