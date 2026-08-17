package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const PreScreeningVersion = "post-close-screening-v0.1"

type ScreeningRecord struct {
	StockCode         string
	StockName         string
	ValidationBatchID uint
	Trend             float64
	Liquidity         float64
	RiskSafety        float64
	DataQuality       float64
	IsST              bool
	IsNewStock        bool
	IsDelistingPeriod bool
}

type ScreeningProvider interface {
	ScreeningRecords(context.Context, time.Time) ([]ScreeningRecord, error)
}

type ScreeningPolicy struct {
	Target             int
	MinimumLiquidity   float64
	MinimumDataQuality float64
}

type QuantitativeCandidateSource struct {
	provider ScreeningProvider
	policy   ScreeningPolicy
}

func NewQuantitativeCandidateSource(provider ScreeningProvider, policy ScreeningPolicy) (*QuantitativeCandidateSource, error) {
	if provider == nil || policy.Target <= 0 || policy.Target > 100 || !validFactor(policy.MinimumLiquidity) || !validFactor(policy.MinimumDataQuality) {
		return nil, errors.New("candidate source requires provider, target 1-100, and valid quality thresholds")
	}
	return &QuantitativeCandidateSource{provider: provider, policy: policy}, nil
}

type screeningEvidence struct {
	Version     string  `json:"version"`
	Trend       float64 `json:"trend"`
	Liquidity   float64 `json:"liquidity"`
	RiskSafety  float64 `json:"riskSafety"`
	DataQuality float64 `json:"dataQuality"`
	IsST        bool    `json:"isST"`
	IsNewStock  bool    `json:"isNewStock"`
}

// Candidates performs a transparent, non-AI pre-screen. Its score ranks which
// stocks receive expensive analysis; it is not an upward probability or advice.
func (s *QuantitativeCandidateSource) Candidates(ctx context.Context, tradeDate time.Time) ([]AnalysisCandidate, error) {
	if tradeDate.IsZero() {
		return nil, errors.New("candidate trade date is required")
	}
	records, err := s.provider.ScreeningRecords(ctx, tradeDate)
	if err != nil {
		return nil, fmt.Errorf("load quantitative screening records: %w", err)
	}
	type ranked struct {
		candidate AnalysisCandidate
		score     float64
	}
	eligible := make([]ranked, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		code := strings.TrimSpace(record.StockCode)
		if code == "" || strings.TrimSpace(record.StockName) == "" || record.ValidationBatchID == 0 || record.IsDelistingPeriod {
			continue
		}
		if seen[code] {
			return nil, fmt.Errorf("duplicate screening record %s", code)
		}
		seen[code] = true
		factors := []float64{record.Trend, record.Liquidity, record.RiskSafety, record.DataQuality}
		valid := true
		for _, factor := range factors {
			valid = valid && validFactor(factor)
		}
		if !valid || record.Liquidity < s.policy.MinimumLiquidity || record.DataQuality < s.policy.MinimumDataQuality {
			continue
		}
		score := round(0.35*record.Trend+0.25*record.Liquidity+0.25*record.RiskSafety+0.15*record.DataQuality, 4)
		evidence, _ := json.Marshal(screeningEvidence{PreScreeningVersion, record.Trend, record.Liquidity,
			record.RiskSafety, record.DataQuality, record.IsST, record.IsNewStock})
		eligible = append(eligible, ranked{candidate: AnalysisCandidate{StockCode: code, StockName: strings.TrimSpace(record.StockName),
			ValidationBatchID: record.ValidationBatchID, ScreeningScore: score, ScreeningJSON: string(evidence)}, score: score})
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].score == eligible[j].score {
			return eligible[i].candidate.StockCode < eligible[j].candidate.StockCode
		}
		return eligible[i].score > eligible[j].score
	})
	limit := min(s.policy.Target, len(eligible))
	result := make([]AnalysisCandidate, limit)
	for index := 0; index < limit; index++ {
		result[index] = eligible[index].candidate
	}
	return result, nil
}

func validFactor(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}
