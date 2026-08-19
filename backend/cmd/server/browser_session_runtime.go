package main

import (
	"fmt"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/browserauth"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/gin-gonic/gin"
)

func registerConfiguredBrowserSessionRoutes(v1 *gin.RouterGroup, cfg config.BrowserSessionConfig, production bool, store *database.Store) {
	if v1 == nil || store == nil {
		panic("browser session runtime dependencies are required")
	}
	origin, err := browserauth.ValidateCanonicalOrigin(cfg.Origin, production)
	if err != nil {
		panic(fmt.Sprintf("validated browser session origin became invalid: %v", err))
	}
	registry, err := browserauth.ParseGrantRegistryJSON(cfg.AccessGrantsJSON)
	if err != nil {
		panic(fmt.Sprintf("validated browser access grant registry became invalid: %v", err))
	}
	trustedProxies, err := browserauth.ParseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		panic(fmt.Sprintf("validated browser trusted proxy configuration became invalid: %v", err))
	}
	rateConfig := browserauth.ExchangeRateConfig{
		SourceLimit: cfg.ExchangeSourceLimit,
		GlobalLimit: cfg.ExchangeGlobalLimit,
		Window:      cfg.ExchangeRateWindow,
	}
	if err := browserauth.ValidateExchangeRateConfig(rateConfig); err != nil {
		panic(fmt.Sprintf("validated browser exchange rate configuration became invalid: %v", err))
	}
	handler := &browserauth.SessionHTTPHandler{
		Registry: registry,
		Sessions: browserauth.SessionStore{Client: store.Redis},
		Limiter:  browserauth.RedisExchangeRateLimiter{Client: store.Redis},
		Config: browserauth.SessionHTTPConfig{
			Origin:         origin,
			Production:     production,
			TrustedProxies: trustedProxies,
			ExchangeRate:   rateConfig,
		},
	}
	handler.Register(v1)
}
