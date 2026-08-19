package recommendation

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

const (
	SourceAutomatic = "automatic"
	SourceManual    = "manual"
	StatusPending   = "pending_review"
)

// Store is the only persistence entry point for recommendation snapshots. Keeping
// snapshot creation here prevents API and scheduler callers from silently writing
// incomplete records or updating an historical prediction in place.
type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("recommendation store requires a database")
	}
	return &Store{db: db}, nil
}

// CreateSnapshot persists the immutable prediction and its pending review atomically.
func (s *Store) CreateSnapshot(snapshot *models.AIRecommendationSnapshot) error {
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := requireValidationBatch(tx, snapshot); err != nil {
			return err
		}
		if err := tx.Create(snapshot).Error; err != nil {
			return fmt.Errorf("create recommendation snapshot: %w", err)
		}
		review := models.AIRecommendationReview{
			RecommendationID: snapshot.ID,
			OriginalDueDate:  snapshot.ReviewDueDate,
			Status:           StatusPending,
		}
		if err := tx.Create(&review).Error; err != nil {
			return fmt.Errorf("create pending recommendation review: %w", err)
		}
		return nil
	})
}

func requireValidationBatch(tx *gorm.DB, snapshot *models.AIRecommendationSnapshot) error {
	var batch models.MarketDataValidationBatch
	if err := tx.First(&batch, snapshot.ValidationBatchID).Error; err != nil {
		return fmt.Errorf("load market data validation batch: %w", err)
	}
	if batch.Status != "passed" || batch.ExpectedDates <= 0 || batch.ReleasedBars != batch.ExpectedDates {
		return errors.New("market data validation batch is not approved for recommendation analysis")
	}
	batchCode := strings.SplitN(batch.InstrumentCode, ".", 2)[0]
	snapshotCode := strings.SplitN(snapshot.StockCode, ".", 2)[0]
	if batchCode == "" || batchCode != snapshotCode {
		return errors.New("market data validation batch does not match recommendation stock")
	}
	dataDate := snapshot.DataAsOf.Format(time.DateOnly)
	if dataDate < batch.RangeStart.Format(time.DateOnly) || dataDate > batch.RangeEnd.Format(time.DateOnly) {
		return errors.New("recommendation data time is outside validation batch range")
	}
	return nil
}

// SetFavorite toggles the relationship without deleting the recommendation history.
func (s *Store) SetFavorite(recommendationID uint, favorite bool, at time.Time) error {
	if recommendationID == 0 || at.IsZero() {
		return errors.New("recommendation id and event time are required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.AIRecommendationSnapshot{}).Where("id = ?", recommendationID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}

		var relation models.AIRecommendationFavorite
		err := tx.Where("recommendation_id = ?", recommendationID).First(&relation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !favorite {
				return nil
			}
			relation = models.AIRecommendationFavorite{
				RecommendationID: recommendationID,
				IsFavorite:       true,
				FavoritedAt:      at,
			}
			return tx.Create(&relation).Error
		}
		if err != nil {
			return err
		}

		updates := map[string]any{"is_favorite": favorite}
		if favorite {
			updates["favorited_at"] = at
			updates["unfavorited_at"] = nil
		} else {
			updates["unfavorited_at"] = at
		}
		return tx.Model(&relation).Updates(updates).Error
	})
}

func validateSnapshot(snapshot *models.AIRecommendationSnapshot) error {
	if snapshot == nil {
		return errors.New("recommendation snapshot is required")
	}
	if snapshot.ID != 0 {
		return errors.New("new recommendation snapshot must not already have an id")
	}
	if snapshot.SourceType != SourceAutomatic && snapshot.SourceType != SourceManual {
		return errors.New("source type must be automatic or manual")
	}
	if snapshot.ValidationBatchID == 0 {
		return errors.New("validated market data batch is required")
	}
	if snapshot.StockCode == "" || snapshot.StockName == "" {
		return errors.New("stock code and name are required")
	}
	if !finite(snapshot.BaselinePrice) || snapshot.BaselinePrice <= 0 {
		return errors.New("baseline price must be positive and finite")
	}
	if !finite(snapshot.RiseProbability) || snapshot.RiseProbability < 0 || snapshot.RiseProbability > 100 {
		return errors.New("rise probability must be between 0 and 100")
	}
	if !finite(snapshot.ReturnRangeLow) || !finite(snapshot.ReturnRangeHigh) || snapshot.ReturnRangeLow > snapshot.ReturnRangeHigh {
		return errors.New("predicted return range is invalid")
	}
	if snapshot.AIRecommendationIndex < 0 || snapshot.AIRecommendationIndex > 100 {
		return errors.New("AI recommendation index must be between 0 and 100")
	}
	if snapshot.CompletedAt.IsZero() || snapshot.DataAsOf.IsZero() || snapshot.BaselineMarketTime.IsZero() || snapshot.ReviewDueDate.IsZero() {
		return errors.New("analysis, market, and review times are required")
	}
	if snapshot.ReviewDueDate.Before(snapshot.CompletedAt) {
		return errors.New("review due date cannot precede analysis completion")
	}
	if snapshot.ModelVersion == "" || snapshot.PromptVersion == "" || snapshot.StrategyVersion == "" || snapshot.Status == "" || snapshot.ProbabilityNotice == "" {
		return errors.New("model, prompt, strategy, status, and probability notice are required")
	}
	if err := validJSONObject(snapshot.ScoreComponentsJSON); err != nil {
		return fmt.Errorf("score components: %w", err)
	}
	if err := validJSONArray(snapshot.PenaltiesJSON); err != nil {
		return fmt.Errorf("penalties: %w", err)
	}
	if err := validJSONArray(snapshot.RiskLabelsJSON); err != nil {
		return fmt.Errorf("risk labels: %w", err)
	}
	if err := validJSONArray(snapshot.EvidenceJSON); err != nil {
		return fmt.Errorf("evidence: %w", err)
	}
	if err := validJSONObject(snapshot.AgentConclusionsJSON); err != nil {
		return fmt.Errorf("agent conclusions: %w", err)
	}
	return nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validJSONObject(value string) error {
	var decoded map[string]any
	if value == "" || json.Unmarshal([]byte(value), &decoded) != nil || decoded == nil {
		return errors.New("must be a JSON object")
	}
	return nil
}

func validJSONArray(value string) error {
	var decoded []any
	if value == "" || json.Unmarshal([]byte(value), &decoded) != nil || decoded == nil {
		return errors.New("must be a JSON array")
	}
	return nil
}
