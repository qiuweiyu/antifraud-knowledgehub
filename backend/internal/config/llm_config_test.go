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
}

func TestLLMAssistanceDefaultsDisabled(t *testing.T) {
	clearLLMAssistanceEnv(t)

	cfg := Load()
	if cfg.LLMAssistanceEnabled {
		t.Fatal("LLM assistance must be disabled by default")
	}
	if cfg.LLMAssistanceProvider != "" {
		t.Fatalf("expected empty provider by default, got %q", cfg.LLMAssistanceProvider)
	}
	if cfg.LLMAssistanceTimeout != 5*time.Second {
		t.Fatalf("expected 5s default timeout, got %s", cfg.LLMAssistanceTimeout)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled LLM assistance should validate: %v", err)
	}
}

func TestLLMAssistanceInvalidBooleanFailsClosed(t *testing.T) {
	t.Setenv("LLM_ASSISTANCE_ENABLED", "definitely")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "future-provider")
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "5s")

	cfg := Load()
	if cfg.LLMAssistanceEnabled {
		t.Fatal("invalid LLM assistance enablement must fail closed to disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fail-closed disabled config should validate: %v", err)
	}
}

func TestLLMAssistanceEnabledIsRejectedUntilRuntimeProviderExists(t *testing.T) {
	t.Setenv("LLM_ASSISTANCE_ENABLED", "true")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", " future-provider ")
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "5s")

	cfg := Load()
	if !cfg.LLMAssistanceEnabled {
		t.Fatal("expected LLM assistance enablement to parse true")
	}
	if cfg.LLMAssistanceProvider != "future-provider" {
		t.Fatalf("expected trimmed provider identifier, got %q", cfg.LLMAssistanceProvider)
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "no runtime provider") {
		t.Fatalf("enabled scaffold must reject activation until a runtime provider exists, got %v", err)
	}
}

func TestLLMAssistanceEnabledRejectsInvalidTimeoutFirst(t *testing.T) {
	t.Setenv("LLM_ASSISTANCE_ENABLED", "true")
	t.Setenv("LLM_ASSISTANCE_PROVIDER", "future-provider")
	t.Setenv("LLM_ASSISTANCE_TIMEOUT", "not-a-duration")

	err := Load().Validate()
	if err == nil || !strings.Contains(err.Error(), "LLM_ASSISTANCE_TIMEOUT") {
		t.Fatalf("invalid enabled timeout must fail validation, got %v", err)
	}
}

func TestLLMAssistanceConfigHasNoGenericCredentialOrEndpointFields(t *testing.T) {
	typeInfo := reflect.TypeOf(Config{})
	for _, forbidden := range []string{"LLMAPIKey", "LLMBaseURL", "LLMEndpoint", "LLMAssistanceAPIKey", "LLMAssistanceBaseURL"} {
		if _, ok := typeInfo.FieldByName(forbidden); ok {
			t.Fatalf("first slice must not expose generic provider credential/endpoint field %s", forbidden)
		}
	}
}
