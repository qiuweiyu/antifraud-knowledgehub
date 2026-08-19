package main

import (
	"fmt"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/middleware"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/analysis"
	"github.com/gin-gonic/gin"
)

const (
	defaultLLMAssistedProfileID         = "default"
	defaultLLMAssistedProfileDisclosure = "Submitted text is sent to the configured third-party AI provider for supplemental analysis."
)

func buildLLMAssistedProfileRegistry(cfg config.Config) (*llmassist.ProfileRegistry, error) {
	credential, err := cfg.LLMAssistanceCredential()
	if err != nil {
		return nil, err
	}
	providerRegistry, err := llmassist.NewDefaultRegistry()
	if err != nil {
		return nil, fmt.Errorf("initialize LLM provider registry: %w", err)
	}
	profileRegistry, err := llmassist.NewProfileRegistry(providerRegistry, []llmassist.ProfileDefinition{
		{
			ID:                  defaultLLMAssistedProfileID,
			DisplayName:         "Default AI Assistance",
			Provider:            cfg.LLMAssistanceProvider,
			ProviderDisplayName: llmProviderDisplayName(cfg.LLMAssistanceProvider),
			Model:               cfg.LLMAssistanceModel,
			ModelDisplayName:    cfg.LLMAssistanceModel,
			APIKey:              credential,
			Timeout:             cfg.LLMAssistanceTimeout,
			Enabled:             true,
			Disclosure:          defaultLLMAssistedProfileDisclosure,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("construct assisted AI profile registry: %w", err)
	}
	return profileRegistry, nil
}

func buildLLMAssistedAnalysisService(cfg config.Config) (analysis.AssistanceService, error) {
	profileRegistry, err := buildLLMAssistedProfileRegistry(cfg)
	if err != nil {
		return nil, err
	}
	profile, err := profileRegistry.Resolve(defaultLLMAssistedProfileID)
	if err != nil {
		return nil, fmt.Errorf("resolve default assisted AI profile: %w", err)
	}
	return profile.Service, nil
}

func llmProviderDisplayName(provider string) string {
	switch provider {
	case llmassist.ProviderOpenAI:
		return "OpenAI"
	case llmassist.ProviderGemini:
		return "Google Gemini"
	case llmassist.ProviderDeepSeek:
		return "DeepSeek"
	default:
		return "Configured AI Provider"
	}
}

func registerConfiguredLLMAssistedAnalysisRoute(v1 *gin.RouterGroup, cfg config.Config, store *database.Store) {
	service, err := buildLLMAssistedAnalysisService(cfg)
	if err != nil {
		panic("validated assisted-analysis runtime could not be constructed")
	}
	registerLLMAssistedAnalysisRoute(
		v1,
		cfg,
		store,
		service,
		middleware.RedisLLMAssistedRateBackend{Client: store.Redis},
	)
}

func registerLLMAssistedAnalysisRoute(
	v1 *gin.RouterGroup,
	cfg config.Config,
	store *database.Store,
	service analysis.AssistanceService,
	rateBackend middleware.LLMAssistedRateBackend,
) {
	if v1 == nil || store == nil {
		panic("assisted-analysis route dependencies are required")
	}
	v1.POST(
		"/analysis/assisted",
		middleware.LLMAssistedAnalysisAuthorization(cfg.LLMAssistedAnalysisToken),
		middleware.LLMAssistedAnalysisRateLimit(
			rateBackend,
			cfg.LLMAssistedAnalysisToken,
			middleware.LLMAssistedRateConfig{
				CredentialLimit: cfg.LLMAssistedAnalysisCredentialLimit,
				GlobalLimit:     cfg.LLMAssistedAnalysisGlobalLimit,
				Window:          cfg.LLMAssistedAnalysisRateWindow,
			},
		),
		analysis.AssistedAnalysisHandler(store.DB, service, cfg.LLMAssistanceProvider, cfg.LLMAssistanceModel),
	)
}
