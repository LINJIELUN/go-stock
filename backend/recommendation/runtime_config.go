package recommendation

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	RuntimeConfigVersion = "shadow-runtime-config-v0.1"
	defaultSoftBudgetUSD = 50
	defaultHardBudgetUSD = 100
)

// RuntimeEnvironment is deliberately injectable so tests and desktop settings
// never need to mutate process-global environment variables.
type RuntimeEnvironment interface {
	LookupEnv(string) (string, bool)
}

type RuntimeConfig struct {
	Version             string
	Enabled             bool
	TushareToken        string
	ModelAPIKey         string
	ModelEndpoint       string
	ModelProvider       string
	ModelID             string
	PromptVersion       string
	WorkerExecutable    string
	WorkerDirectory     string
	WorkerManifest      string
	InputUSDPerMillion  float64
	OutputUSDPerMillion float64
	SoftBudgetUSD       float64
	HardBudgetUSD       float64
	TickInterval        time.Duration
	RetryInitial        time.Duration
	RetryMaximum        time.Duration
}

type RuntimeConfigIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RuntimeReadiness struct {
	Version string               `json:"version"`
	Enabled bool                 `json:"enabled"`
	Ready   bool                 `json:"ready"`
	Issues  []RuntimeConfigIssue `json:"issues"`
}

func LoadRuntimeConfig(environment RuntimeEnvironment) (RuntimeConfig, error) {
	if environment == nil {
		return RuntimeConfig{}, errors.New("runtime configuration requires environment")
	}
	config := RuntimeConfig{
		Version:       RuntimeConfigVersion,
		SoftBudgetUSD: defaultSoftBudgetUSD,
		HardBudgetUSD: defaultHardBudgetUSD,
		TickInterval:  5 * time.Minute,
		RetryInitial:  time.Minute,
		RetryMaximum:  30 * time.Minute,
	}
	enabled, err := optionalBool(environment, "AI_SHADOW_ENABLED")
	if err != nil {
		return RuntimeConfig{}, err
	}
	config.Enabled = enabled
	config.TushareToken = secret(environment, "TUSHARE_TOKEN")
	config.ModelAPIKey = secret(environment, "AI_SHADOW_MODEL_API_KEY")
	config.ModelEndpoint = value(environment, "AI_SHADOW_MODEL_ENDPOINT")
	config.ModelProvider = value(environment, "AI_SHADOW_MODEL_PROVIDER")
	config.ModelID = value(environment, "AI_SHADOW_MODEL_ID")
	config.PromptVersion = value(environment, "AI_SHADOW_PROMPT_VERSION")
	config.WorkerExecutable = value(environment, "AI_SHADOW_WORKER_EXECUTABLE")
	config.WorkerDirectory = value(environment, "AI_SHADOW_WORKER_DIRECTORY")
	config.WorkerManifest = value(environment, "AI_SHADOW_WORKER_MANIFEST")
	if config.InputUSDPerMillion, err = optionalFloat(environment, "AI_SHADOW_INPUT_USD_PER_MILLION", 0); err != nil {
		return RuntimeConfig{}, err
	}
	if config.OutputUSDPerMillion, err = optionalFloat(environment, "AI_SHADOW_OUTPUT_USD_PER_MILLION", 0); err != nil {
		return RuntimeConfig{}, err
	}
	if config.SoftBudgetUSD, err = optionalFloat(environment, "AI_SHADOW_SOFT_BUDGET_USD", config.SoftBudgetUSD); err != nil {
		return RuntimeConfig{}, err
	}
	if config.HardBudgetUSD, err = optionalFloat(environment, "AI_SHADOW_HARD_BUDGET_USD", config.HardBudgetUSD); err != nil {
		return RuntimeConfig{}, err
	}
	return config, nil
}

// Readiness returns only non-secret evidence. It intentionally does not expose
// tokens, API keys, or their lengths/prefixes.
func (c RuntimeConfig) Readiness() RuntimeReadiness {
	result := RuntimeReadiness{Version: RuntimeConfigVersion, Enabled: c.Enabled, Issues: make([]RuntimeConfigIssue, 0)}
	if !c.Enabled {
		result.Issues = append(result.Issues, RuntimeConfigIssue{"disabled", "影子运行尚未显式启用"})
		return result
	}
	add := func(condition bool, code, message string) {
		if condition {
			result.Issues = append(result.Issues, RuntimeConfigIssue{code, message})
		}
	}
	add(strings.TrimSpace(c.TushareToken) == "", "missing_market_token", "缺少 TuShare 日线数据凭据")
	add(strings.TrimSpace(c.ModelAPIKey) == "", "missing_model_key", "缺少模型供应商 API Key")
	add(strings.TrimSpace(c.ModelProvider) == "", "missing_model_provider", "缺少模型供应商标识")
	add(strings.TrimSpace(c.ModelID) == "", "missing_model_id", "缺少模型 ID")
	add(strings.TrimSpace(c.PromptVersion) == "", "missing_prompt_version", "缺少提示词版本")
	add(!validHTTPSURL(c.ModelEndpoint), "invalid_model_endpoint", "模型端点必须是无内嵌凭据和查询参数的 HTTPS URL")
	add(!filepath.IsAbs(c.WorkerExecutable), "invalid_worker_executable", "Worker 可执行文件必须使用绝对路径")
	add(!filepath.IsAbs(c.WorkerDirectory), "invalid_worker_directory", "Worker 工作目录必须使用绝对路径")
	add(!filepath.IsAbs(c.WorkerManifest), "invalid_worker_manifest", "Worker 构件清单必须使用绝对路径")
	add(!finiteNonNegative(c.InputUSDPerMillion) || !finiteNonNegative(c.OutputUSDPerMillion), "invalid_model_pricing", "模型 Token 单价必须是有限非负数")
	add(!finitePositive(c.SoftBudgetUSD) || !finitePositive(c.HardBudgetUSD) || c.SoftBudgetUSD > c.HardBudgetUSD, "invalid_budget", "模型预算必须为有限正数且软上限不能超过硬上限")
	add(c.TickInterval <= 0 || c.RetryInitial <= 0 || c.RetryMaximum < c.RetryInitial, "invalid_schedule", "运行周期和重试间隔配置无效")
	result.Ready = len(result.Issues) == 0
	return result
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
func finitePositive(value float64) bool { return finiteNonNegative(value) && value > 0 }

func validHTTPSURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func value(environment RuntimeEnvironment, key string) string {
	raw, _ := environment.LookupEnv(key)
	return strings.TrimSpace(raw)
}

func secret(environment RuntimeEnvironment, key string) string {
	raw, _ := environment.LookupEnv(key)
	return raw
}

func optionalBool(environment RuntimeEnvironment, key string) (bool, error) {
	raw, exists := environment.LookupEnv(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("parse %s as boolean: %w", key, err)
	}
	return parsed, nil
}

func optionalFloat(environment RuntimeEnvironment, key string, fallback float64) (float64, error) {
	raw, exists := environment.LookupEnv(key)
	if !exists || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s as number: %w", key, err)
	}
	return parsed, nil
}
