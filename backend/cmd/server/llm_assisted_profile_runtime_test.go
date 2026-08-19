package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
)

func TestBuildLLMAssistedProfileRegistryConstructsDefaultProfilesWithoutSecretExposure(t *testing.T) {
	tests := []config.Config{
		{
			LLMAssistanceProvider: llmassist.ProviderOpenAI,
			LLMAssistanceModel:    "openai-runtime-model",
			LLMAssistanceTimeout:  time.Second,
			OpenAIAPIKey:          "openai-profile-test-secret",
		},
		{
			LLMAssistanceProvider: llmassist.ProviderGemini,
			LLMAssistanceModel:    "gemini-runtime-model",
			LLMAssistanceTimeout:  time.Second,
			GeminiAPIKey:          "gemini-profile-test-secret",
		},
		{
			LLMAssistanceProvider: llmassist.ProviderDeepSeek,
			LLMAssistanceModel:    "deepseek-runtime-model",
			LLMAssistanceTimeout:  time.Second,
			DeepSeekAPIKey:        "deepseek-profile-test-secret",
		},
	}

	for _, cfg := range tests {
		registry, err := buildLLMAssistedProfileRegistry(cfg)
		if err != nil {
			t.Fatalf("build %s profile registry: %v", cfg.LLMAssistanceProvider, err)
		}
		profiles := registry.PublicProfiles()
		if len(profiles) != 1 {
			t.Fatalf("%s public profile count=%d", cfg.LLMAssistanceProvider, len(profiles))
		}
		if profiles[0].ID != defaultLLMAssistedProfileID || profiles[0].Availability != llmassist.ProfileAvailabilityAvailable {
			t.Fatalf("%s public profile=%+v", cfg.LLMAssistanceProvider, profiles[0])
		}
		resolved, err := registry.Resolve(defaultLLMAssistedProfileID)
		if err != nil {
			t.Fatalf("resolve %s default profile: %v", cfg.LLMAssistanceProvider, err)
		}
		if resolved.Provider != cfg.LLMAssistanceProvider || resolved.Model != cfg.LLMAssistanceModel {
			t.Fatalf("%s resolved routing=%+v", cfg.LLMAssistanceProvider, resolved)
		}

		encoded, err := json.Marshal(profiles)
		if err != nil {
			t.Fatalf("marshal %s public profile: %v", cfg.LLMAssistanceProvider, err)
		}
		payload := string(encoded)
		credential, err := cfg.LLMAssistanceCredential()
		if err != nil {
			t.Fatalf("resolve %s credential: %v", cfg.LLMAssistanceProvider, err)
		}
		if strings.Contains(payload, credential) {
			t.Fatalf("%s public profile exposed provider credential", cfg.LLMAssistanceProvider)
		}
	}
}
