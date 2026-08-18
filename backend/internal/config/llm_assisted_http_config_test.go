package config

import (
	"strings"
	"testing"
	"time"
)

const testLLMAssistedAnalysisToken = "abcdef0123456789abcdef0123456789"

func clearLLMAssistedHTTPEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_ASSISTED_ANALYSIS_HTTP_ENABLED", "")
	t.Setenv("LLM_ASSISTED_ANALYSIS_TOKEN", "")
	t.Setenv("LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT", "")
	t.Setenv("LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT", "")
	t.Setenv("LLM_ASSISTED_ANALYSIS_RATE_WINDOW", "")
}

func setValidLLMAssistedHTTPEnv(t *testing.T) {
	t.Helper()
	setValidOpenAIAssistanceEnv(t)
	clearLLMAssistedHTTPEnv(t)
	t.Setenv("LLM_ASSISTED_ANALYSIS_HTTP_ENABLED", "true")
	t.Setenv("LLM_ASSISTED_ANALYSIS_TOKEN", testLLMAssistedAnalysisToken)
	t.Setenv("LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT", "10")
	t.Setenv("LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT", "50")
	t.Setenv("LLM_ASSISTED_ANALYSIS_RATE_WINDOW", "1m")
}

func TestLLMAssistedAnalysisHTTPDefaultsDisabled(t *testing.T) {
	setValidOpenAIAssistanceEnv(t)
	clearLLMAssistedHTTPEnv(t)

	cfg := Load()
	if cfg.LLMAssistedAnalysisHTTPEnabled {
		t.Fatal("assisted-analysis HTTP transport must be disabled by default")
	}
	if cfg.LLMAssistedAnalysisToken != "" {
		t.Fatal("assisted-analysis token must default empty")
	}
	if cfg.LLMAssistedAnalysisCredentialLimit != 10 {
		t.Fatalf("credential limit default = %d", cfg.LLMAssistedAnalysisCredentialLimit)
	}
	if cfg.LLMAssistedAnalysisGlobalLimit != 50 {
		t.Fatalf("global limit default = %d", cfg.LLMAssistedAnalysisGlobalLimit)
	}
	if cfg.LLMAssistedAnalysisRateWindow != time.Minute {
		t.Fatalf("rate window default = %s", cfg.LLMAssistedAnalysisRateWindow)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled transport should validate: %v", err)
	}
}

func TestLLMAssistedAnalysisHTTPRequiresLLMAssistance(t *testing.T) {
	clearLLMAssistanceEnv(t)
	clearLLMAssistedHTTPEnv(t)
	t.Setenv("LLM_ASSISTED_ANALYSIS_HTTP_ENABLED", "true")
	t.Setenv("LLM_ASSISTED_ANALYSIS_TOKEN", testLLMAssistedAnalysisToken)

	err := Load().Validate()
	if err == nil || !strings.Contains(err.Error(), "LLM_ASSISTANCE_ENABLED") {
		t.Fatalf("enabled HTTP transport must require LLM assistance, got %v", err)
	}
}

func TestLLMAssistedAnalysisHTTPValidConfiguration(t *testing.T) {
	setValidLLMAssistedHTTPEnv(t)

	cfg := Load()
	if !cfg.LLMAssistedAnalysisHTTPEnabled {
		t.Fatal("expected HTTP transport enabled")
	}
	if cfg.LLMAssistedAnalysisToken != testLLMAssistedAnalysisToken {
		t.Fatal("unexpected assisted-analysis token")
	}
	if cfg.LLMAssistedAnalysisCredentialLimit != 10 || cfg.LLMAssistedAnalysisGlobalLimit != 50 {
		t.Fatalf("unexpected rate limits: %d/%d", cfg.LLMAssistedAnalysisCredentialLimit, cfg.LLMAssistedAnalysisGlobalLimit)
	}
	if cfg.LLMAssistedAnalysisRateWindow != time.Minute {
		t.Fatalf("rate window = %s", cfg.LLMAssistedAnalysisRateWindow)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid assisted-analysis transport config rejected: %v", err)
	}
}

func TestLLMAssistedAnalysisHTTPRejectsInvalidControls(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "blank token", key: "LLM_ASSISTED_ANALYSIS_TOKEN", value: "   ", want: "LLM_ASSISTED_ANALYSIS_TOKEN"},
		{name: "short token", key: "LLM_ASSISTED_ANALYSIS_TOKEN", value: "too-short", want: "LLM_ASSISTED_ANALYSIS_TOKEN"},
		{name: "zero credential limit", key: "LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT", value: "0", want: "LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT"},
		{name: "invalid credential limit", key: "LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT", value: "invalid", want: "LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT"},
		{name: "zero global limit", key: "LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT", value: "0", want: "LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT"},
		{name: "global below credential", key: "LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT", value: "9", want: "LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT"},
		{name: "invalid window", key: "LLM_ASSISTED_ANALYSIS_RATE_WINDOW", value: "bad-duration", want: "LLM_ASSISTED_ANALYSIS_RATE_WINDOW"},
		{name: "zero window", key: "LLM_ASSISTED_ANALYSIS_RATE_WINDOW", value: "0s", want: "LLM_ASSISTED_ANALYSIS_RATE_WINDOW"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setValidLLMAssistedHTTPEnv(t)
			t.Setenv(tc.key, tc.value)
			err := Load().Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s validation failure, got %v", tc.want, err)
			}
		})
	}
}

func TestLLMAssistedAnalysisTokenMustBeIndependentFromRuleWorkflowCredentials(t *testing.T) {
	for _, key := range []string{
		"RULE_SUBMISSION_WRITE_TOKEN",
		"RULE_SUBMISSION_REVIEW_TOKEN",
		"RULE_SUBMISSION_PUBLICATION_TOKEN",
	} {
		t.Run(key, func(t *testing.T) {
			setValidLLMAssistedHTTPEnv(t)
			t.Setenv(key, testLLMAssistedAnalysisToken)
			err := Load().Validate()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("transport token reuse with %s must fail, got %v", key, err)
			}
		})
	}
}
