package marketdata

import (
	"testing"
	"time"
)

func qualifiedInputs(now time.Time) (ProviderDeclaration, AggregateReport, QualificationPolicy) {
	declaration := ProviderDeclaration{
		Name: "licensed-trial", MonthlyCostUSD: 19.99, IdentityRequirementsVerified: true,
		DisplayRightsVerified: true, TermsURL: "https://provider.example/terms",
		PricingVerifiedAt: now, TermsVerifiedAt: now, MinimumPollingInterval: 5 * time.Second,
		Coverage: Coverage{true, true, true, true, true, true, true},
	}
	report := AggregateReport{
		Samples: 1000, PassedSamples: 995, SuccessRate: 0.995,
		RequestDurationP95: time.Second, MarketAgeP95: 3 * time.Second,
		MissingMainNetInflowRate: 0.005, TradeDateCoverageRate: 1, CloseMismatchRate: 0,
		IssueCounts: map[IssueCode]int{},
	}
	return declaration, report, DefaultQualificationPolicy()
}

func TestQualifyAcceptsOnlyCompleteVerifiedProvider(t *testing.T) {
	now := time.Now().UTC()
	declaration, report, policy := qualifiedInputs(now)
	result := Qualify(declaration, report, policy, now)
	if !result.Qualified || len(result.Blockers) != 0 {
		t.Fatalf("expected qualification, got %+v", result)
	}
}

func TestQualifyFailsClosedForBudgetRightsCoverageAndEvidence(t *testing.T) {
	now := time.Now().UTC()
	declaration, report, policy := qualifiedInputs(now)
	declaration.MonthlyCostUSD = 20.01
	declaration.DisplayRightsVerified = false
	declaration.Coverage.BeijingStocks = false
	declaration.PricingVerifiedAt = now.Add(-31 * 24 * time.Hour)
	report.Samples = 99
	report.SuccessRate = 0.98
	report.IssueCounts[IssueProviderMismatch] = 1
	result := Qualify(declaration, report, policy, now)
	if result.Qualified {
		t.Fatal("provider with unresolved blockers was qualified")
	}
	want := map[string]bool{
		"monthly_cost": false, "display_rights": false, "coverage": false, "stale_pricing": false,
		"sample_count": false, "success_rate": false, "integrity_provider_mismatch": false,
	}
	for _, blocker := range result.Blockers {
		if _, exists := want[blocker.Code]; exists {
			want[blocker.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("missing blocker %s: %+v", code, result.Blockers)
		}
	}
}

func TestDefaultPolicyKeepsMainInflowAsExplicitGate(t *testing.T) {
	now := time.Now().UTC()
	declaration, report, policy := qualifiedInputs(now)
	policy.RequireMainNetInflow = true
	policy.MaximumMainInflowMissingRate = 0.01
	report.MissingMainNetInflowRate = 1
	result := Qualify(declaration, report, policy, now)
	if result.Qualified || len(result.Blockers) != 1 || result.Blockers[0].Code != "main_net_inflow" {
		t.Fatalf("main-inflow absence was not explicit: %+v", result)
	}
	policy.RequireMainNetInflow = false
	if result = Qualify(declaration, report, policy, now); !result.Qualified {
		t.Fatalf("optional main-inflow policy still blocked provider: %+v", result)
	}
}

func TestDefaultPolicyPrioritizesEndOfDayAccuracy(t *testing.T) {
	policy := DefaultQualificationPolicy()
	if policy.Purpose != "end_of_day_analysis" || policy.RequireMainNetInflow ||
		policy.MinimumTradeDateCoverageRate != 1 || policy.MaximumCloseMismatchRate != 0 {
		t.Fatalf("unexpected end-of-day defaults: %+v", policy)
	}
	intraday := DefaultIntradayWatchlistPolicy()
	if intraday.MaximumPollingInterval != 5*time.Minute || intraday.MaximumMarketAgeP95 != 15*time.Minute {
		t.Fatalf("unexpected lower-frequency watchlist profile: %+v", intraday)
	}
}

func TestQualifyBlocksIncompleteOrMismatchedCloseData(t *testing.T) {
	now := time.Now().UTC()
	declaration, report, policy := qualifiedInputs(now)
	report.TradeDateCoverageRate = 0.99
	report.CloseMismatchRate = 0.001
	result := Qualify(declaration, report, policy, now)
	want := map[string]bool{"trade_date_coverage": false, "close_accuracy": false}
	for _, blocker := range result.Blockers {
		if _, exists := want[blocker.Code]; exists {
			want[blocker.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("missing %s blocker: %+v", code, result.Blockers)
		}
	}
}

func TestQualifyRejectsInvalidPolicyAndFutureEvidence(t *testing.T) {
	now := time.Now().UTC()
	declaration, report, policy := qualifiedInputs(now)
	declaration.TermsVerifiedAt = now.Add(time.Hour)
	policy.MinimumSuccessRate = 2
	result := Qualify(declaration, report, policy, now)
	want := map[string]bool{"invalid_policy": false, "stale_terms": false}
	for _, blocker := range result.Blockers {
		if _, exists := want[blocker.Code]; exists {
			want[blocker.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("missing %s blocker: %+v", code, result.Blockers)
		}
	}
}
