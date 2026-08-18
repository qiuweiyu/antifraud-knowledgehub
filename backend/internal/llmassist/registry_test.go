package llmassist

import (
	"context"
	"strings"
	"testing"
)

type registryTestProvider struct{}

func (registryTestProvider) Assist(context.Context, Input) (Assistance, error) {
	return Assistance{}, nil
}

func TestRegistryRegisterAndCreate(t *testing.T) {
	registry := NewRegistry()
	var got ProviderConfig
	if err := registry.Register("test-provider", func(cfg ProviderConfig) (Provider, error) {
		got = cfg
		return registryTestProvider{}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	provider, err := registry.Create("test-provider", ProviderConfig{
		Model:  "  model-x  ",
		APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if provider == nil {
		t.Fatal("expected provider")
	}
	if got.Model != "model-x" {
		t.Fatalf("factory model = %q", got.Model)
	}
	if got.APIKey != "provider-secret" {
		t.Fatal("registry must pass the provider credential unchanged")
	}
	if !registry.Has("test-provider") {
		t.Fatal("registered provider should be discoverable")
	}
}

func TestRegistryRejectsInvalidRegistration(t *testing.T) {
	registry := NewRegistry()
	factory := func(ProviderConfig) (Provider, error) { return registryTestProvider{}, nil }

	for _, name := range []string{"", "   ", "OpenAI", "bad/provider", strings.Repeat("a", maxProviderNameBytes+1)} {
		t.Run("name_"+strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			if err := registry.Register(name, factory); err == nil {
				t.Fatalf("provider name %q must be rejected", name)
			}
		})
	}
	if err := registry.Register("nil-factory", nil); err == nil {
		t.Fatal("nil factory must be rejected")
	}
	if err := registry.Register("duplicate", factory); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := registry.Register("duplicate", factory); err == nil {
		t.Fatal("duplicate provider registration must be rejected")
	}
}

func TestRegistryRejectsUnknownProvider(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Create("missing", ProviderConfig{Model: "model-x", APIKey: "secret"}); err == nil {
		t.Fatal("unknown provider must be rejected")
	}
	if registry.Has("missing") {
		t.Fatal("unknown provider must not be reported as registered")
	}
}

func TestNormalizeModelIdentifier(t *testing.T) {
	valid, err := NormalizeModelIdentifier("  provider-model_1.2  ")
	if err != nil {
		t.Fatalf("valid model rejected: %v", err)
	}
	if valid != "provider-model_1.2" {
		t.Fatalf("normalized model = %q", valid)
	}

	invalid := []string{
		"",
		"   ",
		"bad\nmodel",
		string([]byte{0xff}),
		strings.Repeat("m", maxModelIdentifierBytes+1),
	}
	for _, value := range invalid {
		if _, err := NormalizeModelIdentifier(value); err == nil {
			t.Fatalf("model %q must be rejected", value)
		}
	}
}

func TestDefaultRegistryContainsConfiguredProviders(t *testing.T) {
	registry, err := NewDefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	if !registry.Has(ProviderOpenAI) {
		t.Fatal("default registry must contain OpenAI")
	}
	if !registry.Has(ProviderGemini) {
		t.Fatal("default registry must contain Gemini")
	}

	openAIProviderValue, err := registry.Create(ProviderOpenAI, ProviderConfig{
		Model:  "gpt-configurable-test",
		APIKey: "test-secret",
	})
	if err != nil {
		t.Fatalf("create configured OpenAI provider: %v", err)
	}
	openAI, ok := openAIProviderValue.(*openAIProvider)
	if !ok {
		t.Fatalf("unexpected OpenAI provider type %T", openAIProviderValue)
	}
	if openAI.model != "gpt-configurable-test" {
		t.Fatalf("OpenAI provider model = %q", openAI.model)
	}

	geminiProviderValue, err := registry.Create(ProviderGemini, ProviderConfig{
		Model:  "gemini-configurable-test",
		APIKey: "gemini-test-secret",
	})
	if err != nil {
		t.Fatalf("create configured Gemini provider: %v", err)
	}
	gemini, ok := geminiProviderValue.(*geminiProvider)
	if !ok {
		t.Fatalf("unexpected Gemini provider type %T", geminiProviderValue)
	}
	if gemini.model != "gemini-configurable-test" {
		t.Fatalf("Gemini provider model = %q", gemini.model)
	}
}
