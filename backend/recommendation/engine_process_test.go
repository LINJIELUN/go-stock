package recommendation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-stock/backend/models"
)

type fakeEngineRunner struct {
	output  []byte
	started bool
	err     error
	input   []byte
}

func (f *fakeEngineRunner) Run(_ context.Context, input []byte) (ProcessRunResult, error) {
	f.input = append([]byte(nil), input...)
	return ProcessRunResult{Output: f.output, Started: f.started}, f.err
}

type fixedEngineContext struct{ value EngineInputContext }

func (f fixedEngineContext) LoadEngineContext(context.Context, models.AIAnalysisJob) (EngineInputContext, error) {
	return f.value, nil
}

func engineFixture(t *testing.T) (models.AIAnalysisJob, EngineInputContext, []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	job := models.AIAnalysisJob{Model: gormModel(9), StockCode: "600000", StockName: "浦发银行", ValidationBatchID: 7, Attempts: 1, Status: JobRunning}
	bundle := models.AIAnalysisInputBundle{Model: gormModel(11), JobID: job.ID, StockCode: job.StockCode, ValidationBatchID: job.ValidationBatchID,
		BundleHash: strings.Repeat("c", 64), SchemaVersion: InputBundleSchemaVersion, DataAsOf: now.Add(-time.Hour), PayloadJSON: `{"schemaVersion":"analysis-input-bundle-v0.1"}`}
	snapshotContext := validAnalysisContext(now)
	snapshotContext.Job = job
	snapshotContext.InputBundle = bundle
	snapshotContext.DataAsOf = bundle.DataAsOf
	analysis := validAnalysisContract(t, bundle.DataAsOf)
	response, _ := json.Marshal(engineResponse{ProtocolVersion: EngineProtocolVersion, BundleHash: bundle.BundleHash, Analysis: analysis,
		ActualCostKnown: true, ActualCostUSD: 0.25, InputTokens: 1000, OutputTokens: 200})
	return job, EngineInputContext{Bundle: bundle, Snapshot: snapshotContext}, response
}

func TestIsolatedEngineClientBindsBundleAndCompilesOutput(t *testing.T) {
	job, engineContext, response := engineFixture(t)
	runner := &fakeEngineRunner{output: response, started: true}
	client, err := NewIsolatedEngineClient(runner, fixedEngineContext{engineContext}, IsolatedEngineConfig{
		Provider: "provider", Model: "model", PromptVersion: "prompt-v1", EstimatedCostUSD: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Analyze(context.Background(), job, ModelPlan{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequestSent || !result.ActualCostKnown || result.Snapshot == nil || result.Snapshot.InputBundleHash != engineContext.Bundle.BundleHash ||
		result.Snapshot.ModelVersion != "provider/model" || result.InputTokens != 1000 {
		t.Fatalf("unexpected isolated engine result: %+v", result)
	}
	if !strings.Contains(string(runner.input), engineContext.Bundle.BundleHash) || !strings.Contains(string(runner.input), engineContext.Bundle.PayloadJSON[1:20]) {
		t.Fatalf("runner did not receive frozen input: %s", runner.input)
	}
}

func TestIsolatedEngineClientRejectsBundleMismatch(t *testing.T) {
	job, engineContext, response := engineFixture(t)
	var decoded engineResponse
	json.Unmarshal(response, &decoded)
	decoded.BundleHash = strings.Repeat("d", 64)
	response, _ = json.Marshal(decoded)
	client, _ := NewIsolatedEngineClient(&fakeEngineRunner{output: response, started: true}, fixedEngineContext{engineContext},
		IsolatedEngineConfig{Provider: "provider", Model: "model", PromptVersion: "prompt-v1", EstimatedCostUSD: 0.5})
	result, err := client.Analyze(context.Background(), job, ModelPlan{})
	if err == nil || !result.RequestSent {
		t.Fatalf("expected sent mismatched response rejection: %+v %v", result, err)
	}
}

func TestSubprocessRunnerUsesDirectJSONStdin(t *testing.T) {
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat executable unavailable")
	}
	cat, _ = filepath.Abs(cat)
	digest := fileDigest(t, cat)
	runner, err := NewSubprocessRunner(cat, nil, []string{}, time.Second, 1024, EngineProcessPolicy{
		WorkingDirectory: t.TempDir(), Artifacts: []EngineArtifact{{Path: cat, SHA256: digest}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"safe":"$(echo not-a-shell)"}`)
	result, err := runner.Run(context.Background(), payload)
	if err != nil || !result.Started || string(result.Output) != string(payload) {
		t.Fatalf("subprocess did not preserve literal JSON: %q %v", result.Output, err)
	}
}

func TestSubprocessRunnerRejectsRelativeExecutableAndLimitsOutput(t *testing.T) {
	if _, err := NewSubprocessRunner("relative-engine", nil, nil, time.Second, 10, EngineProcessPolicy{}); err == nil {
		t.Fatal("expected relative executable rejection")
	}
	buffer := &limitedProcessBuffer{limit: 3}
	if _, err := buffer.Write([]byte("abcdef")); err != nil || !buffer.exceeded || string(buffer.Bytes()) != "abc" {
		t.Fatalf("output limiter failed: %q exceeded=%v err=%v", buffer.Bytes(), buffer.exceeded, err)
	}
}

func TestSubprocessRunnerRejectsChangedArtifactBeforeStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(path, []byte("original"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner, err := NewSubprocessRunner(path, nil, nil, time.Second, 1024, EngineProcessPolicy{
		WorkingDirectory: t.TempDir(), Artifacts: []EngineArtifact{{Path: path, SHA256: fileDigest(t, path)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), nil)
	if err == nil || result.Started || !strings.Contains(err.Error(), "integrity mismatch") {
		t.Fatalf("expected pre-start integrity rejection: %+v %v", result, err)
	}
}

func TestSubprocessRunnerRejectsEnvironmentOutsideAllowlist(t *testing.T) {
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat executable unavailable")
	}
	cat, _ = filepath.Abs(cat)
	_, err = NewSubprocessRunner(cat, nil, []string{"UNEXPECTED_SECRET=value"}, time.Second, 1024, EngineProcessPolicy{
		WorkingDirectory: t.TempDir(), Artifacts: []EngineArtifact{{Path: cat, SHA256: fileDigest(t, cat)}},
		AllowedEnvironmentKeys: []string{"MODEL_API_KEY"},
	})
	if err == nil || !strings.Contains(err.Error(), "non-allowlisted") {
		t.Fatalf("expected environment allowlist rejection: %v", err)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(value))
}
