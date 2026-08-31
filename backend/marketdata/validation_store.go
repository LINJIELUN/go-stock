package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

const (
	ValidationStatusPassed = "passed"
	ValidationStatusFailed = "failed"
)

// ValidationStore persists validation evidence independently from recommendation
// output. Saving the same result is idempotent, while corrected provider data
// produces a new immutable batch.
type ValidationStore struct {
	db *gorm.DB
}

func NewValidationStore(db *gorm.DB) (*ValidationStore, error) {
	if db == nil {
		return nil, errors.New("market data validation store requires a database")
	}
	return &ValidationStore{db: db}, nil
}

func (s *ValidationStore) Save(result EndOfDayValidationResult) (*models.MarketDataValidationBatch, error) {
	if err := validateStoredResult(result); err != nil {
		return nil, err
	}
	evidence, err := json.Marshal(result.Reconciliation)
	if err != nil {
		return nil, fmt.Errorf("encode reconciliation evidence: %w", err)
	}
	status := ValidationStatusFailed
	if result.Passed() {
		status = ValidationStatusPassed
	}
	batch := models.MarketDataValidationBatch{
		BatchKey:           validationBatchKey(result, evidence),
		InstrumentCode:     result.Instrument.CanonicalCode(),
		Exchange:           string(result.Instrument.Exchange),
		SecurityType:       string(result.Instrument.SecurityType),
		RangeStart:         result.Start,
		RangeEnd:           result.End,
		CalendarSource:     result.CalendarSource,
		PrimarySource:      result.PrimarySource,
		ReferenceSource:    result.ReferenceSource,
		ValidatedAt:        result.ValidatedAt,
		Status:             status,
		ExpectedDates:      result.Reconciliation.ExpectedDates,
		ReleasedBars:       len(result.ValidatedBars),
		ReconciliationJSON: string(evidence),
	}
	var stored models.MarketDataValidationBatch
	err = s.db.Transaction(func(tx *gorm.DB) error {
		lookup := tx.Where("batch_key = ?", batch.BatchKey).First(&stored)
		if lookup.Error == nil {
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		stored = batch
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist market data validation batch: %w", err)
	}
	return &stored, nil
}

// RequirePassed is the persistence-level gate used before starting an AI job.
func (s *ValidationStore) RequirePassed(batchID uint) (*models.MarketDataValidationBatch, error) {
	if batchID == 0 {
		return nil, errors.New("validation batch id is required")
	}
	var batch models.MarketDataValidationBatch
	if err := s.db.First(&batch, batchID).Error; err != nil {
		return nil, err
	}
	if batch.Status != ValidationStatusPassed || batch.ReleasedBars != batch.ExpectedDates || batch.ExpectedDates <= 0 {
		return nil, errors.New("market data validation batch is not approved for AI analysis")
	}
	return &batch, nil
}

func validateStoredResult(result EndOfDayValidationResult) error {
	if result.Instrument.Code == "" || result.Instrument.Exchange == "" || result.Instrument.SecurityType == "" {
		return errors.New("validation result instrument is incomplete")
	}
	if result.Start.IsZero() || result.End.IsZero() || result.Start.After(result.End) || result.ValidatedAt.IsZero() {
		return errors.New("validation result times are invalid")
	}
	if result.CalendarSource == "" || result.PrimarySource == "" || result.ReferenceSource == "" {
		return errors.New("validation result sources are required")
	}
	if result.PrimarySource == result.ReferenceSource {
		return errors.New("validation result sources must be independent")
	}
	if result.Reconciliation.ExpectedDates <= 0 {
		return errors.New("validation result must contain expected trading dates")
	}
	if len(result.ValidatedBars) != 0 && !result.Passed() {
		return errors.New("failed validation result cannot release daily bars")
	}
	return nil
}

func validationBatchKey(result EndOfDayValidationResult, evidence []byte) string {
	payload := struct {
		Instrument, Start, End, Calendar, Primary, Reference, ValidatedAt string
		Evidence                                                          json.RawMessage
	}{
		Instrument: result.Instrument.CanonicalCode(), Start: result.Start.UTC().Format("2006-01-02"),
		End: result.End.UTC().Format("2006-01-02"), Calendar: result.CalendarSource,
		Primary: result.PrimarySource, Reference: result.ReferenceSource,
		ValidatedAt: result.ValidatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), Evidence: evidence,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
