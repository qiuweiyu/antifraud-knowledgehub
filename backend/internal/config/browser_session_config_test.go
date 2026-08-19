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
		"BROWSER_ASSISTED_ORIGIN",
		"BROWSER_ASSISTED_ACCESS_GRANTS_JSON",
		"BROWSER_ASSISTED_TRUSTED_PROXY_CIDRS",
		"BROWSER_SESSION_EXCHANGE_SOURCE_LIMIT",
		"BROWSER_SESSION_EXCHANGE_GLOBAL_LIMIT",
		"BROWSER_SESSION_EXCHANGE_RATE_WINDOW",
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

func TestBrowserSessionConfigDefaultsDisabled(t *testing.T) {
	clearBrowserSessionEnv(t)
	cfg := LoadBrowserSessionConfig()
	if cfg.Enabled {
		t.Fatal("browser assisted bridge must default disabled")
	}
	if cfg.ExchangeSourceLimit != 5 || cfg.ExchangeGlobalLimit != 50 || cfg.ExchangeRateWindow != 10*time.Minute {
		t.Fatalf("unexpected defaults: %+v", cfg)
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
