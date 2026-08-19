package recommendation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-stock/backend/marketdata"
)

// ScreeningUniverseItem binds local security identity and risk labels to the
// canonical instrument requested from both end-of-day providers.
type ScreeningUniverseItem struct {
	Identity   ScreeningIdentity
	Instrument marketdata.Instrument
}

type ScreeningUniverse interface {
	Securities(context.Context, time.Time) ([]ScreeningUniverseItem, error)
}

// ValidatedScreeningProvider is the fail-closed bridge from dual-source daily
// validation to quantitative pre-screening. No record can carry a caller-made
// validation batch ID: the ID always comes from persisted reconciliation.
type ValidatedScreeningProvider struct {
	universe     ScreeningUniverse
	validation   *marketdata.EndOfDayValidationService
	store        *marketdata.ValidationStore
	lookbackDays int
}

func NewValidatedScreeningProvider(universe ScreeningUniverse, validation *marketdata.EndOfDayValidationService, store *marketdata.ValidationStore, lookbackDays int) (*ValidatedScreeningProvider, error) {
	if universe == nil || validation == nil || store == nil || lookbackDays < 30 {
		return nil, errors.New("validated screening requires universe, validation service, store, and at least 30 lookback days")
	}
	return &ValidatedScreeningProvider{universe: universe, validation: validation, store: store, lookbackDays: lookbackDays}, nil
}

func (p *ValidatedScreeningProvider) ScreeningRecords(ctx context.Context, tradeDate time.Time) ([]ScreeningRecord, error) {
	if ctx == nil || tradeDate.IsZero() {
		return nil, errors.New("validated screening requires context and trade date")
	}
	items, err := p.universe.Securities(ctx, tradeDate)
	if err != nil {
		return nil, fmt.Errorf("load screening universe: %w", err)
	}
	if len(items) == 0 {
		return nil, errors.New("screening universe is empty")
	}
	start := tradeDate.AddDate(0, 0, -p.lookbackDays)
	records := make([]ScreeningRecord, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		code := strings.TrimSpace(item.Identity.StockCode)
		if code == "" || seen[code] || item.Instrument.Code == "" || item.Instrument.Exchange == "" || item.Instrument.SecurityType == "" {
			return nil, errors.New("screening universe contains incomplete or duplicate identity")
		}
		seen[code] = true
		result, err := p.validation.Validate(ctx, item.Instrument, start, tradeDate)
		if err != nil {
			return nil, fmt.Errorf("validate screening market data for %s: %w", code, err)
		}
		batch, err := p.store.Save(result)
		if err != nil {
			return nil, fmt.Errorf("persist screening validation for %s: %w", code, err)
		}
		if !result.Passed() {
			return nil, fmt.Errorf("screening market data for %s failed dual-source reconciliation (batch %d)", code, batch.ID)
		}
		identity := item.Identity
		identity.ValidationBatchID = batch.ID
		record, err := BuildScreeningRecord(identity, result.ValidatedBars, result.Reconciliation.ExpectedDates, result.ValidatedAt, result.ValidatedAt)
		if err != nil {
			return nil, fmt.Errorf("build verified screening record for %s: %w", code, err)
		}
		records = append(records, record)
	}
	return records, nil
}
