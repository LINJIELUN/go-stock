package marketdata

import (
	"testing"
	"time"
)

func completeHistoricalSubmission(now time.Time) HistoricalProviderSubmission {
	evidence := make([]CapabilityEvidence, 0, len(RequiredHistoricalCapabilities()))
	for _, capability := range RequiredHistoricalCapabilities() {
		evidence = append(evidence, CapabilityEvidence{Capability: capability, Verified: true, Evidence: "contract-or-test-evidence", VerifiedAt: now.Add(-24 * time.Hour)})
	}
	return HistoricalProviderSubmission{ProviderName: "candidate", MonthlyCostUSD: 20, TermsPermitUse: true,
		TermsEvidence: "terms-review", Capabilities: evidence}
}

func TestHistoricalProviderQualificationRequiresCompleteCurrentEvidence(t *testing.T) {
	now := time.Now().UTC()
	result, err := AssessHistoricalProvider(completeHistoricalSubmission(now), HistoricalQualificationPolicy{
		MaximumMonthlyCostUSD: 20, MaximumEvidenceAge: 30 * 24 * time.Hour, AsOf: now,
	})
	if err != nil || !result.Qualified || len(result.Issues) != 0 {
		t.Fatalf("complete provider was rejected: %+v %v", result, err)
	}
}

func TestHistoricalProviderQualificationRejectsBudgetTermsAndMissingPointInTimeData(t *testing.T) {
	now := time.Now().UTC()
	submission := completeHistoricalSubmission(now)
	submission.MonthlyCostUSD = 20.01
	submission.TermsPermitUse = false
	submission.Capabilities = submission.Capabilities[1:]
	result, err := AssessHistoricalProvider(submission, HistoricalQualificationPolicy{
		MaximumMonthlyCostUSD: 20, MaximumEvidenceAge: 30 * 24 * time.Hour, AsOf: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Qualified || len(result.Issues) != 3 || result.Evidence[CapabilityPointInTimeUniverse] {
		t.Fatalf("unsafe provider was accepted or issues were lost: %+v", result)
	}
}

func TestHistoricalProviderQualificationRejectsStaleOrFutureEvidence(t *testing.T) {
	now := time.Now().UTC()
	submission := completeHistoricalSubmission(now)
	submission.Capabilities[0].VerifiedAt = now.Add(-31 * 24 * time.Hour)
	submission.Capabilities[1].VerifiedAt = now.Add(time.Hour)
	result, err := AssessHistoricalProvider(submission, HistoricalQualificationPolicy{
		MaximumMonthlyCostUSD: 20, MaximumEvidenceAge: 30 * 24 * time.Hour, AsOf: now,
	})
	if err != nil || result.Qualified || len(result.Issues) != 2 {
		t.Fatalf("invalid evidence dates were accepted: %+v %v", result, err)
	}
}

func TestRequiredHistoricalCapabilitiesReturnsCopy(t *testing.T) {
	first := RequiredHistoricalCapabilities()
	first[0] = "mutated"
	if RequiredHistoricalCapabilities()[0] == "mutated" {
		t.Fatal("caller mutated required capability registry")
	}
}
