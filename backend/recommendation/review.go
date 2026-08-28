package recommendation

import (
	"errors"
	"math"
)

type ReviewResult struct {
	ActualReturnPercent   float64 `json:"actualReturnPercent"`
	DirectionHit          bool    `json:"directionHit"`
	RangeHit              bool    `json:"rangeHit"`
	OutsideRangeDeviation float64 `json:"outsideRangeDeviation"`
}

// CalculateReview compares the recommendation-completion price with the eventual valid close.
// A caller must resolve the seventh trading day and suspension rollover before invoking it.
func CalculateReview(baselinePrice, actualClosePrice, rangeLow, rangeHigh float64) (ReviewResult, error) {
	values := []float64{baselinePrice, actualClosePrice, rangeLow, rangeHigh}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return ReviewResult{}, errors.New("review values must be finite")
		}
	}
	if baselinePrice <= 0 || actualClosePrice < 0 {
		return ReviewResult{}, errors.New("review prices must be valid non-negative market prices")
	}
	if rangeLow > rangeHigh {
		return ReviewResult{}, errors.New("return range lower bound cannot exceed upper bound")
	}

	actualReturn := (actualClosePrice - baselinePrice) / baselinePrice * 100
	deviation := 0.0
	if actualReturn < rangeLow {
		deviation = rangeLow - actualReturn
	} else if actualReturn > rangeHigh {
		deviation = actualReturn - rangeHigh
	}

	return ReviewResult{
		ActualReturnPercent:   round(actualReturn, 4),
		DirectionHit:          (rangeLow+rangeHigh)/2 >= 0 && actualReturn >= 0 || (rangeLow+rangeHigh)/2 < 0 && actualReturn < 0,
		RangeHit:              actualReturn >= rangeLow && actualReturn <= rangeHigh,
		OutsideRangeDeviation: round(deviation, 4),
	}, nil
}
