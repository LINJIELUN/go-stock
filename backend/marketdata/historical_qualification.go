package marketdata

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

type HistoricalCapability string

const (
	CapabilityPointInTimeUniverse HistoricalCapability = "point_in_time_universe"
	CapabilityDailyBars           HistoricalCapability = "daily_bars"
	CapabilitySecurityStatus      HistoricalCapability = "security_status_history"
	CapabilityTradingCalendar     HistoricalCapability = "trading_calendar"
	CapabilitySuspensions         HistoricalCapability = "suspension_history"
	CapabilityCorporateActions    HistoricalCapability = "corporate_actions"
	CapabilityPublicationTime     HistoricalCapability = "publication_timestamps"
)

var requiredHistoricalCapabilities = []HistoricalCapability{
	CapabilityPointInTimeUniverse,
	CapabilityDailyBars,
	CapabilitySecurityStatus,
	CapabilityTradingCalendar,
	CapabilitySuspensions,
	CapabilityCorporateActions,
	CapabilityPublicationTime,
}

type CapabilityEvidence struct {
	Capability HistoricalCapability `json:"capability"`
	Verified   bool                 `json:"verified"`
	Evidence   string               `json:"evidence"`
	VerifiedAt time.Time            `json:"verifiedAt"`
}

type HistoricalProviderSubmission struct {
	ProviderName   string               `json:"providerName"`
	MonthlyCostUSD float64              `json:"monthlyCostUsd"`
	TermsPermitUse bool                 `json:"termsPermitUse"`
	TermsEvidence  string               `json:"termsEvidence"`
	Capabilities   []CapabilityEvidence `json:"capabilities"`
}

type HistoricalQualificationPolicy struct {
	MaximumMonthlyCostUSD float64
	MaximumEvidenceAge    time.Duration
	AsOf                  time.Time
}

type HistoricalQualificationIssue struct {
	Code       string               `json:"code"`
	Capability HistoricalCapability `json:"capability,omitempty"`
	Message    string               `json:"message"`
}

type HistoricalQualificationResult struct {
	ProviderName string                         `json:"providerName"`
	Qualified    bool                           `json:"qualified"`
	CheckedAt    time.Time                      `json:"checkedAt"`
	Issues       []HistoricalQualificationIssue `json:"issues"`
	Evidence     map[HistoricalCapability]bool  `json:"evidence"`
}

// AssessHistoricalProvider is deliberately stricter than checking whether an
// API returns K-lines. Every point-in-time capability and usage right must have
// current evidence before a provider can support credible walk-forward replay.
func AssessHistoricalProvider(submission HistoricalProviderSubmission, policy HistoricalQualificationPolicy) (HistoricalQualificationResult, error) {
	if submission.ProviderName == "" || policy.AsOf.IsZero() || policy.MaximumMonthlyCostUSD < 0 || policy.MaximumEvidenceAge <= 0 {
		return HistoricalQualificationResult{}, errors.New("provider, audit time, budget, and positive evidence age are required")
	}
	if math.IsNaN(submission.MonthlyCostUSD) || math.IsInf(submission.MonthlyCostUSD, 0) || submission.MonthlyCostUSD < 0 {
		return HistoricalQualificationResult{}, errors.New("provider monthly cost must be finite and non-negative")
	}
	result := HistoricalQualificationResult{ProviderName: submission.ProviderName, CheckedAt: policy.AsOf,
		Evidence: make(map[HistoricalCapability]bool)}
	if submission.MonthlyCostUSD > policy.MaximumMonthlyCostUSD {
		result.Issues = append(result.Issues, HistoricalQualificationIssue{Code: "over_budget", Message: fmt.Sprintf("monthly cost %.2f USD exceeds %.2f USD budget", submission.MonthlyCostUSD, policy.MaximumMonthlyCostUSD)})
	}
	if !submission.TermsPermitUse || submission.TermsEvidence == "" {
		result.Issues = append(result.Issues, HistoricalQualificationIssue{Code: "usage_rights_unverified", Message: "terms permitting intended storage and analysis are not verified"})
	}
	provided := make(map[HistoricalCapability]CapabilityEvidence, len(submission.Capabilities))
	for _, evidence := range submission.Capabilities {
		if evidence.Capability == "" {
			return HistoricalQualificationResult{}, errors.New("capability evidence name is required")
		}
		if _, duplicate := provided[evidence.Capability]; duplicate {
			return HistoricalQualificationResult{}, fmt.Errorf("duplicate evidence for capability %s", evidence.Capability)
		}
		provided[evidence.Capability] = evidence
	}
	for _, capability := range requiredHistoricalCapabilities {
		evidence, exists := provided[capability]
		valid := exists && evidence.Verified && evidence.Evidence != "" && !evidence.VerifiedAt.IsZero() &&
			!evidence.VerifiedAt.After(policy.AsOf) && policy.AsOf.Sub(evidence.VerifiedAt) <= policy.MaximumEvidenceAge
		result.Evidence[capability] = valid
		if !valid {
			result.Issues = append(result.Issues, HistoricalQualificationIssue{Code: "capability_unverified", Capability: capability,
				Message: "required point-in-time capability lacks current verification evidence"})
		}
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].Code == result.Issues[j].Code {
			return result.Issues[i].Capability < result.Issues[j].Capability
		}
		return result.Issues[i].Code < result.Issues[j].Code
	})
	result.Qualified = len(result.Issues) == 0
	return result, nil
}

func RequiredHistoricalCapabilities() []HistoricalCapability {
	return append([]HistoricalCapability(nil), requiredHistoricalCapabilities...)
}
