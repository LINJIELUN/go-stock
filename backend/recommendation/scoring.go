package recommendation

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const StrategyVersion = "ai-recommendation-index-v0.2"
const SingleCallConsensusScore = 50.0

var shadowRiskPenaltyPoints = map[string]float64{
	"ST":        8,
	"*ST":       12,
	"NEW_STOCK": 5,
}

type ScoreComponents struct {
	RiseProbability   float64 `json:"riseProbability"`
	ReturnOpportunity float64 `json:"returnOpportunity"`
	VolatilitySafety  float64 `json:"volatilitySafety"`
	Liquidity         float64 `json:"liquidity"`
	AgentConsensus    float64 `json:"agentConsensus"`
	DataQuality       float64 `json:"dataQuality"`
}

type Penalty struct {
	Code   string  `json:"code"`
	Points float64 `json:"points"`
}

type ScoreResult struct {
	StrategyVersion string  `json:"strategyVersion"`
	BaseScore       float64 `json:"baseScore"`
	PenaltyPoints   float64 `json:"penaltyPoints"`
	Index           int     `json:"index"`
}

func CalculateIndex(components ScoreComponents, penalties []Penalty) (ScoreResult, error) {
	values := map[string]float64{
		"rise probability":   components.RiseProbability,
		"return opportunity": components.ReturnOpportunity,
		"volatility safety":  components.VolatilitySafety,
		"liquidity":          components.Liquidity,
		"agent consensus":    components.AgentConsensus,
		"data quality":       components.DataQuality,
	}
	for name, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
			return ScoreResult{}, fmt.Errorf("%s score must be a finite value between 0 and 100", name)
		}
	}

	base := 0.30*components.RiseProbability +
		0.20*components.ReturnOpportunity +
		0.20*components.VolatilitySafety +
		0.10*components.Liquidity +
		0.10*components.AgentConsensus +
		0.10*components.DataQuality

	penaltyPoints := 0.0
	seen := make(map[string]struct{}, len(penalties))
	for _, penalty := range penalties {
		if penalty.Code == "" {
			return ScoreResult{}, errors.New("penalty code is required")
		}
		if _, exists := seen[penalty.Code]; exists {
			return ScoreResult{}, fmt.Errorf("duplicate penalty code %q", penalty.Code)
		}
		if math.IsNaN(penalty.Points) || math.IsInf(penalty.Points, 0) || penalty.Points < 0 {
			return ScoreResult{}, fmt.Errorf("penalty %q must have finite non-negative points", penalty.Code)
		}
		seen[penalty.Code] = struct{}{}
		penaltyPoints += penalty.Points
	}

	index := int(math.Round(base - penaltyPoints))
	index = min(100, max(0, index))

	return ScoreResult{
		StrategyVersion: StrategyVersion,
		BaseScore:       round(base, 4),
		PenaltyPoints:   round(penaltyPoints, 4),
		Index:           index,
	}, nil
}

// CalculateReturnOpportunity is a transparent shadow-run heuristic until
// reviewed samples support rolling-percentile calibration. A positive interval
// midpoint helps, while uncertainty expressed as interval width reduces score.
func CalculateReturnOpportunity(low, high float64) (float64, error) {
	if math.IsNaN(low) || math.IsInf(low, 0) || math.IsNaN(high) || math.IsInf(high, 0) || low > high {
		return 0, errors.New("predicted return interval is invalid")
	}
	midpoint, width := (low+high)/2, high-low
	return round(clamp(50+5*midpoint-2*width, 0, 100), 4), nil
}

// LocalRiskPenalties prevents the model from choosing its own index deduction.
// These shadow-run points are versioned with StrategyVersion and must be
// recalibrated from reviewed samples before production claims are made.
func LocalRiskPenalties(labels []string) ([]Penalty, error) {
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		if label == "" {
			return nil, errors.New("risk label is required")
		}
		if _, exists := seen[label]; exists {
			return nil, fmt.Errorf("duplicate risk label %q", label)
		}
		seen[label] = struct{}{}
	}
	if _, st := seen["ST"]; st {
		if _, starST := seen["*ST"]; starST {
			return nil, errors.New("ST and *ST labels are mutually exclusive")
		}
	}
	penalties := make([]Penalty, 0, len(labels))
	for label, points := range shadowRiskPenaltyPoints {
		if _, exists := seen[label]; exists {
			penalties = append(penalties, Penalty{Code: label, Points: points})
		}
	}
	sort.Slice(penalties, func(i, j int) bool { return penalties[i].Code < penalties[j].Code })
	return penalties, nil
}

type Candidate struct {
	ID                uint `json:"id"`
	Index             int  `json:"index"`
	IsST              bool `json:"isST"`
	IsNewStock        bool `json:"isNewStock"`
	IsDelistingPeriod bool `json:"isDelistingPeriod"`
}

// SelectDailyRecommendations returns the highest-ranked eligible candidates while enforcing
// the confirmed 10% ST/*ST and 20% newly-listed-stock caps. A candidate carrying both labels
// consumes both quotas. Delisting-period securities are never eligible.
func SelectDailyRecommendations(candidates []Candidate, target int) ([]Candidate, error) {
	if target < 10 || target > 20 {
		return nil, fmt.Errorf("target must be between 10 and 20")
	}

	ranked := append([]Candidate(nil), candidates...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Index == ranked[j].Index {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].Index > ranked[j].Index
	})

	stLimit := target / 10
	newStockLimit := target / 5
	selected := make([]Candidate, 0, target)
	seen := make(map[uint]struct{}, len(ranked))
	stCount, newStockCount := 0, 0

	for _, candidate := range ranked {
		if len(selected) == target {
			break
		}
		if candidate.ID == 0 || candidate.Index < 0 || candidate.Index > 100 || candidate.IsDelistingPeriod {
			continue
		}
		if _, exists := seen[candidate.ID]; exists {
			continue
		}
		if candidate.IsST && stCount >= stLimit {
			continue
		}
		if candidate.IsNewStock && newStockCount >= newStockLimit {
			continue
		}

		selected = append(selected, candidate)
		seen[candidate.ID] = struct{}{}
		if candidate.IsST {
			stCount++
		}
		if candidate.IsNewStock {
			newStockCount++
		}
	}

	return selected, nil
}

func round(value float64, precision int) float64 {
	factor := math.Pow10(precision)
	return math.Round(value*factor) / factor
}
