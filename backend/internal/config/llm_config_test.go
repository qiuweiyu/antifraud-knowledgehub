package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	configurableOpenAITestModel  = "gpt-configurable-test"
	configurableGeminiTestModel  = "gemini-configurable-test"
	configurableDeepSeekTestModel = "deepseek-configurable-test"
)

func clearLLMAssistanceEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_ASSISTANCE_ENABLED", "")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "")
	t.Setenv("LLM_ASSISTANCE_MODEL", "")
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
}

func setValidOpenAIAssistanceEnv(t *testing.T) {
	t.Helper()
	clearLLMAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_ENABLED", "true")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "openai")
	t.Setenv("LLM_ASSISTANCE_MODEL", configurableOpenAITestModel)
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "5s")
	t.Setenv("OPENAI_API_KEY", "test-server-side-key")
}

func setValidGeminiAssistanceEnv(t *testing.T) {
	t.Helper()
	clearLLMAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_ENABLED", "true")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "gemini")
	t.Setenv("LLM_ASSISTANCE_MODEL", configurableGeminiTestModel)
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "5s")
	t.Setenv("GEMINI_API_KEY", "gemini-test-server-side-key")
}

func setValidDeepSeekAssistanceEnv(t *testing.T) {
	t.Helper()
	clearLLMAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_ENABLED", "true")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "deepseek")
	t.Setenv("LLM_ASSISTANCE_MODEL", configurableDeepSeekTestModel)
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "5s")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-test-server-side-key")
}

func TestLLMAssistanceDefaultsDisabled(t *testing.T) {
	clearLLMAssistanceEnv(t)

	cfg := Load()
	if cfg.LLMAssistanceEnabled {
		t.Fatal("LLM assistance must be disabled by default")
	}
	if cfg.LLMAssistanceProvider != "" {
		t.Fatalf("provider should default empty, got %q", cfg.LLMAssistanceProvider)
	}
	if cfg.LLMAssistanceModel != "" {
		t.Fatalf("model should default empty, got %q", cfg.LLMAssistanceModel)
	}
	if cfg.OpenAIAPIKey != "" || cfg.GeminiAPIKey != "" || cfg.DeepSeekAPIKey != "" {
		t.Fatal("provider API keys should default empty")
	}
	if cfg.LLMAssistanceTimeout != 5*time.Second {
		t.Fatalf("expected 5s default timeout, got %s", cfg.LLMAssistanceTimeout)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled LLM assistance should validate: %v", err)
	}
}

func TestLLMAssistanceDisabledDoesNotRequireProviderConfiguration(t *testing.T) {
	clearLLMAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "future-provider")
	t.Setenv("LLM_ASSISTANCE_MODEL", "future-model")

	if err := Load().Validate(); err != nil {
		t.Fatalf("disabled LLM assistance must ignore inactive provider values: %v", err)
	}
}

func TestLLMAssistanceInvalidBooleanFailsClosed(t *testing.T) {
	clearLLMAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_ENABLED", "definitely")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "not-openai")
	t.Setenv("LLM_ASSISTANCE_MODEL", "not-used")

	cfg := Load()
	if cfg.LLMAssistanceEnabled {
		t.Fatal("invalid LLM assistance enablement must fail closed to disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fail-closed disabled config should validate: %v", err)
	}
}

func TestLLMAssistanceOpenAIConfigurationValidatesConfigurableModel(t *testing.T) {
	setValidOpenAIAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_PROVIDER", " openai ")
	t.Setenv("OPENAI_API_KEY", "  test-server-side-key  ")
	t.Setenv("LLM_ASSISTANCE_MODEL", " model-selected-at-runtime ")

	cfg := Load()
	if !cfg.LLMAssistanceEnabled {
		t.Fatal("expected LLM assistance enabled")
	}
	if cfg.LLMAssistanceProvider != "openai" {
		t.Fatalf("provider = %q", cfg.LLMAssistanceProvider)
	}
	if cfg.LLMAssistanceModel != "model-selected-at-runtime" {
		t.Fatalf("model = %q", cfg.LLMAssistanceModel)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid configurable OpenAI assistance config rejected: %v", err)
	}
	credential, err := cfg.LLMAssistanceCredential()
	if err != nil {
		t.Fatalf("resolve credential: %v", err)
	}
	if strings.TrimSpace(credential) != "test-server-side-key" {
		t.Fatal("unexpected resolved OpenAI credential")
	}
}

func TestLLMAssistanceGeminiConfigurationValidates(t *testing.T) {
	setValidGeminiAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_PROVIDER", " gemini ")
	t.Setenv("GEMINI_API_KEY", "  gemini-test-server-side-key  ")
	t.Setenv("LLM_ASSISTANCE_MODEL", " gemini-runtime-model_1.2 ")

	cfg := Load()
	if cfg.LLMAssistanceProvider != "gemini" {
		t.Fatalf("provider = %q", cfg.LLMAssistanceProvider)
	}
	if cfg.LLMAssistanceModel != "gemini-runtime-model_1.2" {
		t.Fatalf("model = %q", cfg.LLMAssistanceModel)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid Gemini assistance config rejected: %v", err)
	}
	credential, err := cfg.LLMAssistanceCredential()
	if err != nil {
		t.Fatalf("resolve Gemini credential: %v", err)
	}
	if strings.TrimSpace(credential) != "gemini-test-server-side-key" {
		t.Fatal("unexpected resolved Gemini credential")
	}
}

func TestLLMAssistanceDeepSeekConfigurationValidates(t *testing.T) {
	setValidDeepSeekAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_PROVIDER", " deepseek ")
	t.Setenv("DEEPSEEK_API_KEY", "  deepseek-test-server-side-key  ")
	t.Setenv("LLM_ASSISTANCE_MODEL", " deepseek-runtime-model-v4 ")

	cfg := Load()
	if cfg.LLMAssistanceProvider != "deepseek" {
		t.Fatalf("provider = %q", cfg.LLMAssistanceProvider)
	}
	if cfg.LLMAssistanceModel != "deepseek-runtime-model-v4" {
		t.Fatalf("model = %q", cfg.LLMAssistanceModel)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid DeepSeek assistance config rejected: %v", err)
	}
	credential, err := cfg.LLMAssistanceCredential()
	if err != nil {
		t.Fatalf("resolve DeepSeek credential: %v", err)
	}
	if strings.TrimSpace(credential) != "deepseek-test-server-side-key" {
		t.Fatal("unexpected resolved DeepSeek credential")
	}
}

func TestLLMAssistanceConfigurationRejectsInvalidActivation(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "invalid timeout", key: "LLM_ASSISTANCE_TIMEOUT", value: "not-a-duration", want: "LLM_ASSISTANCE_TIMEOUT"},
		{name: "too small timeout", key: "LLM_ASSISTANCE_TIMEOUT", value: "0s", want: "LLM_ASSISTANCE_TIMEOUT"},
		{name: "unregistered provider", key: "LLM_ASSISTANCE_PROVIDER", value: "future-provider", want: "credential resolver"},
		{name: "blank key", key: "OPENAI_API_KEY", value: "   ", want: "OPENAI_API_KEY"},
		{name: "blank model", key: "LLM_ASSISTANCE_MODEL", value: "   ", want: "LLM_ASSISTANCE_MODEL"},
		{name: "control character model", key: "LLM_ASSISTANCE_MODEL", value: "bad\nmodel", want: "LLM_ASSISTANCE_MODEL"},
		{name: "overlong model", key: "LLM_ASSISTANCE_MODEL", value: strings.Repeat("m", 129), want: "LLM_ASSISTANCE_MODEL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setValidOpenAIAssistanceEnv(t)
			t.Setenv(tc.key, tc.value)
			err := Load().Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s validation failure, got %v", tc.want, err)
			}
		})
	}
}

func TestLLMAssistanceGeminiConfigurationRejectsMissingKeyAndUnsafeModel(t *testing.T) {
	setValidGeminiAssistanceEnv(t)
	t.Setenv("GEMINI_API_KEY", "   ")
	if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Fatalf("blank Gemini key must fail closed, got %v", err)
	}

	setValidGeminiAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_MODEL", "gemini/unsafe-model")
	if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "LLM provider configuration") {
		t.Fatalf("unsafe Gemini model path must fail provider construction, got %v", err)
	}
}

func TestLLMAssistanceDeepSeekConfigurationRejectsMissingKey(t *testing.T) {
	setValidDeepSeekAssistanceEnv(t)
	t.Setenv("DEEPSEEK_API_KEY", "   ")
	if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatalf("blank DeepSeek key must fail closed, got %v", err)
	}
}

func TestLLMAssistanceConfigHasProviderSpecificKeysButNoGenericCredentialOrEndpoint(t *testing.T) {
	typeInfo := reflect.TypeOf(Config{})
	for _, forbidden := range []string{
		"LLMAPIKey",
		"LLMBaseURL",
		"LLMEndpoint",
		"LLMAssistanceAPIKey",
		"LLMAssistanceBaseURL",
		"OpenAIBaseURL",
		"OpenAIEndpoint",
		"OpenAIModel",
		"GeminiBaseURL",
		"GeminiEndpoint",
		"GeminiModel",
		"DeepSeekBaseURL",
		"DeepSeekEndpoint",
		"DeepSeekModel",
	} {
		if _, ok := typeInfo.FieldByName(forbidden); ok {
			t.Fatalf("config must not expose deprecated/generic unsafe field %s", forbidden)
		}
	}
	for _, required := range []string{"OpenAIAPIKey", "GeminiAPIKey", "DeepSeekAPIKey", "LLMAssistanceModel"} {
		if _, ok := typeInfo.FieldByName(required); !ok {
			t.Fatalf("config must expose %s", required)
		}
	}
}
