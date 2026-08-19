package recommendation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
	workDir     string
	artifacts   []EngineArtifact
	timeout     time.Duration
	maxOutput   int
}

type EngineArtifact struct {
	Path   string
	SHA256 string
}

type EngineProcessPolicy struct {
	WorkingDirectory       string
	Artifacts              []EngineArtifact
	AllowedEnvironmentKeys []string
}

var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func NewSubprocessRunner(executable string, args, environment []string, timeout time.Duration, maxOutput int, policy EngineProcessPolicy) (*SubprocessRunner, error) {
	if executable == "" || !filepath.IsAbs(executable) || timeout <= 0 || maxOutput <= 0 {
		return nil, errors.New("engine subprocess requires absolute executable, timeout, and output limit")
	}
	resolvedExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve engine executable: %w", err)
	}
	workDir, err := validateEngineWorkingDirectory(policy.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	artifacts, executableCovered, err := validateEngineArtifacts(policy.Artifacts, resolvedExecutable)
	if err != nil {
		return nil, err
	}
	if !executableCovered {
		return nil, errors.New("engine artifact manifest must include the executable")
	}
	if err := validateEngineEnvironment(environment, policy.AllowedEnvironmentKeys); err != nil {
		return nil, err
	}
	return &SubprocessRunner{executable: resolvedExecutable, args: append([]string(nil), args...), environment: append([]string(nil), environment...),
		workDir: workDir, artifacts: artifacts, timeout: timeout, maxOutput: maxOutput}, nil
}

// Run invokes an absolute executable directly without a command shell. The
// child receives only the explicitly configured environment and JSON stdin.
func (r *SubprocessRunner) Run(ctx context.Context, input []byte) (ProcessRunResult, error) {
	if err := verifyEngineArtifacts(r.artifacts); err != nil {
		return ProcessRunResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, r.executable, r.args...)
	command.Env = append([]string(nil), r.environment...)
	command.Dir = r.workDir
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

func validateEngineWorkingDirectory(value string) (string, error) {
	if value == "" || !filepath.IsAbs(value) {
		return "", errors.New("engine subprocess requires an absolute working directory")
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", fmt.Errorf("resolve engine working directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("engine working directory must be an existing directory")
	}
	return resolved, nil
}

func validateEngineArtifacts(values []EngineArtifact, executable string) ([]EngineArtifact, bool, error) {
	if len(values) == 0 {
		return nil, false, errors.New("engine artifact manifest is required")
	}
	artifacts := make([]EngineArtifact, 0, len(values))
	covered := false
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !filepath.IsAbs(value.Path) || len(value.SHA256) != sha256.Size*2 {
			return nil, false, errors.New("engine artifact requires absolute path and SHA-256")
		}
		resolved, err := filepath.EvalSymlinks(value.Path)
		if err != nil {
			return nil, false, fmt.Errorf("resolve engine artifact: %w", err)
		}
		digest := strings.ToLower(value.SHA256)
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, false, errors.New("engine artifact SHA-256 is invalid")
		}
		if _, exists := seen[resolved]; exists {
			return nil, false, errors.New("engine artifact manifest contains duplicate path")
		}
		seen[resolved] = struct{}{}
		artifacts = append(artifacts, EngineArtifact{Path: resolved, SHA256: digest})
		covered = covered || resolved == executable
	}
	if err := verifyEngineArtifacts(artifacts); err != nil {
		return nil, false, err
	}
	return artifacts, covered, nil
}

func verifyEngineArtifacts(artifacts []EngineArtifact) error {
	for _, artifact := range artifacts {
		value, err := os.ReadFile(artifact.Path)
		if err != nil {
			return fmt.Errorf("read engine artifact: %w", err)
		}
		info, err := os.Stat(artifact.Path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("engine artifact must be a regular file")
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(value))
		if digest != artifact.SHA256 {
			return fmt.Errorf("engine artifact integrity mismatch: %s", filepath.Base(artifact.Path))
		}
	}
	return nil
}

func validateEngineEnvironment(environment, allowedKeys []string) error {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		if !environmentKeyPattern.MatchString(key) {
			return errors.New("engine environment allowlist contains invalid key")
		}
		allowed[key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		_, permitted := allowed[key]
		if !found || !environmentKeyPattern.MatchString(key) || !permitted {
			return errors.New("engine environment contains a non-allowlisted key")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("engine environment contains duplicate key")
		}
		seen[key] = struct{}{}
	}
	return nil
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
