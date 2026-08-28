package recommendation

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fixedScreeningProvider struct{ records []ScreeningRecord }

func (f fixedScreeningProvider) ScreeningRecords(context.Context, time.Time) ([]ScreeningRecord, error) {
	return f.records, nil
}

func TestQuantitativeCandidateSourceRanksAndAuditsEligibleStocks(t *testing.T) {
	provider := fixedScreeningProvider{records: []ScreeningRecord{
		{StockCode: "600001", StockName: "低质量", ValidationBatchID: 1, Trend: 100, Liquidity: 100, RiskSafety: 100, DataQuality: 70},
		{StockCode: "600002", StockName: "退市整理", ValidationBatchID: 2, Trend: 100, Liquidity: 100, RiskSafety: 100, DataQuality: 100, IsDelistingPeriod: true},
		{StockCode: "600003", StockName: "稳健样本", ValidationBatchID: 3, Trend: 70, Liquidity: 80, RiskSafety: 90, DataQuality: 100},
		{StockCode: "600004", StockName: "ST样本", ValidationBatchID: 4, Trend: 90, Liquidity: 90, RiskSafety: 70, DataQuality: 95, IsST: true},
		{StockCode: "600005", StockName: "低流动性", ValidationBatchID: 5, Trend: 100, Liquidity: 10, RiskSafety: 100, DataQuality: 100},
	}}
	source, err := NewQuantitativeCandidateSource(provider, ScreeningPolicy{Target: 40, MinimumLiquidity: 20, MinimumDataQuality: 90})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := source.Candidates(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].StockCode != "600004" || candidates[1].StockCode != "600003" {
		t.Fatalf("unexpected ranked candidates: %+v", candidates)
	}
	var evidence map[string]any
	if err := json.Unmarshal([]byte(candidates[0].ScreeningJSON), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence["version"] != PreScreeningVersion || evidence["isST"] != true || candidates[0].ScreeningScore != 85.75 {
		t.Fatalf("screening evidence is not auditable: score=%v evidence=%+v", candidates[0].ScreeningScore, evidence)
	}
}

func TestQuantitativeCandidateSourceDoesNotInventTargetCount(t *testing.T) {
	source, _ := NewQuantitativeCandidateSource(fixedScreeningProvider{records: []ScreeningRecord{
		{StockCode: "000001", StockName: "唯一合格", ValidationBatchID: 1, Trend: 50, Liquidity: 50, RiskSafety: 50, DataQuality: 100},
	}}, ScreeningPolicy{Target: 40, MinimumLiquidity: 20, MinimumDataQuality: 90})
	candidates, err := source.Candidates(context.Background(), time.Now())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("selector padded or rejected a short valid pool: %+v %v", candidates, err)
	}
}

func TestQuantitativeCandidateSourceRejectsDuplicateEvidence(t *testing.T) {
	record := ScreeningRecord{StockCode: "000001", StockName: "重复", ValidationBatchID: 1, Trend: 50, Liquidity: 50, RiskSafety: 50, DataQuality: 100}
	source, _ := NewQuantitativeCandidateSource(fixedScreeningProvider{records: []ScreeningRecord{record, record}},
		ScreeningPolicy{Target: 40, MinimumLiquidity: 20, MinimumDataQuality: 90})
	if _, err := source.Candidates(context.Background(), time.Now()); err == nil {
		t.Fatal("expected duplicate source evidence rejection")
	}
}
