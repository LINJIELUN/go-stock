package recommendation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go-stock/backend/models"
)

var ErrHardBudgetExceeded = errors.New("monthly AI hard budget exceeded")

type ModelPlan struct {
	Provider         string
	Model            string
	EstimatedCostUSD float64
}

type PaidAnalysisResult struct {
	Snapshot        *models.AIRecommendationSnapshot
	RequestSent     bool
	ActualCostKnown bool
	ActualCostUSD   float64
	InputTokens     int64
	OutputTokens    int64
}

type PaidAnalyzerClient interface {
	Plan(context.Context, models.AIAnalysisJob) (ModelPlan, error)
	Analyze(context.Context, models.AIAnalysisJob, ModelPlan) (PaidAnalysisResult, error)
}

// BudgetedAnalyzer is the mandatory boundary for paid model clients. It never
// sends a request without a reservation and never releases possibly charged use.
type BudgetedAnalyzer struct {
	budget *BudgetStore
	client PaidAnalyzerClient
	now    func() time.Time
}

func NewBudgetedAnalyzer(budget *BudgetStore, client PaidAnalyzerClient) (*BudgetedAnalyzer, error) {
	if budget == nil || client == nil {
		return nil, errors.New("budgeted analyzer requires budget store and paid client")
	}
	return &BudgetedAnalyzer{budget: budget, client: client, now: time.Now}, nil
}

func (a *BudgetedAnalyzer) Analyze(ctx context.Context, job models.AIAnalysisJob) (*models.AIRecommendationSnapshot, error) {
	plan, err := a.client.Plan(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("plan paid analysis: %w", err)
	}
	decision, err := a.budget.Reserve(job.ID, job.Attempts, plan.Provider, plan.Model, plan.EstimatedCostUSD, a.now())
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, ErrHardBudgetExceeded
	}
	result, analyzeErr := a.client.Analyze(ctx, job, plan)
	if !result.RequestSent {
		if err := a.budget.Release(decision.UsageID); err != nil {
			return nil, fmt.Errorf("release unsent model reservation: %w", err)
		}
		if analyzeErr == nil {
			analyzeErr = errors.New("paid analyzer returned without sending a request")
		}
		return nil, analyzeErr
	}
	if !result.ActualCostKnown {
		if err := a.budget.MarkUncertain(decision.UsageID); err != nil {
			return nil, fmt.Errorf("mark model cost uncertain: %w", err)
		}
		if analyzeErr == nil {
			analyzeErr = errors.New("paid analyzer did not return authoritative cost")
		}
		return nil, analyzeErr
	}
	if err := a.budget.Settle(decision.UsageID, result.ActualCostUSD, result.InputTokens, result.OutputTokens, a.now()); err != nil {
		return nil, fmt.Errorf("settle paid analysis: %w", err)
	}
	if analyzeErr != nil {
		return nil, analyzeErr
	}
	if result.Snapshot == nil {
		return nil, errors.New("paid analyzer returned no recommendation snapshot")
	}
	return result.Snapshot, nil
}
