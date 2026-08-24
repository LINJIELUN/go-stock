package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go-stock/backend/db"
	"go-stock/backend/recommendation"
	"gorm.io/gorm"
)

type processRuntimeEnvironment struct{}

func (processRuntimeEnvironment) LookupEnv(key string) (string, bool) { return os.LookupEnv(key) }

// GetAIShadowRuntimeReadiness reports configuration blockers without exposing
// provider tokens or model API keys.
func (a *App) GetAIShadowRuntimeReadiness() (recommendation.RuntimeReadiness, error) {
	config, err := recommendation.LoadRuntimeConfig(processRuntimeEnvironment{})
	if err != nil {
		return recommendation.RuntimeReadiness{}, err
	}
	return config.Readiness(), nil
}

func (a *App) setAIShadowRuntime(controller *recommendation.RuntimeController) error {
	if controller == nil {
		return fmt.Errorf("AI shadow runtime controller is required")
	}
	a.shadowRuntimeMu.Lock()
	defer a.shadowRuntimeMu.Unlock()
	if a.shadowRuntime != nil && a.shadowRuntime != controller {
		return fmt.Errorf("AI shadow runtime controller is already configured")
	}
	a.shadowRuntime = controller
	return nil
}

func (a *App) startAIShadowRuntime(ctx context.Context) error {
	a.shadowRuntimeMu.RLock()
	controller := a.shadowRuntime
	a.shadowRuntimeMu.RUnlock()
	if controller == nil {
		return nil
	}
	return controller.Start(ctx)
}

func (a *App) stopAIShadowRuntime(ctx context.Context) error {
	a.shadowRuntimeMu.RLock()
	controller := a.shadowRuntime
	a.shadowRuntimeMu.RUnlock()
	if controller == nil {
		return nil
	}
	return controller.Stop(ctx)
}

// GetAIShadowRuntimeHealth is read-only and reports Configured=false when no
// production providers and model runtime have been explicitly assembled.
func (a *App) GetAIShadowRuntimeHealth() recommendation.RuntimeControllerHealth {
	a.shadowRuntimeMu.RLock()
	controller := a.shadowRuntime
	a.shadowRuntimeMu.RUnlock()
	if controller == nil {
		return recommendation.RuntimeControllerHealth{}
	}
	return controller.Health()
}

// GetAIRecommendationCards returns the small user-facing projection used by
// the recommendation and review pages.
func (a *App) GetAIRecommendationCards(limit int, favoritesOnly bool) ([]recommendation.RecommendationCard, error) {
	store, err := recommendation.NewRecommendationViewStore(db.Dao)
	if err != nil {
		return nil, err
	}
	return store.List(limit, favoritesOnly)
}

func (a *App) SearchAIRecommendationCards(query string, limit int) ([]recommendation.RecommendationCard, error) {
	store, err := recommendation.NewRecommendationViewStore(db.Dao)
	if err != nil {
		return nil, err
	}
	return store.Search(query, limit)
}

// SetAIRecommendationFavorite changes only the favorite relationship. The
// immutable prediction and its later review are deliberately retained.
func (a *App) SetAIRecommendationFavorite(recommendationID uint, favorite bool) error {
	store, err := recommendation.NewStore(db.Dao)
	if err != nil {
		return err
	}
	return store.SetFavorite(recommendationID, favorite, time.Now().UTC())
}

// GetAIShadowReport exposes read-only shadow evidence to the desktop client.
// Both boundaries require an explicit RFC3339 offset so the selected cohort is
// reproducible regardless of the machine's local timezone.
func (a *App) GetAIShadowReport(startRFC3339, endRFC3339, strategyVersion, modelVersion, promptVersion string) (recommendation.ShadowReport, error) {
	return buildAIShadowReport(db.Dao, startRFC3339, endRFC3339, strategyVersion, modelVersion, promptVersion)
}

// ListAIShadowCohorts lets the client discover reportable configurations
// instead of guessing or hard-coding version identifiers.
func (a *App) ListAIShadowCohorts(startRFC3339, endRFC3339 string) ([]recommendation.ShadowCohort, error) {
	return listAIShadowCohorts(db.Dao, startRFC3339, endRFC3339)
}

func listAIShadowCohorts(database *gorm.DB, startRFC3339, endRFC3339 string) ([]recommendation.ShadowCohort, error) {
	start, end, err := parseAIShadowWindow(startRFC3339, endRFC3339)
	if err != nil {
		return nil, err
	}
	store, err := recommendation.NewShadowReportStore(database)
	if err != nil {
		return nil, err
	}
	return store.ListCohorts(start, end)
}

func buildAIShadowReport(database *gorm.DB, startRFC3339, endRFC3339, strategyVersion, modelVersion, promptVersion string) (recommendation.ShadowReport, error) {
	start, end, err := parseAIShadowWindow(startRFC3339, endRFC3339)
	if err != nil {
		return recommendation.ShadowReport{}, err
	}
	store, err := recommendation.NewShadowReportStore(database)
	if err != nil {
		return recommendation.ShadowReport{}, err
	}
	return store.Build(start, end, strategyVersion, modelVersion, promptVersion)
}

func parseAIShadowWindow(startRFC3339, endRFC3339 string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339Nano, startRFC3339)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse shadow report start as RFC3339: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, endRFC3339)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse shadow report end as RFC3339: %w", err)
	}
	return start.UTC(), end.UTC(), nil
}
