package recommendation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"go-stock/backend/models"
)

const (
	AnalysisSchemaVersion = "trading-analysis-output-v0.2"
	ProbabilityNotice     = "模型估计、非实际结果"
)

var requiredAnalysisRoles = []string{"technical", "fundamental", "news", "bull", "bear", "risk"}

type AnalysisEvidence struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"publishedAt"`
}

type StructuredAnalysisOutput struct {
	SchemaVersion     string             `json:"schemaVersion"`
	ProbabilityNotice string             `json:"probabilityNotice"`
	RiseProbability   float64            `json:"riseProbability"`
	ReturnRangeLow    float64            `json:"returnRangeLow"`
	ReturnRangeHigh   float64            `json:"returnRangeHigh"`
	RiskLabels        []string           `json:"riskLabels"`
	Rationale         string             `json:"rationale"`
	RiskNotes         string             `json:"riskNotes"`
	Evidence          []AnalysisEvidence `json:"evidence"`
	AgentConclusions  map[string]string  `json:"agentConclusions"`
}

type AnalysisSnapshotContext struct {
	Job                models.AIAnalysisJob
	InputBundle        models.AIAnalysisInputBundle
	StockName          string
	CompletedAt        time.Time
	DataAsOf           time.Time
	BaselinePrice      float64
	BaselineMarketTime time.Time
	ReviewDueDate      time.Time
	ModelVersion       string
	PromptVersion      string
}

// CompileStructuredAnalysis treats model JSON as untrusted input. Authoritative
// stock, validation, price, timing, and version fields come from local context.
func CompileStructuredAnalysis(raw []byte, context AnalysisSnapshotContext) (*models.AIRecommendationSnapshot, error) {
	var output StructuredAnalysisOutput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("decode structured analysis: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if err := validateStructuredAnalysis(output, context.DataAsOf); err != nil {
		return nil, err
	}
	if context.Job.ID == 0 || context.Job.StockCode == "" || context.Job.ValidationBatchID == 0 || context.StockName == "" ||
		context.InputBundle.ID == 0 || len(context.InputBundle.BundleHash) != 64 || context.InputBundle.JobID != context.Job.ID ||
		context.InputBundle.StockCode != context.Job.StockCode || context.InputBundle.ValidationBatchID != context.Job.ValidationBatchID ||
		!context.InputBundle.DataAsOf.Equal(context.DataAsOf) ||
		context.CompletedAt.IsZero() || context.DataAsOf.IsZero() || context.BaselineMarketTime.IsZero() || context.ReviewDueDate.IsZero() ||
		context.ModelVersion == "" || context.PromptVersion == "" || !finite(context.BaselinePrice) || context.BaselinePrice <= 0 {
		return nil, errors.New("analysis snapshot context is incomplete")
	}
	if context.DataAsOf.After(context.CompletedAt) || context.BaselineMarketTime.After(context.CompletedAt) || context.ReviewDueDate.Before(context.CompletedAt) {
		return nil, errors.New("analysis snapshot context times are inconsistent")
	}
	frozen, err := validateFrozenInputBundle(context.InputBundle, context.Job)
	if err != nil {
		return nil, fmt.Errorf("validate analysis evidence bundle: %w", err)
	}
	if err := validateAnalysisEvidenceProvenance(output.Evidence, frozen); err != nil {
		return nil, err
	}
	components, err := authoritativeScoreComponents(output, frozen)
	if err != nil {
		return nil, err
	}
	penalties, err := LocalRiskPenalties(frozen.RiskLabels)
	if err != nil {
		return nil, err
	}
	score, err := CalculateIndex(components, penalties)
	if err != nil {
		return nil, err
	}
	componentsJSON, _ := json.Marshal(components)
	penaltiesJSON, _ := json.Marshal(penalties)
	labels, _ := json.Marshal(output.RiskLabels)
	evidence, _ := json.Marshal(output.Evidence)
	conclusions, _ := json.Marshal(output.AgentConclusions)
	return &models.AIRecommendationSnapshot{SourceType: SourceAutomatic, ValidationBatchID: context.Job.ValidationBatchID,
		InputBundleID: context.InputBundle.ID, InputBundleHash: context.InputBundle.BundleHash,
		StockCode: context.Job.StockCode, StockName: context.StockName, RiskLabelsJSON: string(labels), CompletedAt: context.CompletedAt,
		DataAsOf: context.DataAsOf, BaselinePrice: context.BaselinePrice, BaselineMarketTime: context.BaselineMarketTime,
		RiseProbability: output.RiseProbability, ReturnRangeLow: output.ReturnRangeLow, ReturnRangeHigh: output.ReturnRangeHigh,
		AIRecommendationIndex: score.Index, ScoreComponentsJSON: string(componentsJSON), PenaltiesJSON: string(penaltiesJSON),
		Rationale: strings.TrimSpace(output.Rationale), RiskNotes: strings.TrimSpace(output.RiskNotes), ModelVersion: context.ModelVersion,
		PromptVersion: context.PromptVersion, ProbabilityNotice: ProbabilityNotice, EvidenceJSON: string(evidence), AgentConclusionsJSON: string(conclusions),
		StrategyVersion: score.StrategyVersion, ReviewDueDate: context.ReviewDueDate, Status: "active"}, nil
}

func authoritativeScoreComponents(output StructuredAnalysisOutput, frozen frozenAnalysisInput) (ScoreComponents, error) {
	var screening screeningEvidence
	decoder := json.NewDecoder(bytes.NewReader(frozen.Screening))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&screening); err != nil || ensureJSONEOF(decoder) != nil || screening.Version != PreScreeningVersion {
		return ScoreComponents{}, errors.New("frozen screening score evidence is invalid")
	}
	returnOpportunity, err := CalculateReturnOpportunity(output.ReturnRangeLow, output.ReturnRangeHigh)
	if err != nil {
		return ScoreComponents{}, err
	}
	expectedLabels := append([]string(nil), frozen.RiskLabels...)
	actualLabels := append([]string(nil), output.RiskLabels...)
	sort.Strings(expectedLabels)
	sort.Strings(actualLabels)
	if !slices.Equal(expectedLabels, actualLabels) {
		return ScoreComponents{}, errors.New("model risk labels conflict with frozen inputs")
	}
	return ScoreComponents{RiseProbability: output.RiseProbability, ReturnOpportunity: returnOpportunity,
		VolatilitySafety: screening.RiskSafety, Liquidity: screening.Liquidity,
		AgentConsensus: SingleCallConsensusScore, DataQuality: screening.DataQuality}, nil
}

func validateAnalysisEvidenceProvenance(evidence []AnalysisEvidence, frozen frozenAnalysisInput) error {
	allowed := make(map[string]TimedAnalysisFact, len(frozen.DailyBars)+len(frozen.FinancialFacts)+len(frozen.News)+len(frozen.Announcements))
	for _, bar := range frozen.DailyBars {
		allowed[bar.EvidenceID] = TimedAnalysisFact{ID: bar.EvidenceID, Category: "market", Value: bar.EvidenceTitle,
			Source: bar.Source, PublishedAt: bar.TradeDate}
	}
	for _, fact := range append(append(append([]TimedAnalysisFact{}, frozen.FinancialFacts...), frozen.News...), frozen.Announcements...) {
		allowed[fact.ID] = fact
	}
	for _, item := range evidence {
		fact, exists := allowed[item.ID]
		if !exists || item.Title != fact.Value || item.Source != fact.Source || !item.PublishedAt.Equal(fact.PublishedAt) {
			return fmt.Errorf("analysis evidence %s is not an exact frozen input fact", item.ID)
		}
	}
	return nil
}

func validateStructuredAnalysis(output StructuredAnalysisOutput, dataAsOf time.Time) error {
	if output.SchemaVersion != AnalysisSchemaVersion || output.ProbabilityNotice != ProbabilityNotice {
		return errors.New("analysis schema or probability notice is invalid")
	}
	if !finite(output.RiseProbability) || output.RiseProbability < 0 || output.RiseProbability > 100 ||
		!finite(output.ReturnRangeLow) || !finite(output.ReturnRangeHigh) || output.ReturnRangeLow > output.ReturnRangeHigh {
		return errors.New("analysis probability or return range is invalid")
	}
	if strings.TrimSpace(output.Rationale) == "" || strings.TrimSpace(output.RiskNotes) == "" || len(output.Evidence) == 0 {
		return errors.New("analysis rationale, risks, and evidence are required")
	}
	for _, role := range requiredAnalysisRoles {
		if strings.TrimSpace(output.AgentConclusions[role]) == "" {
			return fmt.Errorf("analysis role %s is required", role)
		}
	}
	seenEvidence := make(map[string]bool, len(output.Evidence))
	for _, item := range output.Evidence {
		if item.ID == "" || item.Title == "" || item.Source == "" || item.PublishedAt.IsZero() || item.PublishedAt.After(dataAsOf) || seenEvidence[item.ID] {
			return errors.New("analysis evidence is incomplete, duplicated, or newer than data cutoff")
		}
		seenEvidence[item.ID] = true
	}
	seenLabels := make(map[string]bool, len(output.RiskLabels))
	for _, label := range output.RiskLabels {
		if strings.TrimSpace(label) == "" || seenLabels[label] {
			return errors.New("risk labels must be non-empty and unique")
		}
		seenLabels[label] = true
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("structured analysis contains trailing JSON values")
	}
	return nil
}
