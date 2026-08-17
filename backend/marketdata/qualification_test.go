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
		MissingMainNetInflowRate: 0.005, IssueCounts: map[IssueCode]int{},
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
	report.Samples = 999
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
