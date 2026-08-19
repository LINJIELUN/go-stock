package recommendation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"go-stock/backend/models"
)

const EngineProtocolVersion = "isolated-analysis-engine-v0.1"

type ProcessRunResult struct {
	Output  []byte
	Started bool
}

type EngineProcessRunner interface {
	Run(context.Context, []byte) (ProcessRunResult, error)
}

type SubprocessRunner struct {
	executable  string
	args        []string
	environment []string
	timeout     time.Duration
	maxOutput   int
}

func NewSubprocessRunner(executable string, args, environment []string, timeout time.Duration, maxOutput int) (*SubprocessRunner, error) {
	if executable == "" || !filepath.IsAbs(executable) || timeout <= 0 || maxOutput <= 0 {
		return nil, errors.New("engine subprocess requires absolute executable, timeout, and output limit")
	}
	return &SubprocessRunner{executable: executable, args: append([]string(nil), args...), environment: append([]string(nil), environment...),
		timeout: timeout, maxOutput: maxOutput}, nil
}

// Run invokes an absolute executable directly without a command shell. The
// child receives only the explicitly configured environment and JSON stdin.
func (r *SubprocessRunner) Run(ctx context.Context, input []byte) (ProcessRunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, r.executable, r.args...)
	command.Env = append([]string(nil), r.environment...)
	command.Stdin = bytes.NewReader(input)
	stdout := &limitedProcessBuffer{limit: r.maxOutput}
	stderr := &limitedProcessBuffer{limit: r.maxOutput}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Start(); err != nil {
		return ProcessRunResult{}, fmt.Errorf("start isolated analysis engine: %w", err)
	}
	result := ProcessRunResult{Started: true}
	err := command.Wait()
	if stdout.exceeded || stderr.exceeded {
		return result, errors.New("isolated analysis engine exceeded output limit")
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("isolated analysis engine timeout: %w", ctx.Err())
		}
		return result, fmt.Errorf("isolated analysis engine failed: %w: %s", err, stderr.String())
	}
	result.Output = append([]byte(nil), stdout.Bytes()...)
	return result, nil
}

type limitedProcessBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedProcessBuffer) Write(value []byte) (int, error) {
	if b.buffer.Len()+len(value) > b.limit {
		remaining := b.limit - b.buffer.Len()
		if remaining > 0 {
			_, _ = b.buffer.Write(value[:remaining])
		}
		b.exceeded = true
		return len(value), nil
	}
	return b.buffer.Write(value)
}
func (b *limitedProcessBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedProcessBuffer) String() string { return b.buffer.String() }

type EngineInputContext struct {
	Bundle   models.AIAnalysisInputBundle
	Snapshot AnalysisSnapshotContext
}

type EngineContextProvider interface {
	LoadEngineContext(context.Context, models.AIAnalysisJob) (EngineInputContext, error)
}

type IsolatedEngineConfig struct {
	Provider         string
	Model            string
	PromptVersion    string
	EstimatedCostUSD float64
}

type IsolatedEngineClient struct {
	runner   EngineProcessRunner
	contexts EngineContextProvider
	config   IsolatedEngineConfig
}

func NewIsolatedEngineClient(runner EngineProcessRunner, contexts EngineContextProvider, config IsolatedEngineConfig) (*IsolatedEngineClient, error) {
	if runner == nil || contexts == nil || config.Provider == "" || config.Model == "" || config.PromptVersion == "" || !validMoney(config.EstimatedCostUSD) {
		return nil, errors.New("isolated engine requires runner, context provider, model identity, prompt, and estimate")
	}
	return &IsolatedEngineClient{runner: runner, contexts: contexts, config: config}, nil
}

func (c *IsolatedEngineClient) Plan(context.Context, models.AIAnalysisJob) (ModelPlan, error) {
	return ModelPlan{Provider: c.config.Provider, Model: c.config.Model, EstimatedCostUSD: c.config.EstimatedCostUSD}, nil
}

type engineRequest struct {
	ProtocolVersion string          `json:"protocolVersion"`
	BundleHash      string          `json:"bundleHash"`
	Bundle          json.RawMessage `json:"bundle"`
	Model           string          `json:"model"`
	PromptVersion   string          `json:"promptVersion"`
}

type engineResponse struct {
	ProtocolVersion string          `json:"protocolVersion"`
	BundleHash      string          `json:"bundleHash"`
	Analysis        json.RawMessage `json:"analysis"`
	ActualCostKnown bool            `json:"actualCostKnown"`
	ActualCostUSD   float64         `json:"actualCostUsd"`
	InputTokens     int64           `json:"inputTokens"`
	OutputTokens    int64           `json:"outputTokens"`
}

func (c *IsolatedEngineClient) Analyze(ctx context.Context, job models.AIAnalysisJob, _ ModelPlan) (PaidAnalysisResult, error) {
	engineContext, err := c.contexts.LoadEngineContext(ctx, job)
	if err != nil {
		return PaidAnalysisResult{}, fmt.Errorf("load frozen engine context: %w", err)
	}
	if engineContext.Bundle.JobID != job.ID || engineContext.Bundle.BundleHash == "" || !json.Valid([]byte(engineContext.Bundle.PayloadJSON)) {
		return PaidAnalysisResult{}, errors.New("frozen engine context does not match job")
	}
	request, _ := json.Marshal(engineRequest{EngineProtocolVersion, engineContext.Bundle.BundleHash, json.RawMessage(engineContext.Bundle.PayloadJSON), c.config.Model, c.config.PromptVersion})
	process, runErr := c.runner.Run(ctx, request)
	result := PaidAnalysisResult{RequestSent: process.Started}
	if runErr != nil {
		return result, runErr
	}
	var response engineResponse
	decoder := json.NewDecoder(bytes.NewReader(process.Output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return result, fmt.Errorf("decode isolated engine response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return result, err
	}
	if response.ProtocolVersion != EngineProtocolVersion || response.BundleHash != engineContext.Bundle.BundleHash ||
		response.InputTokens < 0 || response.OutputTokens < 0 || !validMoney(response.ActualCostUSD) {
		return result, errors.New("isolated engine response identity or usage is invalid")
	}
	result.ActualCostKnown, result.ActualCostUSD = response.ActualCostKnown, response.ActualCostUSD
	result.InputTokens, result.OutputTokens = response.InputTokens, response.OutputTokens
	engineContext.Snapshot.InputBundle = engineContext.Bundle
	engineContext.Snapshot.ModelVersion = c.config.Provider + "/" + c.config.Model
	engineContext.Snapshot.PromptVersion = c.config.PromptVersion
	snapshot, err := CompileStructuredAnalysis(response.Analysis, engineContext.Snapshot)
	if err != nil {
		return result, err
	}
	result.Snapshot = snapshot
	return result, nil
}

var _ io.Writer = (*limitedProcessBuffer)(nil)
