package recommendation

import "testing"

func TestCalculateReviewInsideRange(t *testing.T) {
	result, err := CalculateReview(10, 10.5, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActualReturnPercent != 5 || !result.DirectionHit || !result.RangeHit || result.OutsideRangeDeviation != 0 {
		t.Fatalf("unexpected review: %+v", result)
	}
}

func TestCalculateReviewOutsideRange(t *testing.T) {
	result, err := CalculateReview(10, 9, 2, 6)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActualReturnPercent != -10 || result.DirectionHit || result.RangeHit || result.OutsideRangeDeviation != 12 {
		t.Fatalf("unexpected review: %+v", result)
	}
}

func TestCalculateReviewValidatesInput(t *testing.T) {
	if _, err := CalculateReview(0, 10, 1, 2); err == nil {
		t.Fatal("expected invalid baseline error")
	}
	if _, err := CalculateReview(10, 11, 3, 2); err == nil {
		t.Fatal("expected invalid range error")
	}
}
