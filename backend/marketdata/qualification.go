package marketdata

import (
	"fmt"
	"sort"
	"time"
)

const MarketDataMonthlyBudgetUSD = 20.0

type Coverage struct {
	ShanghaiStocks bool
	ShenzhenStocks bool
	BeijingStocks  bool
	ETFs           bool
	STStocks       bool
	NewStocks      bool
	Suspensions    bool
}

type ProviderDeclaration struct {
	Name                         string
	MonthlyCostUSD               float64
	IdentityRequirementsVerified bool
	IdentityVerificationRequired bool
	DisplayRightsVerified        bool
	TermsURL                     string
	PricingVerifiedAt            time.Time
	TermsVerifiedAt              time.Time
	MinimumPollingInterval       time.Duration
	Coverage                     Coverage
}

type QualificationPolicy struct {
	MaximumMonthlyCostUSD        float64
	MaximumPollingInterval       time.Duration
	MinimumSamples               int
	MinimumSuccessRate           float64
	MaximumRequestDurationP95    time.Duration
	MaximumMarketAgeP95          time.Duration
	MaximumMainInflowMissingRate float64
	RequireMainNetInflow         bool
	MaximumVerificationAge       time.Duration
}

type QualificationBlocker struct {
	Code    string
	Message string
}

type QualificationResult struct {
	Provider  string
	Qualified bool
	Blockers  []QualificationBlocker
}

// Qualify applies product, legal, price and measured-data gates. It deliberately
// treats unknown declarations as blockers rather than assuming a provider is safe.
func Qualify(declaration ProviderDeclaration, report AggregateReport, policy QualificationPolicy, now time.Time) QualificationResult {
	result := QualificationResult{Provider: declaration.Name}
	add := func(code, message string) {
		result.Blockers = append(result.Blockers, QualificationBlocker{Code: code, Message: message})
	}
	if declaration.Name == "" {
		add("missing_provider", "provider name is required")
	}
	if declaration.MonthlyCostUSD < 0 || declaration.MonthlyCostUSD > policy.MaximumMonthlyCostUSD {
		add("monthly_cost", fmt.Sprintf("monthly cost %.2f exceeds or invalidates the %.2f USD ceiling", declaration.MonthlyCostUSD, policy.MaximumMonthlyCostUSD))
	}
	if !declaration.IdentityRequirementsVerified {
		add("identity_requirements", "provider identity requirements have not been verified")
	}
	if !declaration.DisplayRightsVerified || declaration.TermsURL == "" {
		add("display_rights", "desktop display rights and source terms are not verified")
	}
	if declaration.PricingVerifiedAt.IsZero() || now.Sub(declaration.PricingVerifiedAt) > policy.MaximumVerificationAge {
		add("stale_pricing", "pricing verification is missing or stale")
	}
	if declaration.TermsVerifiedAt.IsZero() || now.Sub(declaration.TermsVerifiedAt) > policy.MaximumVerificationAge {
		add("stale_terms", "terms verification is missing or stale")
	}
	if declaration.MinimumPollingInterval <= 0 || declaration.MinimumPollingInterval > policy.MaximumPollingInterval {
		add("polling_interval", "provider terms do not confirm the required polling interval")
	}
	coverage := declaration.Coverage
	if !coverage.ShanghaiStocks || !coverage.ShenzhenStocks || !coverage.BeijingStocks || !coverage.ETFs ||
		!coverage.STStocks || !coverage.NewStocks || !coverage.Suspensions {
		add("coverage", "沪深北、ETF、ST、新股和停牌状态 coverage is incomplete")
	}
	if report.Samples < policy.MinimumSamples {
		add("sample_count", fmt.Sprintf("only %d measured samples; at least %d are required", report.Samples, policy.MinimumSamples))
	}
	if report.SuccessRate < policy.MinimumSuccessRate {
		add("success_rate", fmt.Sprintf("measured success rate %.4f is below %.4f", report.SuccessRate, policy.MinimumSuccessRate))
	}
	if report.RequestDurationP95 > policy.MaximumRequestDurationP95 {
		add("request_latency", "measured request P95 exceeds the acceptance threshold")
	}
	if report.MarketAgeP95 > policy.MaximumMarketAgeP95 {
		add("market_age", "measured quote-age P95 exceeds the acceptance threshold")
	}
	if policy.RequireMainNetInflow && report.MissingMainNetInflowRate > policy.MaximumMainInflowMissingRate {
		add("main_net_inflow", "main-net-inflow missing rate exceeds the acceptance threshold")
	}
	for code, count := range report.IssueCounts {
		if count > 0 && (code == IssueProviderMismatch || code == IssueUnexpectedQuote || code == IssueDuplicateQuote) {
			add("integrity_"+string(code), fmt.Sprintf("observed %d %s integrity issues", count, code))
		}
	}
	sort.Slice(result.Blockers, func(i, j int) bool { return result.Blockers[i].Code < result.Blockers[j].Code })
	result.Qualified = len(result.Blockers) == 0
	return result
}

func DefaultQualificationPolicy() QualificationPolicy {
	return QualificationPolicy{
		MaximumMonthlyCostUSD: MarketDataMonthlyBudgetUSD, MaximumPollingInterval: 5 * time.Second,
		MinimumSamples: 1000, MinimumSuccessRate: 0.99, MaximumRequestDurationP95: 2 * time.Second,
		MaximumMarketAgeP95: 5 * time.Second, MaximumMainInflowMissingRate: 0.01,
		RequireMainNetInflow: true, MaximumVerificationAge: 30 * 24 * time.Hour,
	}
}
