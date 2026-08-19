package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

func clearBrowserSessionEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BROWSER_ASSISTED_ENABLED",
		"BROWSER_ASSISTED_ANALYSIS_ENABLED",
		"BROWSER_ASSISTED_ORIGIN",
		"BROWSER_ASSISTED_ACCESS_GRANTS_JSON",
		"BROWSER_ASSISTED_TRUSTED_PROXY_CIDRS",
		"BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT",
		"BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT",
		"BROWSER_SESSION_EXCHANGE_RATE_WINDOW",
		"BROWSER_ASSISTED_PRINCIPAL_LIMIT",
		"BROWSER_ASSISTED_GLOBAL_LIMIT",
		"BROWSER_ASSISTED_RATE_WINDOW",
	} {
		t.Setenv(key, "")
	}
}

func validBrowserGrantJSON() string {
	raw := "afkhb1_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	digest := sha256.Sum256([]byte(raw))
	return fmt.Sprintf(`[{"principal_id":"beta-user","principal_generation":1,"display_label":"Beta User","grant_sha256":"%s","enabled":true}]`, hex.EncodeToString(digest[:]))
}

func setValidBrowserSessionEnv(t *testing.T) {
	t.Helper()
	clearBrowserSessionEnv(t)
	t.Setenv("BROWSER_ASSISTED_ENABLED", "true")
	t.Setenv("BROWSER_ASSISTED_ORIGIN", "http://127.0.0.1:5173")
	t.Setenv("BROWSER_ASSISTED_ACCESS_GRANTS_JSON", validBrowserGrantJSON())
	t.Setenv("BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT", "5")
	t.Setenv("BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT", "50")
	t.Setenv("BROWSER_SESSION_EXCHANGE_RATE_WINDOW", "10m")
}

func setValidBrowserAssistedAnalysisEnv(t *testing.T) {
	t.Helper()
	setValidBrowserSessionEnv(t)
	t.Setenv("BROWSER_ASSISTED_ANALYSIS_ENABLED", "true")
	t.Setenv("BROWSER_ASSISTED_PRINCIPAL_LIMIT", "5")
	t.Setenv("BROWSER_ASSISTED_GLOBAL_LIMIT", "25")
	t.Setenv("BROWSER_ASSISTED_RATE_WINDOW", "1m")
}

func TestBrowserSessionConfigDefaultsDisabled(t *testing.T) {
	clearBrowserSessionEnv(t)
	cfg := LoadBrowserSessionConfig()
	if cfg.Enabled || cfg.AnalysisEnabled {
		t.Fatal("browser assisted bridge and analysis must default disabled")
	}
	if cfg.ExchangeSourceLimit != 5 || cfg.ExchangeGlobalLimit != 50 || cfg.ExchangeRateWindow != 10*time.Minute {
		t.Fatalf("unexpected exchange defaults: %+v", cfg)
	}
	if cfg.AssistedPrincipalLimit != 5 || cfg.AssistedGlobalLimit != 25 || cfg.AssistedRateWindow != time.Minute {
		t.Fatalf("unexpected assisted defaults: %+v", cfg)
	}
	if err := cfg.Validate(false); err != nil {
		t.Fatalf("disabled config should validate: %v", err)
	}
}

func TestBrowserSessionConfigValidDevelopmentConfiguration(t *testing.T) {
	setValidBrowserSessionEnv(t)
	cfg := LoadBrowserSessionConfig()
	if err := cfg.Validate(false); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestBrowserAssistedAnalysisConfigurationAndPrerequisites(t *testing.T) {
	setValidBrowserAssistedAnalysisEnv(t)
	cfg := LoadBrowserSessionConfig()
	if err := cfg.Validate(false); err != nil {
		t.Fatalf("valid assisted config rejected: %v", err)
	}

	clearLLMAssistanceEnv(t)
	if err := cfg.ValidateAssistedAnalysisPrerequisites(Load()); err == nil || !strings.Contains(err.Error(), "LLM_ASSISTANCE_ENABLED") {
		t.Fatalf("browser assisted analysis must require LLM assistance, got %v", err)
	}
	setValidOpenAIAssistanceEnv(t)
	if err := cfg.ValidateAssistedAnalysisPrerequisites(Load()); err != nil {
		t.Fatalf("valid assisted prerequisite rejected: %v", err)
	}
}

func TestBrowserAssistedAnalysisRequiresSessionBridge(t *testing.T) {
	clearBrowserSessionEnv(t)
	t.Setenv("BROWSER_ASSISTED_ANALYSIS_ENABLED", "true")
	err := LoadBrowserSessionConfig().Validate(false)
	if err == nil || !strings.Contains(err.Error(), "BROWSER_ASSISTED_ENABLED") {
		t.Fatalf("analysis without session bridge must fail, got %v", err)
	}
}

func TestBrowserSessionConfigRejectsProductionHTTPAndInvalidControls(t *testing.T) {
	setValidBrowserSessionEnv(t)
	if err := LoadBrowserSessionConfig().Validate(true); err == nil || !strings.Contains(err.Error(), "BROWSER_ASSISTED_ORIGIN") {
		t.Fatalf("production HTTP origin should fail, got %v", err)
	}

	tests := []struct{ key, value, want string }{
		{"BROWSER_ASSISTED_ACCESS_GRANTS_JSON", `[{"bad":true}]`, "BROWSER_ASSISTED_ACCESS_GRANTS_JSON"},
		{"BROWSER_ASSISTED_TRUSTED_PROXY_CIDRS", "not-a-cidr", "BROWSER_ASSISTED_TRUSTED_PROXY_CIDRS"},
		{"BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT", "0", "BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT"},
		{"BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT", "4", "BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT"},
		{"BROWSER_SESSION_EXCHANGE_RATE_WINDOW", "0s", "BROWSER_SESSION_EXCHANGE_RATE_WINDOW"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			setValidBrowserSessionEnv(t)
			t.Setenv(tc.key, tc.value)
			err := LoadBrowserSessionConfig().Validate(false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s failure, got %v", tc.want, err)
			}
		})
	}
}

func TestBrowserAssistedAnalysisRejectsInvalidCostControls(t *testing.T) {
	tests := []struct{ key, value, want string }{
		{"BROWSER_ASSISTED_PRINCIPAL_LIMIT", "0", "BROWSER_ASSISTED_PRINCIPAL_LIMIT"},
		{"BROWSER_ASSISTED_GLOBAL_LIMIT", "4", "BROWSER_ASSISTED_GLOBAL_LIMIT"},
		{"BROWSER_ASSISTED_RATE_WINDOW", "0s", "BROWSER_ASSISTED_RATE_WINDOW"},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			setValidBrowserAssistedAnalysisEnv(t)
			t.Setenv(tc.key, tc.value)
			err := LoadBrowserSessionConfig().Validate(false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s failure, got %v", tc.want, err)
			}
		})
	}
}
