package recommendation

import (
	"math"
	"testing"
)

func TestCalculateIndex(t *testing.T) {
	result, err := CalculateIndex(ScoreComponents{
		RiseProbability: 80, ReturnOpportunity: 70, VolatilitySafety: 60,
		Liquidity: 90, AgentConsensus: 75, DataQuality: 95,
	}, []Penalty{{Code: "ST", Points: 8}})
	if err != nil {
		t.Fatal(err)
	}
	if result.BaseScore != 76 || result.PenaltyPoints != 8 || result.Index != 68 {
		t.Fatalf("unexpected score: %+v", result)
	}
	if result.StrategyVersion != StrategyVersion {
		t.Fatalf("unexpected strategy version: %s", result.StrategyVersion)
	}
}

func TestCalculateIndexClampsAndRejectsInvalidInput(t *testing.T) {
	high, err := CalculateIndex(ScoreComponents{100, 100, 100, 100, 100, 100}, nil)
	if err != nil || high.Index != 100 {
		t.Fatalf("expected index 100, got %+v, %v", high, err)
	}
	low, err := CalculateIndex(ScoreComponents{}, []Penalty{{Code: "risk", Points: 200}})
	if err != nil || low.Index != 0 {
		t.Fatalf("expected index 0, got %+v, %v", low, err)
	}
	if _, err = CalculateIndex(ScoreComponents{RiseProbability: math.NaN()}, nil); err == nil {
		t.Fatal("expected NaN to be rejected")
	}
	if _, err = CalculateIndex(ScoreComponents{}, []Penalty{{Code: "risk", Points: 1}, {Code: "risk", Points: 2}}); err == nil {
		t.Fatal("expected duplicate penalty to be rejected")
	}
}

func TestCalculateReturnOpportunityRewardsMidpointAndPenalizesWidth(t *testing.T) {
	narrow, err := CalculateReturnOpportunity(2, 4)
	if err != nil || narrow != 61 {
		t.Fatalf("unexpected narrow opportunity: %v %v", narrow, err)
	}
	wide, err := CalculateReturnOpportunity(-3, 8)
	if err != nil || wide != 40.5 || wide >= narrow {
		t.Fatalf("unexpected wide opportunity: %v %v", wide, err)
	}
	if _, err := CalculateReturnOpportunity(2, -2); err == nil {
		t.Fatal("expected inverted range rejection")
	}
}

func TestLocalRiskPenaltiesAreDeterministicAndRejectConflicts(t *testing.T) {
	penalties, err := LocalRiskPenalties([]string{"NEW_STOCK", "ST", "DISPLAY_ONLY"})
	if err != nil || len(penalties) != 2 || penalties[0] != (Penalty{Code: "NEW_STOCK", Points: 5}) || penalties[1] != (Penalty{Code: "ST", Points: 8}) {
		t.Fatalf("unexpected local penalties: %+v %v", penalties, err)
	}
	if _, err := LocalRiskPenalties([]string{"ST", "*ST"}); err == nil {
		t.Fatal("expected mutually exclusive ST labels rejection")
	}
}

func TestSelectDailyRecommendationsEnforcesRiskCaps(t *testing.T) {
	candidates := []Candidate{
		{ID: 1, Index: 99, IsST: true, IsNewStock: true},
		{ID: 2, Index: 98, IsST: true},
		{ID: 3, Index: 97, IsNewStock: true},
		{ID: 4, Index: 96, IsNewStock: true},
		{ID: 5, Index: 95, IsDelistingPeriod: true},
	}
	for id := uint(6); id <= 20; id++ {
		candidates = append(candidates, Candidate{ID: id, Index: 95 - int(id)})
	}

	selected, err := SelectDailyRecommendations(candidates, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 10 {
		t.Fatalf("expected 10 results, got %d", len(selected))
	}
	stCount, newCount := 0, 0
	for _, candidate := range selected {
		if candidate.IsDelistingPeriod {
			t.Fatal("delisting-period candidate was selected")
		}
		if candidate.IsST {
			stCount++
		}
		if candidate.IsNewStock {
			newCount++
		}
	}
	if stCount != 1 || newCount != 2 {
		t.Fatalf("unexpected quotas: ST=%d new=%d", stCount, newCount)
	}
}

func TestSelectDailyRecommendationsValidatesTarget(t *testing.T) {
	if _, err := SelectDailyRecommendations(nil, 9); err == nil {
		t.Fatal("expected target validation error")
	}
}
