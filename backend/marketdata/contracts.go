package marketdata

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Exchange string

const (
	ExchangeShanghai Exchange = "XSHG"
	ExchangeShenzhen Exchange = "XSHE"
	ExchangeBeijing  Exchange = "BSE"
)

type SecurityType string

const (
	SecurityStock SecurityType = "stock"
	SecurityETF   SecurityType = "etf"
)

type TradingStatus string

const (
	StatusTrading   TradingStatus = "trading"
	StatusSuspended TradingStatus = "suspended"
	StatusClosed    TradingStatus = "closed"
	StatusUnknown   TradingStatus = "unknown"
)

type Instrument struct {
	Code         string
	Exchange     Exchange
	SecurityType SecurityType
}

func (i Instrument) CanonicalCode() string {
	return i.Code + "." + string(i.Exchange)
}

// NormalizeInstrument normalizes provider/user symbols. Exchange inference is
// intentionally conservative; the securities master remains authoritative.
func NormalizeInstrument(value string, securityType SecurityType) (Instrument, error) {
	clean := strings.ToUpper(strings.TrimSpace(value))
	clean = strings.TrimPrefix(clean, "SH")
	clean = strings.TrimPrefix(clean, "SZ")
	clean = strings.TrimPrefix(clean, "BJ")
	parts := strings.Split(clean, ".")
	code := parts[0]
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return Instrument{}, fmt.Errorf("A-share code must contain six digits")
	}
	var exchange Exchange
	if len(parts) == 2 {
		switch parts[1] {
		case "SH", "SS", "XSHG":
			exchange = ExchangeShanghai
		case "SZ", "XSHE":
			exchange = ExchangeShenzhen
		case "BJ", "BSE":
			exchange = ExchangeBeijing
		default:
			return Instrument{}, fmt.Errorf("unsupported exchange suffix %q", parts[1])
		}
	} else if len(parts) > 2 {
		return Instrument{}, errors.New("invalid instrument code")
	} else {
		switch code[0] {
		case '5', '6', '9':
			exchange = ExchangeShanghai
		case '0', '1', '2', '3':
			exchange = ExchangeShenzhen
		case '4', '8':
			exchange = ExchangeBeijing
		default:
			return Instrument{}, errors.New("exchange cannot be inferred; use an explicit suffix from the securities master")
		}
	}
	if securityType != SecurityStock && securityType != SecurityETF {
		return Instrument{}, errors.New("security type must be stock or etf")
	}
	return Instrument{Code: code, Exchange: exchange, SecurityType: securityType}, nil
}

type Quote struct {
	Instrument      Instrument
	LastPrice       float64
	PreviousClose   float64
	ChangePercent   float64
	Volume          float64
	Turnover        float64
	TurnoverRate    *float64
	VolumeRatio     *float64
	MainNetInflow   *float64
	Status          TradingStatus
	MarketTime      time.Time
	ReceivedAt      time.Time
	Source          string
	SourceRequestID string
	AdvertisedDelay time.Duration
}

type QuoteProvider interface {
	Name() string
	Quotes(context.Context, []Instrument) ([]Quote, error)
}

type ValidationPolicy struct {
	Now              time.Time
	MaximumAge       time.Duration
	MaximumClockSkew time.Duration
}

// ValidateQuote rejects untraceable, stale, future-dated, or internally invalid
// values. Optional derived fields remain nil when the provider does not supply them.
func ValidateQuote(quote Quote, policy ValidationPolicy) error {
	if quote.Instrument.Code == "" || quote.Source == "" || quote.MarketTime.IsZero() || quote.ReceivedAt.IsZero() {
		return errors.New("quote requires instrument, source, market time, and receive time")
	}
	if policy.Now.IsZero() || policy.MaximumAge < 0 || policy.MaximumClockSkew < 0 {
		return errors.New("invalid quote validation policy")
	}
	if quote.MarketTime.After(policy.Now.Add(policy.MaximumClockSkew)) {
		return errors.New("quote market time is in the future")
	}
	if policy.Now.Sub(quote.MarketTime) > policy.MaximumAge {
		return errors.New("quote is stale")
	}
	if quote.ReceivedAt.Before(quote.MarketTime.Add(-policy.MaximumClockSkew)) {
		return errors.New("quote was received before its market timestamp")
	}
	if !finiteNonNegative(quote.LastPrice) || !finiteNonNegative(quote.PreviousClose) ||
		!finiteNonNegative(quote.Volume) || !finiteNonNegative(quote.Turnover) || !finite(quote.ChangePercent) {
		return errors.New("quote contains invalid numeric values")
	}
	if quote.Status == StatusTrading && (quote.LastPrice <= 0 || quote.PreviousClose <= 0) {
		return errors.New("trading quote requires positive last and previous-close prices")
	}
	if quote.Status != StatusTrading && quote.Status != StatusSuspended && quote.Status != StatusClosed && quote.Status != StatusUnknown {
		return errors.New("quote contains an unsupported trading status")
	}
	if quote.AdvertisedDelay < 0 {
		return errors.New("advertised delay cannot be negative")
	}
	for name, value := range map[string]*float64{"turnover rate": quote.TurnoverRate, "volume ratio": quote.VolumeRatio} {
		if value != nil && !finiteNonNegative(*value) {
			return fmt.Errorf("%s must be finite and non-negative when supplied", name)
		}
	}
	if quote.MainNetInflow != nil && !finite(*quote.MainNetInflow) {
		return errors.New("main net inflow must be finite when supplied")
	}
	return nil
}

func finite(value float64) bool            { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func finiteNonNegative(value float64) bool { return finite(value) && value >= 0 }
