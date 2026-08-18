package recommendation

import (
	"errors"
	"fmt"
	"math"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UsageReserved = "reserved"
	UsageSettled  = "settled"
	UsageReleased = "released"
)

type BudgetPolicy struct {
	SoftLimitUSD float64
	HardLimitUSD float64
	Location     *time.Location
}

type BudgetDecision struct {
	Allowed          bool    `json:"allowed"`
	SoftLimitReached bool    `json:"softLimitReached"`
	CommittedUSD     float64 `json:"committedUsd"`
	ProjectedUSD     float64 `json:"projectedUsd"`
	HardLimitUSD     float64 `json:"hardLimitUsd"`
	UsageID          uint    `json:"usageId"`
}

type BudgetStore struct {
	db     *gorm.DB
	policy BudgetPolicy
}

func NewBudgetStore(db *gorm.DB, policy BudgetPolicy) (*BudgetStore, error) {
	if db == nil || policy.Location == nil || !validMoney(policy.SoftLimitUSD) || !validMoney(policy.HardLimitUSD) || policy.SoftLimitUSD > policy.HardLimitUSD {
		return nil, errors.New("budget store requires database, location, and valid ordered USD limits")
	}
	return &BudgetStore{db: db, policy: policy}, nil
}

// Reserve commits estimated cost before a paid request starts. Repeating the
// same job attempt is idempotent only when provider, model, and estimate match.
func (s *BudgetStore) Reserve(jobID uint, attempt int, provider, model string, estimateUSD float64, now time.Time) (BudgetDecision, error) {
	if jobID == 0 || attempt <= 0 || provider == "" || model == "" || !validMoney(estimateUSD) || now.IsZero() {
		return BudgetDecision{}, errors.New("job, attempt, model identity, estimate, and time are required")
	}
	decision := BudgetDecision{HardLimitUSD: s.policy.HardLimitUSD}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var job models.AIAnalysisJob
		if err := tx.First(&job, jobID).Error; err != nil {
			return fmt.Errorf("load analysis job: %w", err)
		}
		if job.Status != JobRunning || job.Attempts != attempt {
			return errors.New("budget can only be reserved for the current running job attempt")
		}
		var existing models.AIModelUsage
		err := tx.Where("job_id = ? AND attempt = ?", jobID, attempt).First(&existing).Error
		if err == nil {
			if existing.Provider != provider || existing.ModelName != model || existing.EstimatedCostUSD != estimateUSD || existing.Status == UsageReleased {
				return errors.New("job attempt already has a different or released budget reservation")
			}
			committed, err := committedSpend(tx, now, s.policy.Location)
			if err != nil {
				return err
			}
			decision.Allowed, decision.UsageID = true, existing.ID
			decision.CommittedUSD, decision.ProjectedUSD = committed, committed
			decision.SoftLimitReached = committed >= s.policy.SoftLimitUSD
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		committed, err := committedSpend(tx, now, s.policy.Location)
		if err != nil {
			return err
		}
		projected := committed + estimateUSD
		decision.CommittedUSD, decision.ProjectedUSD = round(committed, 6), round(projected, 6)
		decision.SoftLimitReached = projected >= s.policy.SoftLimitUSD
		if projected > s.policy.HardLimitUSD {
			return nil
		}
		usage := models.AIModelUsage{JobID: jobID, Attempt: attempt, Provider: provider, ModelName: model, Status: UsageReserved,
			EstimatedCostUSD: estimateUSD, ReservedAt: now}
		if err := tx.Create(&usage).Error; err != nil {
			return err
		}
		decision.Allowed, decision.UsageID = true, usage.ID
		return nil
	})
	if err != nil {
		return BudgetDecision{}, fmt.Errorf("reserve model budget: %w", err)
	}
	return decision, nil
}

func (s *BudgetStore) Settle(usageID uint, actualUSD float64, inputTokens, outputTokens int64, now time.Time) error {
	if usageID == 0 || !validMoney(actualUSD) || inputTokens < 0 || outputTokens < 0 || now.IsZero() {
		return errors.New("usage, actual cost, token counts, and settlement time are required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var usage models.AIModelUsage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&usage, usageID).Error; err != nil {
			return err
		}
		if usage.Status == UsageSettled {
			if usage.ActualCostUSD != nil && *usage.ActualCostUSD == actualUSD && usage.InputTokens == inputTokens && usage.OutputTokens == outputTokens {
				return nil
			}
			return errors.New("model usage was already settled with different values")
		}
		if usage.Status != UsageReserved {
			return errors.New("only reserved model usage can be settled")
		}
		return tx.Model(&usage).Updates(map[string]any{"status": UsageSettled, "actual_cost_usd": actualUSD,
			"input_tokens": inputTokens, "output_tokens": outputTokens, "settled_at": &now}).Error
	})
}

func (s *BudgetStore) Release(usageID uint) error {
	if usageID == 0 {
		return errors.New("usage id is required")
	}
	result := s.db.Model(&models.AIModelUsage{}).Where("id = ? AND status = ?", usageID, UsageReserved).Update("status", UsageReleased)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("only an active reservation can be released")
	}
	return nil
}

func committedSpend(tx *gorm.DB, now time.Time, location *time.Location) (float64, error) {
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	var usages []models.AIModelUsage
	if err := tx.Where("reserved_at >= ? AND reserved_at < ? AND status IN ?", start, end, []string{UsageReserved, UsageSettled}).Find(&usages).Error; err != nil {
		return 0, err
	}
	total := 0.0
	for _, usage := range usages {
		if usage.Status == UsageSettled && usage.ActualCostUSD != nil {
			total += *usage.ActualCostUSD
		} else {
			total += usage.EstimatedCostUSD
		}
	}
	return total, nil
}

func validMoney(value float64) bool { return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) }
