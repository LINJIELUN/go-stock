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
func (a *App) GetAIShadowReport(startRFC3339, endRFC3339, strategyVersion string) (recommendation.ShadowReport, error) {
	return buildAIShadowReport(db.Dao, startRFC3339, endRFC3339, strategyVersion)
}

func buildAIShadowReport(database *gorm.DB, startRFC3339, endRFC3339, strategyVersion string) (recommendation.ShadowReport, error) {
	start, err := time.Parse(time.RFC3339Nano, startRFC3339)
	if err != nil {
		return recommendation.ShadowReport{}, fmt.Errorf("parse shadow report start as RFC3339: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, endRFC3339)
	if err != nil {
		return recommendation.ShadowReport{}, fmt.Errorf("parse shadow report end as RFC3339: %w", err)
	}
	store, err := recommendation.NewShadowReportStore(database)
	if err != nil {
		return recommendation.ShadowReport{}, err
	}
	return store.Build(start.UTC(), end.UTC(), strategyVersion)
}
