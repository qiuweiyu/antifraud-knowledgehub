package main

import (
	"fmt"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/browserauth"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/analysis"
	"github.com/gin-gonic/gin"
)

func registerConfiguredBrowserAssistedAnalysisRoutes(
	v1 *gin.RouterGroup,
	cfg config.Config,
	browserCfg config.BrowserSessionConfig,
	store *database.Store,
	sessionHandler *browserauth.SessionHTTPHandler,
) {
	profileRegistry, err := buildLLMAssistedProfileRegistry(cfg)
	if err != nil {
		panic("validated browser assisted-analysis profile runtime could not be constructed")
	}
	rateConfig := browserauth.BrowserAssistedRateConfig{
		PrincipalLimit: browserCfg.AssistedPrincipalLimit,
		GlobalLimit:    browserCfg.AssistedGlobalLimit,
		Window:         browserCfg.AssistedRateWindow,
	}
	if err := browserauth.ValidateBrowserAssistedRateConfig(rateConfig); err != nil {
		panic(fmt.Sprintf("validated browser assisted-analysis rate configuration became invalid: %v", err))
	}
	registerBrowserAssistedAnalysisRoutes(
		v1,
		store,
		sessionHandler,
		profileRegistry,
		browserauth.RedisBrowserAssistedRateBackend{Client: store.Redis},
		rateConfig,
	)
}

func registerBrowserAssistedAnalysisRoutes(
	v1 *gin.RouterGroup,
	store *database.Store,
	sessionHandler *browserauth.SessionHTTPHandler,
	profileRegistry *llmassist.ProfileRegistry,
	rateBackend browserauth.BrowserAssistedRateBackend,
	rateConfig browserauth.BrowserAssistedRateConfig,
) {
	if v1 == nil || store == nil || sessionHandler == nil || profileRegistry == nil {
		panic("browser assisted-analysis route dependencies are required")
	}
	v1.GET(
		"/browser/analysis/assisted/profiles",
		sessionHandler.RequireSession(false),
		analysis.BrowserAssistedProfilesHandler(profileRegistry),
	)
	v1.POST(
		"/browser/analysis/assisted",
		sessionHandler.RequireSession(true),
		browserauth.BrowserAssistedCostLimit(rateBackend, rateConfig),
		analysis.BrowserAssistedAnalysisHandler(store.DB, profileRegistry),
	)
}
