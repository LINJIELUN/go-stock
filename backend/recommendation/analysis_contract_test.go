package recommendation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
)

func validAnalysisContract(t *testing.T, dataAsOf time.Time) []byte {
	t.Helper()
	payload := StructuredAnalysisOutput{
		SchemaVersion: AnalysisSchemaVersion, ProbabilityNotice: ProbabilityNotice, RiseProbability: 68,
		ReturnRangeLow: -3, ReturnRangeHigh: 8,
		ScoreComponents: ScoreComponents{RiseProbability: 68, ReturnOpportunity: 70, VolatilitySafety: 60, Liquidity: 80, AgentConsensus: 75, DataQuality: 95},
		Penalties:       []Penalty{{Code: "NEW_STOCK", Points: 5}}, RiskLabels: []string{"NEW_STOCK"},
		Rationale: "多角色证据综合后偏多，但存在不确定性。", RiskNotes: "新股历史样本较少。",
		Evidence: []AnalysisEvidence{{ID: "market-1", Title: "已验证日线", Source: "validated-market-data", PublishedAt: dataAsOf.Add(-time.Hour)}},
		AgentConclusions: map[string]string{"technical": "趋势改善", "fundamental": "样本有限", "news": "无重大新增",
			"bull": "存在上行空间", "bear": "估值风险", "risk": "控制风险敞口"},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func validAnalysisContext(now time.Time) AnalysisSnapshotContext {
	dataAsOf := now.Add(-time.Hour)
	screening, _ := json.Marshal(screeningEvidence{Version: PreScreeningVersion, Trend: 80, Liquidity: 80, RiskSafety: 60, DataQuality: 95, IsNewStock: true})
	frozen := frozenAnalysisInput{SchemaVersion: InputBundleSchemaVersion, StockCode: "600000", ValidationBatchID: 7, DataAsOf: dataAsOf,
		BaselinePrice: 10, Screening: screening, RiskLabels: []string{"NEW_STOCK"},
		DailyBars: []FrozenDailyBar{{EvidenceID: dailyBarEvidenceID("600000", dataAsOf.Add(-24*time.Hour)), EvidenceTitle: dailyBarEvidenceTitle(dataAsOf.Add(-24 * time.Hour)), TradeDate: dataAsOf.Add(-24 * time.Hour), Open: 9.8, High: 10.2, Low: 9.7, Close: 10, Volume: 100, Turnover: 1000, Source: "validated"}},
		News:      []TimedAnalysisFact{{ID: "market-1", Category: "market", Value: "已验证日线", Source: "validated-market-data", PublishedAt: dataAsOf.Add(-time.Hour)}}}
	payload, _ := json.Marshal(frozen)
	return AnalysisSnapshotContext{Job: models.AIAnalysisJob{Model: gormModel(9), StockCode: "600000", ValidationBatchID: 7},
		InputBundle: models.AIAnalysisInputBundle{Model: gormModel(11), JobID: 9, StockCode: "600000", ValidationBatchID: 7,
			BundleHash: hashPayload(payload), SchemaVersion: InputBundleSchemaVersion, DataAsOf: dataAsOf, PayloadJSON: string(payload)}, StockName: "浦发银行",
		CompletedAt: now, DataAsOf: dataAsOf, BaselinePrice: 10, BaselineMarketTime: dataAsOf,
		ReviewDueDate: now.AddDate(0, 0, 10), ModelVersion: "provider/model-v1", PromptVersion: "prompt-v1"}
}

func gormModel(id uint) gorm.Model { return gorm.Model{ID: id} }

func TestCompileStructuredAnalysisUsesAuthoritativeContextAndCalculatesIndex(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	context := validAnalysisContext(now)
	snapshot, err := CompileStructuredAnalysis(validAnalysisContract(t, context.DataAsOf), context)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.StockCode != context.Job.StockCode || snapshot.ValidationBatchID != context.Job.ValidationBatchID ||
		snapshot.BaselinePrice != 10 || snapshot.AIRecommendationIndex != 66 || snapshot.ProbabilityNotice != ProbabilityNotice ||
		snapshot.ModelVersion != "provider/model-v1" || snapshot.PromptVersion != "prompt-v1" {
		t.Fatalf("unexpected compiled snapshot: %+v", snapshot)
	}
	if !strings.Contains(snapshot.EvidenceJSON, "validated-market-data") || !strings.Contains(snapshot.AgentConclusionsJSON, "technical") {
		t.Fatalf("analysis audit evidence was not preserved: %+v", snapshot)
	}
}

func TestCompileStructuredAnalysisRejectsUnknownOrTrailingJSON(t *testing.T) {
	now := time.Now().UTC()
	context := validAnalysisContext(now)
	raw := validAnalysisContract(t, context.DataAsOf)
	withUnknown := []byte(strings.Replace(string(raw), `"schemaVersion":`, `"unexpected":true,"schemaVersion":`, 1))
	if _, err := CompileStructuredAnalysis(withUnknown, context); err == nil {
		t.Fatal("expected unknown field rejection")
	}
	if _, err := CompileStructuredAnalysis(append(raw, []byte(` {}`)...), context); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func TestCompileStructuredAnalysisRejectsFutureEvidenceAndMissingRole(t *testing.T) {
	now := time.Now().UTC()
	context := validAnalysisContext(now)
	var output StructuredAnalysisOutput
	if err := json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output); err != nil {
		t.Fatal(err)
	}
	output.Evidence[0].PublishedAt = context.DataAsOf.Add(time.Second)
	raw, _ := json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil {
		t.Fatal("expected future evidence rejection")
	}
	output.Evidence[0].PublishedAt = context.DataAsOf
	delete(output.AgentConclusions, "bear")
	raw, _ = json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil {
		t.Fatal("expected missing agent role rejection")
	}
}

func TestCompileStructuredAnalysisRejectsSpoofedNoticeAndInvalidContext(t *testing.T) {
	now := time.Now().UTC()
	context := validAnalysisContext(now)
	var output StructuredAnalysisOutput
	json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output)
	output.ProbabilityNotice = "保证上涨"
	raw, _ := json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil {
		t.Fatal("expected probability notice rejection")
	}
	context.Job.ValidationBatchID = 0
	if _, err := CompileStructuredAnalysis(validAnalysisContract(t, context.DataAsOf), context); err == nil {
		t.Fatal("expected invalid context rejection")
	}
}

func TestCompileStructuredAnalysisRejectsInventedOrAlteredEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	context := validAnalysisContext(now)
	var output StructuredAnalysisOutput
	json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output)
	output.Evidence[0].ID = "invented"
	raw, _ := json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil || !strings.Contains(err.Error(), "exact frozen input fact") {
		t.Fatalf("expected invented evidence rejection: %v", err)
	}
	output.Evidence[0].ID = "market-1"
	output.Evidence[0].Source = "different-source"
	raw, _ = json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil {
		t.Fatal("expected altered evidence source rejection")
	}
}

func TestCompileStructuredAnalysisAcceptsDeterministicDailyBarEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	context := validAnalysisContext(now)
	var frozen frozenAnalysisInput
	json.Unmarshal([]byte(context.InputBundle.PayloadJSON), &frozen)
	bar := frozen.DailyBars[0]
	var output StructuredAnalysisOutput
	json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output)
	output.Evidence[0] = AnalysisEvidence{ID: bar.EvidenceID, Title: bar.EvidenceTitle, Source: bar.Source, PublishedAt: bar.TradeDate}
	raw, _ := json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err != nil {
		t.Fatalf("deterministic daily bar evidence rejected: %v", err)
	}
}

func TestCompileStructuredAnalysisRejectsModelControlledAuthoritativeScores(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	context := validAnalysisContext(now)
	var output StructuredAnalysisOutput
	json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output)
	output.ScoreComponents.Liquidity = 100
	raw, _ := json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil || !strings.Contains(err.Error(), "authoritative inputs") {
		t.Fatalf("expected model-controlled liquidity rejection: %v", err)
	}
	json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output)
	output.RiskLabels = []string{"ST"}
	raw, _ = json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil || !strings.Contains(err.Error(), "frozen inputs") {
		t.Fatalf("expected invented risk label rejection: %v", err)
	}
	json.Unmarshal(validAnalysisContract(t, context.DataAsOf), &output)
	output.Penalties = append(output.Penalties, Penalty{Code: "INVENTED", Points: 20})
	raw, _ = json.Marshal(output)
	if _, err := CompileStructuredAnalysis(raw, context); err == nil || !strings.Contains(err.Error(), "not backed") {
		t.Fatalf("expected unbacked penalty rejection: %v", err)
	}
}
