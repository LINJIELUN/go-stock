package recommendation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	concurrency  int
}

func NewValidatedScreeningProvider(universe ScreeningUniverse, validation *marketdata.EndOfDayValidationService, store *marketdata.ValidationStore, lookbackDays, concurrency int) (*ValidatedScreeningProvider, error) {
	if universe == nil || validation == nil || store == nil || lookbackDays < 30 || concurrency <= 0 || concurrency > 32 {
		return nil, errors.New("validated screening requires universe, validation service, store, at least 30 lookback days, and concurrency 1-32")
	}
	return &ValidatedScreeningProvider{universe: universe, validation: validation, store: store, lookbackDays: lookbackDays, concurrency: concurrency}, nil
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
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		code := strings.TrimSpace(item.Identity.StockCode)
		if code == "" || seen[code] || item.Instrument.Code == "" || item.Instrument.Exchange == "" || item.Instrument.SecurityType == "" {
			return nil, errors.New("screening universe contains incomplete or duplicate identity")
		}
		seen[code] = true
	}
	type validationOutcome struct {
		result marketdata.EndOfDayValidationResult
		err    error
	}
	outcomes := make([]validationOutcome, len(items))
	work := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(p.concurrency, len(items))
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range work {
				outcomes[index].result, outcomes[index].err = p.validation.Validate(ctx, items[index].Instrument, start, tradeDate)
			}
		}()
	}
	for index := range items {
		work <- index
	}
	close(work)
	workers.Wait()

	records := make([]ScreeningRecord, 0, len(items))
	var firstFailure error
	for index, item := range items {
		code := strings.TrimSpace(item.Identity.StockCode)
		if outcomes[index].err != nil {
			if firstFailure == nil {
				firstFailure = fmt.Errorf("validate screening market data for %s: %w", code, outcomes[index].err)
			}
			continue
		}
		result := outcomes[index].result
		batch, err := p.store.Save(result)
		if err != nil {
			if firstFailure == nil {
				firstFailure = fmt.Errorf("persist screening validation for %s: %w", code, err)
			}
			continue
		}
		if !result.Passed() {
			if firstFailure == nil {
				firstFailure = fmt.Errorf("screening market data for %s failed dual-source reconciliation (batch %d)", code, batch.ID)
			}
			continue
		}
		identity := item.Identity
		identity.ValidationBatchID = batch.ID
		record, err := BuildScreeningRecord(identity, result.ValidatedBars, result.Reconciliation.ExpectedDates, result.ValidatedAt, result.ValidatedAt)
		if err != nil {
			if firstFailure == nil {
				firstFailure = fmt.Errorf("build verified screening record for %s: %w", code, err)
			}
			continue
		}
		records = append(records, record)
	}
	if firstFailure != nil {
		return nil, firstFailure
	}
	return records, nil
}
