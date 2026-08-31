package recommendation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go-stock/backend/marketdata"
)

const FeatureFormulaVersion = "daily-bar-features-v0.1"

type ScreeningIdentity struct {
	StockCode         string
	StockName         string
	ValidationBatchID uint
	IsST              bool
	IsNewStock        bool
	IsDelistingPeriod bool
}

type ScreeningRawFeatures struct {
	Version                string  `json:"version"`
	Observations           int     `json:"observations"`
	ExpectedObservations   int     `json:"expectedObservations"`
	Return5DayPercent      float64 `json:"return5DayPercent"`
	Return20DayPercent     float64 `json:"return20DayPercent"`
	AnnualizedVolatility   float64 `json:"annualizedVolatilityPercent"`
	MaximumDrawdownPercent float64 `json:"maximumDrawdownPercent"`
	MedianTurnoverYuan     float64 `json:"medianTurnoverYuan"`
	FreshnessHours         float64 `json:"freshnessHours"`
}

// BuildScreeningRecord converts verified, unadjusted daily bars into normalized
// pre-screen factors. The formula is deliberately versioned and is not a forecast.
func BuildScreeningRecord(identity ScreeningIdentity, bars []marketdata.DailyBar, expectedObservations int, validatedAt, asOf time.Time) (ScreeningRecord, error) {
	if identity.StockCode == "" || identity.StockName == "" || identity.ValidationBatchID == 0 {
		return ScreeningRecord{}, errors.New("screening identity and validation batch are required")
	}
	if len(bars) < 21 || expectedObservations < 21 || validatedAt.IsZero() || asOf.IsZero() || validatedAt.After(asOf) {
		return ScreeningRecord{}, errors.New("at least 21 expected bars and valid evidence times are required")
	}
	ordered := append([]marketdata.DailyBar(nil), bars...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].TradeDate.Before(ordered[j].TradeDate) })
	for index, bar := range ordered {
		if bar.Instrument.Code == "" || bar.TradeDate.IsZero() || bar.Adjustment != marketdata.AdjustmentNone ||
			!positiveFinite(bar.Close) || !finiteNonNegativeFeature(bar.Turnover) {
			return ScreeningRecord{}, fmt.Errorf("daily bar %d is invalid for screening", index)
		}
		if index > 0 && !ordered[index-1].TradeDate.Before(bar.TradeDate) {
			return ScreeningRecord{}, errors.New("screening daily bars contain duplicate dates")
		}
		if bar.Instrument != ordered[0].Instrument {
			return ScreeningRecord{}, errors.New("screening daily bars contain multiple instruments")
		}
	}
	last := len(ordered) - 1
	if strings.SplitN(identity.StockCode, ".", 2)[0] != ordered[0].Instrument.Code {
		return ScreeningRecord{}, errors.New("screening identity does not match daily bars")
	}
	if ordered[last].TradeDate.After(validatedAt) || ordered[last].TradeDate.After(asOf) {
		return ScreeningRecord{}, errors.New("screening bars extend beyond the evidence cutoff")
	}
	return5 := percentChange(ordered[last-5].Close, ordered[last].Close)
	return20 := percentChange(ordered[last-20].Close, ordered[last].Close)
	returns := make([]float64, 0, 20)
	for index := last - 19; index <= last; index++ {
		returns = append(returns, ordered[index].Close/ordered[index-1].Close-1)
	}
	volatility := sampleStdDev(returns) * math.Sqrt(252) * 100
	maxDrawdown := maximumDrawdown(ordered[last-20:])
	turnovers := make([]float64, 0, 20)
	for _, bar := range ordered[last-19:] {
		turnovers = append(turnovers, bar.Turnover)
	}
	medianTurnover := median(turnovers)
	freshnessHours := asOf.Sub(validatedAt).Hours()
	trend := clamp(50+2*(0.4*return5+0.6*return20), 0, 100)
	volatilitySafety := 100 - clamp(volatility*1.5, 0, 100)
	drawdownSafety := 100 - clamp(math.Abs(maxDrawdown)*2, 0, 100)
	riskSafety := 0.6*volatilitySafety + 0.4*drawdownSafety
	liquidity := 0.0
	if medianTurnover > 0 {
		liquidity = clamp((math.Log10(medianTurnover)-6)*25, 0, 100)
	}
	coverage := clamp(float64(len(ordered))/float64(expectedObservations)*100, 0, 100)
	freshness := 100 - clamp(freshnessHours/48*100, 0, 100)
	quality := 0.8*coverage + 0.2*freshness
	raw := &ScreeningRawFeatures{FeatureFormulaVersion, len(ordered), expectedObservations, round(return5, 4), round(return20, 4),
		round(volatility, 4), round(maxDrawdown, 4), round(medianTurnover, 2), round(freshnessHours, 4)}
	return ScreeningRecord{StockCode: identity.StockCode, StockName: identity.StockName, ValidationBatchID: identity.ValidationBatchID,
		Trend: round(trend, 4), Liquidity: round(liquidity, 4), RiskSafety: round(riskSafety, 4), DataQuality: round(quality, 4),
		IsST: identity.IsST, IsNewStock: identity.IsNewStock, IsDelistingPeriod: identity.IsDelistingPeriod, RawFeatures: raw}, nil
}

func percentChange(start, end float64) float64 { return (end/start - 1) * 100 }

func sampleStdDev(values []float64) float64 {
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	sum := 0.0
	for _, value := range values {
		difference := value - mean
		sum += difference * difference
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

func maximumDrawdown(bars []marketdata.DailyBar) float64 {
	peak, worst := bars[0].Close, 0.0
	for _, bar := range bars[1:] {
		peak = math.Max(peak, bar.Close)
		worst = math.Min(worst, (bar.Close/peak-1)*100)
	}
	return worst
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}
	return sorted[middle]
}

func clamp(value, low, high float64) float64 { return math.Min(high, math.Max(low, value)) }
func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func finiteNonNegativeFeature(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
