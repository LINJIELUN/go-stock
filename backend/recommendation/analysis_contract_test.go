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
	return AnalysisSnapshotContext{Job: models.AIAnalysisJob{Model: gormModel(9), StockCode: "600000", ValidationBatchID: 7}, StockName: "浦发银行",
		CompletedAt: now, DataAsOf: now.Add(-time.Hour), BaselinePrice: 10, BaselineMarketTime: now.Add(-time.Hour),
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
