package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func clearLLMAssistanceEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_ASSISTANCE_ENABLED", "")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "")
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
}

func setValidOpenAIAssistanceEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_ASSISTANCE_ENABLED", "true")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "openai")
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "5s")
	t.Setenv("OPENAI_API_KEY", "test-server-side-key")
	t.Setenv("OPENAI_MODEL", "gpt-5.6")
}

func TestLLMAssistanceDefaultsDisabled(t *testing.T) {
	clearLLMAssistanceEnv(t)

	cfg := Load()
	if cfg.LLMAssistanceEnabled {
		t.Fatal("LLM assistance must be disabled by default")
	}
	if cfg.LLMAssistanceProvider != "" || cfg.OpenAIAPIKey != "" || cfg.OpenAIModel != "" {
		t.Fatalf("provider-specific settings should default empty: %+v", cfg)
	}
	if cfg.LLMAssistanceTimeout != 5*time.Second {
		t.Fatalf("expected 5s default timeout, got %s", cfg.LLMAssistanceTimeout)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled LLM assistance should validate: %v", err)
	}
}

func TestLLMAssistanceDisabledDoesNotRequireOpenAIConfiguration(t *testing.T) {
	clearLLMAssistanceEnv(t)
	t.Setenv("OPENAI_MODEL", "unsupported-while-disabled")

	if err := Load().Validate(); err != nil {
		t.Fatalf("disabled LLM assistance must ignore provider-specific activation values: %v", err)
	}
}

func TestLLMAssistanceInvalidBooleanFailsClosed(t *testing.T) {
	clearLLMAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_ENABLED", "definitely")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "not-openai")
	t.Setenv("OPENAI_MODEL", "not-allowed")

	cfg := Load()
	if cfg.LLMAssistanceEnabled {
		t.Fatal("invalid LLM assistance enablement must fail closed to disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fail-closed disabled config should validate: %v", err)
	}
}

func TestLLMAssistanceOpenAIConfigurationValidates(t *testing.T) {
	setValidOpenAIAssistanceEnv(t)
	t.Setenv("LLM_ASSISTANCE_PROVIDER", " openai ")
	t.Setenv("OPENAI_API_KEY", "  test-server-side-key  ")
	t.Setenv("OPENAI_MODEL", " gpt-5.6 ")

	cfg := Load()
	if !cfg.LLMAssistanceEnabled {
		t.Fatal("expected LLM assistance enabled")
	}
	if cfg.LLMAssistanceProvider != "openai" || cfg.OpenAIModel != "gpt-5.6" {
		t.Fatalf("provider/model should be trimmed: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid OpenAI assistance config rejected: %v", err)
	}
}

func TestLLMAssistanceOpenAIConfigurationRejectsInvalidActivation(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "invalid timeout", key: "LLM_ASSISTANCE_TIMEOUT", value: "not-a-duration", want: "LLM_ASSISTANCE_TIMEOUT"},
		{name: "too small timeout", key: "LLM_ASSISTANCE_TIMEOUT", value: "0s", want: "LLM_ASSISTANCE_TIMEOUT"},
		{name: "wrong provider", key: "LLM_ASSISTANCE_PROVIDER", value: "other", want: "LLM_ASSISTANCE_PROVIDER"},
		{name: "blank key", key: "OPENAI_API_KEY", value: "   ", want: "OPENAI_API_KEY"},
		{name: "wrong model", key: "OPENAI_MODEL", value: "other-model", want: "OPENAI_MODEL"},
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

func TestLLMAssistanceConfigHasNoGenericOrOpenAIEndpointOverride(t *testing.T) {
	typeInfo := reflect.TypeOf(Config{})
	for _, forbidden := range []string{
		"LLMAPIKey",
		"LLMBaseURL",
		"LLMEndpoint",
		"LLMAssistanceAPIKey",
		"LLMAssistanceBaseURL",
		"OpenAIBaseURL",
		"OpenAIEndpoint",
	} {
		if _, ok := typeInfo.FieldByName(forbidden); ok {
			t.Fatalf("config must not expose generic/arbitrary provider endpoint field %s", forbidden)
		}
	}
	if _, ok := typeInfo.FieldByName("OpenAIAPIKey"); !ok {
		t.Fatal("bounded OpenAI provider must expose its server-side API key field")
	}
	if _, ok := typeInfo.FieldByName("OpenAIModel"); !ok {
		t.Fatal("bounded OpenAI provider must expose its allowlisted model field")
	}
}
