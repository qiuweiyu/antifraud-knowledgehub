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

func buildLLMAssistedAnalysisService(cfg config.Config) (analysis.AssistanceService, error) {
	credential, err := cfg.LLMAssistanceCredential()
	if err != nil {
		return nil, err
	}
	registry, err := llmassist.NewDefaultRegistry()
	if err != nil {
		return nil, fmt.Errorf("initialize LLM provider registry: %w", err)
	}
	provider, err := registry.Create(cfg.LLMAssistanceProvider, llmassist.ProviderConfig{
		Model:  cfg.LLMAssistanceModel,
		APIKey: credential,
	})
	if err != nil {
		return nil, fmt.Errorf("construct configured LLM provider: %w", err)
	}
	service, err := llmassist.NewService(provider, cfg.LLMAssistanceTimeout)
	if err != nil {
		return nil, fmt.Errorf("construct LLM assistance service: %w", err)
	}
	return service, nil
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
