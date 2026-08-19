package llmassist

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type profileTestProvider struct{}

func (profileTestProvider) Assist(_ context.Context, _ Input) (Assistance, error) {
	return Assistance{Summary: "test assistance"}, nil
}

func newProfileTestProviderRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register("test", func(cfg ProviderConfig) (Provider, error) {
		if cfg.APIKey != "server-secret" {
			return nil, errors.New("bad credential")
		}
		if cfg.Model != "internal-model-v1" {
			return nil, errors.New("bad model")
		}
		return profileTestProvider{}, nil
	}); err != nil {
		t.Fatalf("register test provider: %v", err)
	}
	return registry
}

func enabledProfileDefinition() ProfileDefinition {
	return ProfileDefinition{
		ID:                  "balanced-analysis",
		DisplayName:         "Balanced AI Analysis",
		Provider:            "test",
		ProviderDisplayName: "Test Provider",
		Model:               "internal-model-v1",
		ModelDisplayName:    "Balanced Model",
		APIKey:              "server-secret",
		Timeout:             time.Second,
		Enabled:             true,
		Disclosure:          "Submitted text is sent to the configured third-party AI provider.",
	}
}

func TestProfileRegistryRejectsInvalidAndDuplicateIDs(t *testing.T) {
	registry := newProfileTestProviderRegistry(t)
	for _, id := range []string{"", " leading", "trailing ", "UPPER", "-leading", "bad/id", strings.Repeat("a", maxProfileIDBytes+1)} {
		definition := enabledProfileDefinition()
		definition.ID = id
		if _, err := NewProfileRegistry(registry, []ProfileDefinition{definition}); err == nil {
			t.Fatalf("profile ID %q should fail", id)
		}
	}

	first := enabledProfileDefinition()
	second := enabledProfileDefinition()
	second.DisplayName = "Duplicate"
	if _, err := NewProfileRegistry(registry, []ProfileDefinition{first, second}); err == nil {
		t.Fatal("duplicate profile IDs should fail")
	}
}

func TestProfileRegistryBoundsProfileCount(t *testing.T) {
	definitions := make([]ProfileDefinition, maxAssistedProfiles+1)
	if _, err := NewProfileRegistry(newProfileTestProviderRegistry(t), definitions); err == nil {
		t.Fatal("profile count above limit should fail before construction")
	}
}

func TestProfileRegistryFailsClosedForInvalidEnabledRuntime(t *testing.T) {
	registry := newProfileTestProviderRegistry(t)

	badCredential := enabledProfileDefinition()
	badCredential.APIKey = "wrong-secret"
	if _, err := NewProfileRegistry(registry, []ProfileDefinition{badCredential}); err == nil {
		t.Fatal("invalid enabled provider credential should fail")
	}

	badModel := enabledProfileDefinition()
	badModel.Model = "wrong-model"
	if _, err := NewProfileRegistry(registry, []ProfileDefinition{badModel}); err == nil {
		t.Fatal("invalid enabled model should fail")
	}

	unknownProvider := enabledProfileDefinition()
	unknownProvider.Provider = "unknown"
	if _, err := NewProfileRegistry(registry, []ProfileDefinition{unknownProvider}); err == nil {
		t.Fatal("unknown enabled provider should fail")
	}

	badTimeout := enabledProfileDefinition()
	badTimeout.Timeout = 0
	if _, err := NewProfileRegistry(registry, []ProfileDefinition{badTimeout}); err == nil {
		t.Fatal("invalid enabled timeout should fail")
	}
}

func TestProfileRegistryUnknownAndDisabledProfilesCannotExecute(t *testing.T) {
	definition := enabledProfileDefinition()
	definition.Enabled = false
	definition.APIKey = ""
	registry, err := NewProfileRegistry(newProfileTestProviderRegistry(t), []ProfileDefinition{definition})
	if err != nil {
		t.Fatalf("construct disabled profile registry: %v", err)
	}
	if _, err := registry.Resolve("balanced-analysis"); err == nil {
		t.Fatal("disabled profile should not resolve")
	}
	if _, err := registry.Resolve("missing-profile"); err == nil {
		t.Fatal("unknown profile should not resolve")
	}
	public := registry.PublicProfiles()
	if len(public) != 1 || public[0].Availability != ProfileAvailabilityDisabled {
		t.Fatalf("disabled public metadata=%+v", public)
	}
}

func TestProfileRegistryPublicMetadataIsBoundedSecretFreeAndDefensive(t *testing.T) {
	definition := enabledProfileDefinition()
	registry, err := NewProfileRegistry(newProfileTestProviderRegistry(t), []ProfileDefinition{definition})
	if err != nil {
		t.Fatalf("construct registry: %v", err)
	}

	metadata := registry.PublicProfiles()
	if len(metadata) != 1 {
		t.Fatalf("metadata count=%d", len(metadata))
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	payload := string(encoded)
	for _, forbidden := range []string{
		definition.APIKey,
		definition.Model,
		"authorization",
		"base_url",
		"endpoint",
		"system_prompt",
	} {
		if strings.Contains(strings.ToLower(payload), strings.ToLower(forbidden)) {
			t.Fatalf("public metadata leaked forbidden value %q: %s", forbidden, payload)
		}
	}
	if !strings.Contains(payload, definition.ModelDisplayName) || !strings.Contains(payload, definition.ProviderDisplayName) {
		t.Fatalf("public display metadata missing: %s", payload)
	}

	metadata[0].DisplayName = "mutated by caller"
	again := registry.PublicProfiles()
	if again[0].DisplayName != definition.DisplayName {
		t.Fatalf("caller mutated registry metadata: %+v", again[0])
	}
}

func TestProfileRegistryResolveUsesServerOwnedProviderModelAndService(t *testing.T) {
	definition := enabledProfileDefinition()
	registry, err := NewProfileRegistry(newProfileTestProviderRegistry(t), []ProfileDefinition{definition})
	if err != nil {
		t.Fatalf("construct registry: %v", err)
	}
	resolved, err := registry.Resolve(definition.ID)
	if err != nil {
		t.Fatalf("resolve profile: %v", err)
	}
	if resolved.ID != definition.ID || resolved.Provider != definition.Provider || resolved.Model != definition.Model {
		t.Fatalf("resolved routing mismatch: %+v", resolved)
	}
	if resolved.Public.ModelDisplayName != definition.ModelDisplayName || resolved.Public.ProviderDisplayName != definition.ProviderDisplayName {
		t.Fatalf("resolved public metadata mismatch: %+v", resolved.Public)
	}
	outcome := resolved.Service.Assist(context.Background(), Input{})
	if outcome.Status != StatusAvailable || outcome.Assistance.Summary != "test assistance" {
		t.Fatalf("resolved service outcome=%+v", outcome)
	}
}
