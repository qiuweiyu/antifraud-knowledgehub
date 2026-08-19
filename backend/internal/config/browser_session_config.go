package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/browserauth"
)

const (
	defaultBrowserSessionExchangeSourceLimit int64 = 5
	defaultBrowserSessionExchangeGlobalLimit int64 = 50
	defaultBrowserSessionExchangeRateWindow        = 10 * time.Minute
	defaultBrowserAssistedPrincipalLimit      int64 = 5
	defaultBrowserAssistedGlobalLimit         int64 = 25
	defaultBrowserAssistedRateWindow                = time.Minute
)

type BrowserSessionConfig struct {
	Enabled                       bool
	AnalysisEnabled               bool
	Origin                        string
	AccessGrantsJSON              string
	TrustedProxyCIDRs             []string
	ExchangeSourceLimit           int64
	ExchangeGlobalLimit           int64
	ExchangeRateWindow            time.Duration
	AssistedPrincipalLimit        int64
	AssistedGlobalLimit           int64
	AssistedRateWindow            time.Duration
}

func LoadBrowserSessionConfig() BrowserSessionConfig {
	return BrowserSessionConfig{
		Enabled:                boolEnv("BROWSER_ASSISTED_ENABLED"),
		AnalysisEnabled:        boolEnv("BROWSER_ASSISTED_ANALYSIS_ENABLED"),
		Origin:                 strings.TrimSpace(os.Getenv("BROWSER_ASSISTED_ORIGIN")),
		AccessGrantsJSON:       os.Getenv("BROWSER_ASSISTED_ACCESS_GRANTS_JSON"),
		TrustedProxyCIDRs:      splitEnv("BROWSER_ASSISTED_TRUSTED_PROXY_CIDRS", []string{}),
		ExchangeSourceLimit:    int64Env("BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT", defaultBrowserSessionExchangeSourceLimit),
		ExchangeGlobalLimit:    int64Env("BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT", defaultBrowserSessionExchangeGlobalLimit),
		ExchangeRateWindow:     durationEnv("BROWSER_SESSION_EXCHANGE_RATE_WINDOW", defaultBrowserSessionExchangeRateWindow),
		AssistedPrincipalLimit: int64Env("BROWSER_ASSISTED_PRINCIPAL_LIMIT", defaultBrowserAssistedPrincipalLimit),
		AssistedGlobalLimit:    int64Env("BROWSER_ASSISTED_GLOBAL_LIMIT", defaultBrowserAssistedGlobalLimit),
		AssistedRateWindow:     durationEnv("BROWSER_ASSISTED_RATE_WINDOW", defaultBrowserAssistedRateWindow),
	}
}

func (c BrowserSessionConfig) Validate(production bool) error {
	if !c.Enabled {
		if c.AnalysisEnabled {
			return fmt.Errorf("BROWSER_ASSISTED_ENABLED must be true when browser assisted-analysis is enabled")
		}
		return nil
	}
	if _, err := browserauth.ValidateCanonicalOrigin(c.Origin, production); err != nil {
		return fmt.Errorf("BROWSER_ASSISTED_ORIGIN is invalid: %w", err)
	}
	if _, err := browserauth.ParseGrantRegistryJSON(c.AccessGrantsJSON); err != nil {
		return fmt.Errorf("BROWSER_ASSISTED_ACCESS_GRANTS_JSON is invalid: %w", err)
	}
	if _, err := browserauth.ParseTrustedProxyCIDRs(c.TrustedProxyCIDRs); err != nil {
		return fmt.Errorf("BROWSER_ASSISTED_TRUSTED_PROXY_CIDRS is invalid: %w", err)
	}
	if c.ExchangeSourceLimit <= 0 {
		return fmt.Errorf("BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT must be positive")
	}
	if c.ExchangeGlobalLimit <= 0 || c.ExchangeGlobalLimit < c.ExchangeSourceLimit {
		return fmt.Errorf("BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT must be positive and at least the source limit")
	}
	if c.ExchangeRateWindow < time.Millisecond {
		return fmt.Errorf("BROWSER_SESSION_EXCHANGE_RATE_WINDOW must be at least 1ms")
	}
	if c.AnalysisEnabled {
		if c.AssistedPrincipalLimit <= 0 {
			return fmt.Errorf("BROWSER_ASSISTED_PRINCIPAL_LIMIT must be positive")
		}
		if c.AssistedGlobalLimit <= 0 || c.AssistedGlobalLimit < c.AssistedPrincipalLimit {
			return fmt.Errorf("BROWSER_ASSISTED_GLOBAL_LIMIT must be positive and at least the principal limit")
		}
		if c.AssistedRateWindow < time.Millisecond {
			return fmt.Errorf("BROWSER_ASSISTED_RATE_WINDOW must be at least 1ms")
		}
	}
	return nil
}

func (c BrowserSessionConfig) ValidateAssistedAnalysisPrerequisites(base Config) error {
	if !c.AnalysisEnabled {
		return nil
	}
	if !base.LLMAssistanceEnabled {
		return fmt.Errorf("LLM_ASSISTANCE_ENABLED must be true when browser assisted-analysis is enabled")
	}
	return nil
}
