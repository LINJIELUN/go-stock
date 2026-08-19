package recommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

const InputBundleSchemaVersion = "analysis-input-bundle-v0.1"

type FrozenDailyBar struct {
	TradeDate time.Time `json:"tradeDate"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	Turnover  float64   `json:"turnover"`
	Source    string    `json:"source"`
}

type TimedAnalysisFact struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Value       string    `json:"value"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"publishedAt"`
}

type AnalysisInputDraft struct {
	StockCode         string
	ValidationBatchID uint
	DataAsOf          time.Time
	BaselinePrice     float64
	ScreeningJSON     string
	RiskLabels        []string
	DailyBars         []FrozenDailyBar
	FinancialFacts    []TimedAnalysisFact
	News              []TimedAnalysisFact
	Announcements     []TimedAnalysisFact
}

type frozenAnalysisInput struct {
	SchemaVersion     string              `json:"schemaVersion"`
	StockCode         string              `json:"stockCode"`
	ValidationBatchID uint                `json:"validationBatchId"`
	DataAsOf          time.Time           `json:"dataAsOf"`
	BaselinePrice     float64             `json:"baselinePrice"`
	Screening         json.RawMessage     `json:"screening"`
	RiskLabels        []string            `json:"riskLabels"`
	DailyBars         []FrozenDailyBar    `json:"dailyBars"`
	FinancialFacts    []TimedAnalysisFact `json:"financialFacts"`
	News              []TimedAnalysisFact `json:"news"`
	Announcements     []TimedAnalysisFact `json:"announcements"`
}

type InputBundleStore struct{ db *gorm.DB }

func NewInputBundleStore(db *gorm.DB) (*InputBundleStore, error) {
	if db == nil {
		return nil, errors.New("input bundle store requires a database")
	}
	return &InputBundleStore{db: db}, nil
}

// Save freezes canonical JSON and rejects a retry that changes any input for an
// existing job. No model-produced field participates in this package.
func (s *InputBundleStore) Save(job models.AIAnalysisJob, draft AnalysisInputDraft) (*models.AIAnalysisInputBundle, error) {
	payload, hash, err := freezeAnalysisInput(job, draft)
	if err != nil {
		return nil, err
	}
	var stored models.AIAnalysisInputBundle
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var current models.AIAnalysisJob
		if err := tx.First(&current, job.ID).Error; err != nil {
			return err
		}
		if current.StockCode != job.StockCode || current.ValidationBatchID != job.ValidationBatchID {
			return errors.New("analysis job identity changed before input freeze")
		}
		var validation models.MarketDataValidationBatch
		if err := tx.First(&validation, job.ValidationBatchID).Error; err != nil {
			return err
		}
		if validation.Status != "passed" || validation.ExpectedDates <= 0 || validation.ReleasedBars != validation.ExpectedDates {
			return errors.New("analysis input cannot use unapproved market data")
		}
		err := tx.Where("job_id = ?", job.ID).First(&stored).Error
		if err == nil {
			if stored.BundleHash != hash {
				return errors.New("analysis job already has a different frozen input bundle")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		stored = models.AIAnalysisInputBundle{JobID: job.ID, StockCode: job.StockCode, ValidationBatchID: job.ValidationBatchID,
			BundleHash: hash, SchemaVersion: InputBundleSchemaVersion, DataAsOf: draft.DataAsOf, PayloadJSON: string(payload)}
		return tx.Create(&stored).Error
	})
	if err != nil {
		return nil, fmt.Errorf("persist analysis input bundle: %w", err)
	}
	return &stored, nil
}

func freezeAnalysisInput(job models.AIAnalysisJob, draft AnalysisInputDraft) ([]byte, string, error) {
	if job.ID == 0 || job.StockCode == "" || job.ValidationBatchID == 0 || draft.StockCode != job.StockCode ||
		draft.ValidationBatchID != job.ValidationBatchID || draft.DataAsOf.IsZero() || !finite(draft.BaselinePrice) || draft.BaselinePrice <= 0 || len(draft.DailyBars) == 0 {
		return nil, "", errors.New("analysis input identity, cutoff, price, and daily bars are required")
	}
	screening, err := canonicalJSONObject(draft.ScreeningJSON)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize screening evidence: %w", err)
	}
	labels := append([]string(nil), draft.RiskLabels...)
	sort.Strings(labels)
	for index, label := range labels {
		if strings.TrimSpace(label) == "" || index > 0 && label == labels[index-1] {
			return nil, "", errors.New("input risk labels must be non-empty and unique")
		}
	}
	bars := append([]FrozenDailyBar(nil), draft.DailyBars...)
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate.Before(bars[j].TradeDate) })
	for index, bar := range bars {
		if bar.TradeDate.IsZero() || bar.TradeDate.After(draft.DataAsOf) || bar.Source == "" || !finite(bar.Close) || bar.Close <= 0 ||
			index > 0 && !bars[index-1].TradeDate.Before(bar.TradeDate) {
			return nil, "", errors.New("input daily bars are invalid, duplicated, or newer than cutoff")
		}
	}
	facts, err := canonicalFacts(draft.DataAsOf, append(append(append([]TimedAnalysisFact{}, draft.FinancialFacts...), draft.News...), draft.Announcements...))
	if err != nil {
		return nil, "", err
	}
	byID := make(map[string]TimedAnalysisFact, len(facts))
	for _, fact := range facts {
		byID[fact.ID] = fact
	}
	selectFacts := func(values []TimedAnalysisFact) []TimedAnalysisFact {
		result := make([]TimedAnalysisFact, 0, len(values))
		for _, value := range values {
			result = append(result, byID[value.ID])
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
		return result
	}
	frozen := frozenAnalysisInput{InputBundleSchemaVersion, draft.StockCode, draft.ValidationBatchID, draft.DataAsOf, draft.BaselinePrice,
		screening, labels, bars, selectFacts(draft.FinancialFacts), selectFacts(draft.News), selectFacts(draft.Announcements)}
	payload, err := json.Marshal(frozen)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:]), nil
}

// validateFrozenInputBundle verifies the persisted bytes rather than trusting
// duplicated database metadata. It is called immediately before an external
// engine receives the bundle.
func validateFrozenInputBundle(bundle models.AIAnalysisInputBundle, job models.AIAnalysisJob) (frozenAnalysisInput, error) {
	var frozen frozenAnalysisInput
	if bundle.ID == 0 || job.ID == 0 || bundle.JobID != job.ID || bundle.StockCode != job.StockCode ||
		bundle.ValidationBatchID != job.ValidationBatchID || bundle.SchemaVersion != InputBundleSchemaVersion || len(bundle.BundleHash) != sha256.Size*2 {
		return frozen, errors.New("frozen input bundle metadata does not match job")
	}
	payload := []byte(bundle.PayloadJSON)
	digest := sha256.Sum256(payload)
	if !strings.EqualFold(bundle.BundleHash, hex.EncodeToString(digest[:])) {
		return frozen, errors.New("frozen input bundle hash does not match payload")
	}
	decoder := json.NewDecoder(strings.NewReader(bundle.PayloadJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frozen); err != nil {
		return frozen, fmt.Errorf("decode frozen input bundle: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return frozen, err
	}
	if frozen.SchemaVersion != InputBundleSchemaVersion || frozen.StockCode != bundle.StockCode ||
		frozen.ValidationBatchID != bundle.ValidationBatchID || !frozen.DataAsOf.Equal(bundle.DataAsOf) || frozen.DataAsOf.IsZero() ||
		!finite(frozen.BaselinePrice) || frozen.BaselinePrice <= 0 || len(frozen.DailyBars) == 0 {
		return frozen, errors.New("frozen input bundle payload identity is invalid")
	}
	canonical, err := json.Marshal(frozen)
	if err != nil || !strings.EqualFold(bundle.BundleHash, hashPayload(canonical)) || !json.Valid(canonical) {
		return frozen, errors.New("frozen input bundle payload is not canonical")
	}
	return frozen, nil
}

func hashPayload(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func canonicalJSONObject(value string) (json.RawMessage, error) {
	var decoded map[string]any
	if value == "" || json.Unmarshal([]byte(value), &decoded) != nil || decoded == nil {
		return nil, errors.New("must be a JSON object")
	}
	encoded, err := json.Marshal(decoded)
	return encoded, err
}

func canonicalFacts(cutoff time.Time, values []TimedAnalysisFact) ([]TimedAnalysisFact, error) {
	result := append([]TimedAnalysisFact(nil), values...)
	seen := make(map[string]bool, len(result))
	for _, fact := range result {
		if fact.ID == "" || fact.Category == "" || fact.Value == "" || fact.Source == "" || fact.PublishedAt.IsZero() ||
			fact.PublishedAt.After(cutoff) || seen[fact.ID] {
			return nil, errors.New("input facts are incomplete, duplicated, or newer than cutoff")
		}
		seen[fact.ID] = true
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
