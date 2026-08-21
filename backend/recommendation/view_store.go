package recommendation

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

// RecommendationCard is the deliberately small user-facing projection used by
// the MVP. It does not expose frozen prompts, internal scoring JSON, or secrets.
type RecommendationCard struct {
	ID                    uint       `json:"id"`
	SourceType            string     `json:"sourceType"`
	Status                string     `json:"status"`
	StockCode             string     `json:"stockCode"`
	StockName             string     `json:"stockName"`
	RiskLabels            []string   `json:"riskLabels"`
	CompletedAt           time.Time  `json:"completedAt"`
	DataAsOf              time.Time  `json:"dataAsOf"`
	BaselinePrice         float64    `json:"baselinePrice"`
	RiseProbability       float64    `json:"riseProbability"`
	ReturnRangeLow        float64    `json:"returnRangeLow"`
	ReturnRangeHigh       float64    `json:"returnRangeHigh"`
	AIRecommendationIndex int        `json:"aiRecommendationIndex"`
	Rationale             string     `json:"rationale"`
	RiskNotes             string     `json:"riskNotes"`
	ProbabilityNotice     string     `json:"probabilityNotice"`
	ReviewDueDate         time.Time  `json:"reviewDueDate"`
	IsFavorite            bool       `json:"isFavorite"`
	ActualReviewDate      *time.Time `json:"actualReviewDate"`
	ActualReturnPercent   *float64   `json:"actualReturnPercent"`
	DirectionHit          *bool      `json:"directionHit"`
	RangeHit              *bool      `json:"rangeHit"`
	OutsideRangeDeviation *float64   `json:"outsideRangeDeviation"`
	ReviewStatus          string     `json:"reviewStatus"`
}

type RecommendationViewStore struct{ db *gorm.DB }

func NewRecommendationViewStore(db *gorm.DB) (*RecommendationViewStore, error) {
	if db == nil {
		return nil, errors.New("recommendation view store requires database")
	}
	return &RecommendationViewStore{db: db}, nil
}

func (s *RecommendationViewStore) List(limit int, favoritesOnly bool) ([]RecommendationCard, error) {
	if limit < 1 || limit > 200 {
		return nil, errors.New("recommendation card limit must be between 1 and 200")
	}
	query := s.db.Model(&models.AIRecommendationSnapshot{}).
		Order("ai_recommendation_snapshots.completed_at DESC, ai_recommendation_snapshots.id DESC").Limit(limit)
	if favoritesOnly {
		query = query.Joins("JOIN ai_recommendation_favorites f ON f.recommendation_id = ai_recommendation_snapshots.id AND f.is_favorite = ?", true)
	}
	var snapshots []models.AIRecommendationSnapshot
	if err := query.Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("list recommendation snapshots: %w", err)
	}
	if len(snapshots) == 0 {
		return []RecommendationCard{}, nil
	}
	ids := make([]uint, len(snapshots))
	for index := range snapshots {
		ids[index] = snapshots[index].ID
	}
	var favorites []models.AIRecommendationFavorite
	if err := s.db.Where("recommendation_id IN ? AND is_favorite = ?", ids, true).Find(&favorites).Error; err != nil {
		return nil, fmt.Errorf("load recommendation favorites: %w", err)
	}
	var reviews []models.AIRecommendationReview
	if err := s.db.Where("recommendation_id IN ?", ids).Find(&reviews).Error; err != nil {
		return nil, fmt.Errorf("load recommendation reviews: %w", err)
	}
	favoriteByID := make(map[uint]bool, len(favorites))
	for _, favorite := range favorites {
		favoriteByID[favorite.RecommendationID] = true
	}
	reviewByID := make(map[uint]models.AIRecommendationReview, len(reviews))
	for _, review := range reviews {
		reviewByID[review.RecommendationID] = review
	}
	result := make([]RecommendationCard, 0, len(snapshots))
	for _, snapshot := range snapshots {
		var labels []string
		if err := json.Unmarshal([]byte(snapshot.RiskLabelsJSON), &labels); err != nil || labels == nil {
			return nil, fmt.Errorf("decode recommendation %d risk labels", snapshot.ID)
		}
		card := RecommendationCard{ID: snapshot.ID, SourceType: snapshot.SourceType, Status: snapshot.Status,
			StockCode: snapshot.StockCode, StockName: snapshot.StockName, RiskLabels: labels, CompletedAt: snapshot.CompletedAt,
			DataAsOf: snapshot.DataAsOf, BaselinePrice: snapshot.BaselinePrice, RiseProbability: snapshot.RiseProbability,
			ReturnRangeLow: snapshot.ReturnRangeLow, ReturnRangeHigh: snapshot.ReturnRangeHigh,
			AIRecommendationIndex: snapshot.AIRecommendationIndex, Rationale: snapshot.Rationale, RiskNotes: snapshot.RiskNotes,
			ProbabilityNotice: snapshot.ProbabilityNotice, ReviewDueDate: snapshot.ReviewDueDate, IsFavorite: favoriteByID[snapshot.ID]}
		if review, ok := reviewByID[snapshot.ID]; ok {
			card.ActualReviewDate, card.ActualReturnPercent = review.ActualReviewDate, review.ActualReturnPercent
			card.DirectionHit, card.RangeHit = review.DirectionHit, review.RangeHit
			card.OutsideRangeDeviation, card.ReviewStatus = review.OutsideRangeDeviation, review.Status
		}
		result = append(result, card)
	}
	return result, nil
}
