package marketdata

import (
	"math"
	"testing"
	"time"
)

func TestNormalizeInstrumentCoversShanghaiShenzhenBeijingAndETF(t *testing.T) {
	tests := []struct {
		input string
		kind  SecurityType
		want  string
	}{
		{"600519.SH", SecurityStock, "600519.XSHG"},
		{"000001.SZ", SecurityStock, "000001.XSHE"},
		{"830799.BJ", SecurityStock, "830799.BSE"},
		{"510300", SecurityETF, "510300.XSHG"},
		{"159915", SecurityETF, "159915.XSHE"},
	}
	for _, test := range tests {
		instrument, err := NormalizeInstrument(test.input, test.kind)
		if err != nil {
			t.Fatalf("normalize %s: %v", test.input, err)
		}
		if instrument.CanonicalCode() != test.want {
			t.Fatalf("normalize %s: got %s, want %s", test.input, instrument.CanonicalCode(), test.want)
		}
	}
}

func TestNormalizeInstrumentFailsClosedForAmbiguousPrefix(t *testing.T) {
	if _, err := NormalizeInstrument("700001", SecurityStock); err == nil {
		t.Fatal("expected ambiguous exchange to require an explicit suffix")
	}
}

func TestValidateQuoteAcceptsMissingOptionalVendorFields(t *testing.T) {
	now := time.Now().UTC()
	instrument, _ := NormalizeInstrument("600519.SH", SecurityStock)
	quote := Quote{
		Instrument: instrument, LastPrice: 1500, PreviousClose: 1490, ChangePercent: 0.6711,
		Volume: 100, Turnover: 150000, Status: StatusTrading, MarketTime: now.Add(-time.Second),
		ReceivedAt: now, Source: "contract-test",
	}
	if err := ValidateQuote(quote, ValidationPolicy{Now: now, MaximumAge: 5 * time.Second, MaximumClockSkew: time.Second}); err != nil {
		t.Fatal(err)
	}
	if quote.MainNetInflow != nil {
		t.Fatal("missing main net inflow must remain unavailable, not be estimated")
	}
}

func TestValidateQuoteRejectsStaleFutureAndNonFiniteValues(t *testing.T) {
	now := time.Now().UTC()
	instrument, _ := NormalizeInstrument("000001.SZ", SecurityStock)
	base := Quote{Instrument: instrument, LastPrice: 10, PreviousClose: 10, Status: StatusTrading, MarketTime: now, ReceivedAt: now, Source: "test"}
	policy := ValidationPolicy{Now: now, MaximumAge: 5 * time.Second, MaximumClockSkew: time.Second}

	stale := base
	stale.MarketTime = now.Add(-6 * time.Second)
	if err := ValidateQuote(stale, policy); err == nil {
		t.Fatal("expected stale quote rejection")
	}
	future := base
	future.MarketTime = now.Add(2 * time.Second)
	if err := ValidateQuote(future, policy); err == nil {
		t.Fatal("expected future quote rejection")
	}
	nonFinite := base
	nonFinite.LastPrice = math.NaN()
	if err := ValidateQuote(nonFinite, policy); err == nil {
		t.Fatal("expected non-finite quote rejection")
	}
	negativeRatio := -1.0
	invalidDerived := base
	invalidDerived.VolumeRatio = &negativeRatio
	if err := ValidateQuote(invalidDerived, policy); err == nil {
		t.Fatal("expected negative volume ratio rejection")
	}
}
