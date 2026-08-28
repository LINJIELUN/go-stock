package recommendation

import (
	"strings"
	"testing"
)

type mapEnvironment map[string]string

func (e mapEnvironment) LookupEnv(key string) (string, bool) {
	value, ok := e[key]
	return value, ok
}

func completeRuntimeEnvironment() mapEnvironment {
	return mapEnvironment{
		"AI_SHADOW_ENABLED":                "true",
		"TUSHARE_TOKEN":                    "market-secret",
		"AI_SHADOW_MODEL_API_KEY":          "model-secret",
		"AI_SHADOW_MODEL_ENDPOINT":         "https://model.example/v1/chat/completions",
		"AI_SHADOW_MODEL_PROVIDER":         "example",
		"AI_SHADOW_MODEL_ID":               "example-model",
		"AI_SHADOW_PROMPT_VERSION":         "prompt-v1",
		"AI_SHADOW_WORKER_EXECUTABLE":      "/usr/bin/python3",
		"AI_SHADOW_WORKER_DIRECTORY":       "/opt/go-stock/worker",
		"AI_SHADOW_WORKER_MANIFEST":        "/opt/go-stock/worker/manifest.json",
		"AI_SHADOW_INPUT_USD_PER_MILLION":  "0.5",
		"AI_SHADOW_OUTPUT_USD_PER_MILLION": "1.5",
	}
}

func TestRuntimeConfigIsDisabledByDefault(t *testing.T) {
	config, err := LoadRuntimeConfig(mapEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	readiness := config.Readiness()
	if readiness.Enabled || readiness.Ready || len(readiness.Issues) != 1 || readiness.Issues[0].Code != "disabled" {
		t.Fatalf("unexpected disabled readiness: %+v", readiness)
	}
}

func TestRuntimeConfigAcceptsCompleteExplicitConfiguration(t *testing.T) {
	config, err := LoadRuntimeConfig(completeRuntimeEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if readiness := config.Readiness(); !readiness.Ready || len(readiness.Issues) != 0 {
		t.Fatalf("unexpected readiness blockers: %+v", readiness)
	}
	if config.SoftBudgetUSD != 50 || config.HardBudgetUSD != 100 {
		t.Fatalf("confirmed default budget was not preserved: %+v", config)
	}
}

func TestRuntimeReadinessNeverExposesSecrets(t *testing.T) {
	environment := completeRuntimeEnvironment()
	delete(environment, "AI_SHADOW_MODEL_ID")
	config, err := LoadRuntimeConfig(environment)
	if err != nil {
		t.Fatal(err)
	}
	readiness := config.Readiness()
	text := strings.Join([]string{readiness.Issues[0].Code, readiness.Issues[0].Message}, " ")
	for _, secret := range []string{"market-secret", "model-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("readiness leaked secret %q: %+v", secret, readiness)
		}
	}
}

func TestRuntimeConfigRejectsAmbiguousOrUnsafeValues(t *testing.T) {
	environment := completeRuntimeEnvironment()
	environment["AI_SHADOW_ENABLED"] = "sometimes"
	if _, err := LoadRuntimeConfig(environment); err == nil {
		t.Fatal("expected invalid enabled flag rejection")
	}
	environment = completeRuntimeEnvironment()
	environment["AI_SHADOW_MODEL_ENDPOINT"] = "https://user:pass@model.example/v1?token=x"
	environment["AI_SHADOW_SOFT_BUDGET_USD"] = "101"
	environment["AI_SHADOW_HARD_BUDGET_USD"] = "100"
	config, err := LoadRuntimeConfig(environment)
	if err != nil {
		t.Fatal(err)
	}
	readiness := config.Readiness()
	if readiness.Ready || len(readiness.Issues) < 2 {
		t.Fatalf("unsafe configuration passed readiness: %+v", readiness)
	}
}
