package main

import (
	"fmt"
	"time"

	"go-stock/backend/db"
	"go-stock/backend/recommendation"
	"gorm.io/gorm"
)

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
