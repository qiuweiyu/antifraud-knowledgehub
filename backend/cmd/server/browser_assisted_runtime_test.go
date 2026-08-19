package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/browserauth"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/glebarez/sqlite"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const runtimeBrowserCookieName = "__Host-afkh_browser_session"

type runtimeSessionBackend struct {
	principal browserauth.Principal
	validate  int
	err       error
}

func (b *runtimeSessionBackend) Create(context.Context, browserauth.Principal) (string, string, browserauth.SessionRecord, error) {
	return "", "", browserauth.SessionRecord{}, errors.New("not used")
}

func (b *runtimeSessionBackend) Validate(context.Context, string, *browserauth.GrantRegistry) (browserauth.SessionRecord, browserauth.Principal, error) {
	b.validate++
	if b.err != nil {
		return browserauth.SessionRecord{}, browserauth.Principal{}, b.err
	}
	return browserauth.SessionRecord{
		PrincipalID:         b.principal.ID,
		PrincipalGeneration: b.principal.Generation,
		IssuedUnix:          time.Now().Add(-time.Minute).Unix(),
		ExpiresUnix:         time.Now().Add(time.Hour).Unix(),
	}, b.principal, nil
}

func (b *runtimeSessionBackend) Delete(context.Context, string) error { return errors.New("not used") }

type runtimeCostBackend struct {
	allowed bool
	calls   int
}

func (b *runtimeCostBackend) Allow(_ context.Context, principalID string, _ browserauth.BrowserAssistedRateConfig) (bool, error) {
	b.calls++
	if principalID != "beta-user" {
		return false, errors.New("unexpected principal")
	}
	return b.allowed, nil
}

type runtimeProvider struct {
	calls int
	err   error
}

func (p *runtimeProvider) Assist(context.Context, llmassist.Input) (llmassist.Assistance, error) {
	p.calls++
	if p.err != nil {
		return llmassist.Assistance{}, p.err
	}
	return llmassist.Assistance{Summary: "Supplemental warning"}, nil
}

func runtimeGrantRegistry(t *testing.T) *browserauth.GrantRegistry {
	t.Helper()
	raw := "afkhb1_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	digest := sha256.Sum256([]byte(raw))
	registry, err := browserauth.NewGrantRegistry([]browserauth.GrantDefinition{{
		PrincipalID:         "beta-user",
		PrincipalGeneration: 1,
		DisplayLabel:        "Beta User",
		GrantSHA256:         hex.EncodeToString(digest[:]),
		Enabled:             true,
	}})
	if err != nil {
		t.Fatalf("grant registry: %v", err)
	}
	return registry
}

func runtimeProfileRegistry(t *testing.T, provider *runtimeProvider) *llmassist.ProfileRegistry {
	t.Helper()
	providers := llmassist.NewRegistry()
	if err := providers.Register("fake", func(llmassist.ProviderConfig) (llmassist.Provider, error) { return provider, nil }); err != nil {
		t.Fatalf("register fake provider: %v", err)
	}
	registry, err := llmassist.NewProfileRegistry(providers, []llmassist.ProfileDefinition{{
		ID:                  "default",
		DisplayName:         "Default AI Assistance",
		Provider:            "fake",
		ProviderDisplayName: "Fake Provider",
		Model:               "fake-model",
		ModelDisplayName:    "Fake Model",
		APIKey:              "fake-test-key-not-real",
		Timeout:             time.Second,
		Enabled:             true,
		Disclosure:          "Submitted text is transferred to a third-party AI provider.",
	}})
	if err != nil {
		t.Fatalf("profile registry: %v", err)
	}
	return registry
}

func runtimeAnalysisDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.RiskRule{}); err != nil {
		t.Fatalf("migrate risk rule: %v", err)
	}
	if err := db.Create(&database.RiskRule{
		Code: "safe_account_transfer", Name: "安全账户转账", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "安全账户", Weight: 30, Severity: "critical", Enabled: true,
		Explanation: "安全账户是典型诈骗话术。", Recommendation: "不要向陌生账户转账。",
	}).Error; err != nil {
		t.Fatalf("seed risk rule: %v", err)
	}
	return db
}

func runtimeBrowserRouter(t *testing.T, db *gorm.DB, sessions *runtimeSessionBackend, cost *runtimeCostBackend, provider *runtimeProvider) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sessionHandler := &browserauth.SessionHTTPHandler{
		Registry: runtimeGrantRegistry(t),
		Sessions: sessions,
		Config: browserauth.SessionHTTPConfig{
			Origin:     "https://antifraud.example",
			Production: true,
		},
	}
	router := gin.New()
	v1 := router.Group("/api/v1")
	registerBrowserAssistedAnalysisRoutes(
		v1,
		&database.Store{DB: db},
		sessionHandler,
		runtimeProfileRegistry(t, provider),
		cost,
		browserauth.BrowserAssistedRateConfig{PrincipalLimit: 2, GlobalLimit: 10, Window: time.Minute},
	)
	return router
}

func browserRuntimeRequest(router *gin.Engine, method, path, body, origin, csrf, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf != "" {
		req.Header.Set("X-AFKH-CSRF", csrf)
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: runtimeBrowserCookieName, Value: session})
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestBrowserAssistedRuntimeRejectedAuthPathsNeverReachCostOrProvider(t *testing.T) {
	rawSession := "opaque-runtime-session"
	tests := []struct {
		name       string
		origin     string
		csrf       string
		session    string
		wantStatus int
		wantValid  int
	}{
		{name: "unauthenticated", origin: "https://antifraud.example", session: "", wantStatus: http.StatusUnauthorized, wantValid: 0},
		{name: "origin rejected", origin: "https://evil.example", session: rawSession, wantStatus: http.StatusForbidden, wantValid: 0},
		{name: "csrf rejected", origin: "https://antifraud.example", csrf: "bad", session: rawSession, wantStatus: http.StatusForbidden, wantValid: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessions := &runtimeSessionBackend{principal: browserauth.Principal{ID: "beta-user", Generation: 1}}
			cost := &runtimeCostBackend{allowed: true}
			provider := &runtimeProvider{}
			router := runtimeBrowserRouter(t, nil, sessions, cost, provider)
			recorder := browserRuntimeRequest(router, http.MethodPost, "/api/v1/browser/analysis/assisted", `{"text":"安全账户","profile_id":"default"}`, tc.origin, tc.csrf, tc.session)
			if recorder.Code != tc.wantStatus || sessions.validate != tc.wantValid || cost.calls != 0 || provider.calls != 0 {
				t.Fatalf("status=%d validate=%d cost=%d provider=%d body=%s", recorder.Code, sessions.validate, cost.calls, provider.calls, recorder.Body.String())
			}
		})
	}
}

func TestBrowserAssistedRuntimeCostDenialAndInvalidProfilePerformZeroProviderCalls(t *testing.T) {
	rawSession := "opaque-runtime-session"
	csrf := browserauth.CSRFToken(rawSession)

	sessions := &runtimeSessionBackend{principal: browserauth.Principal{ID: "beta-user", Generation: 1}}
	cost := &runtimeCostBackend{allowed: false}
	provider := &runtimeProvider{}
	router := runtimeBrowserRouter(t, nil, sessions, cost, provider)
	recorder := browserRuntimeRequest(router, http.MethodPost, "/api/v1/browser/analysis/assisted", `{"text":"安全账户","profile_id":"default"}`, "https://antifraud.example", csrf, rawSession)
	if recorder.Code != http.StatusTooManyRequests || cost.calls != 1 || provider.calls != 0 {
		t.Fatalf("cost denial status=%d cost=%d provider=%d body=%s", recorder.Code, cost.calls, provider.calls, recorder.Body.String())
	}

	sessions = &runtimeSessionBackend{principal: browserauth.Principal{ID: "beta-user", Generation: 1}}
	cost = &runtimeCostBackend{allowed: true}
	provider = &runtimeProvider{}
	router = runtimeBrowserRouter(t, runtimeAnalysisDB(t), sessions, cost, provider)
	recorder = browserRuntimeRequest(router, http.MethodPost, "/api/v1/browser/analysis/assisted", `{"text":"安全账户","profile_id":"attacker-profile"}`, "https://antifraud.example", csrf, rawSession)
	if recorder.Code != http.StatusBadRequest || cost.calls != 1 || provider.calls != 0 {
		t.Fatalf("invalid profile status=%d cost=%d provider=%d body=%s", recorder.Code, cost.calls, provider.calls, recorder.Body.String())
	}
}

func TestBrowserAssistedRuntimeSuccessAndProfileMetadata(t *testing.T) {
	rawSession := "opaque-runtime-session"
	csrf := browserauth.CSRFToken(rawSession)
	sessions := &runtimeSessionBackend{principal: browserauth.Principal{ID: "beta-user", Generation: 1}}
	cost := &runtimeCostBackend{allowed: true}
	provider := &runtimeProvider{}
	router := runtimeBrowserRouter(t, runtimeAnalysisDB(t), sessions, cost, provider)

	profiles := browserRuntimeRequest(router, http.MethodGet, "/api/v1/browser/analysis/assisted/profiles", "", "", "", rawSession)
	if profiles.Code != http.StatusOK || provider.calls != 0 || !strings.Contains(profiles.Body.String(), "third-party AI provider") {
		t.Fatalf("profiles status=%d provider=%d body=%s", profiles.Code, provider.calls, profiles.Body.String())
	}

	recorder := browserRuntimeRequest(router, http.MethodPost, "/api/v1/browser/analysis/assisted", `{"text":"请转账到安全账户","profile_id":"default"}`, "https://antifraud.example", csrf, rawSession)
	if recorder.Code != http.StatusOK || cost.calls != 1 || provider.calls != 1 {
		t.Fatalf("success status=%d cost=%d provider=%d body=%s", recorder.Code, cost.calls, provider.calls, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "rule_result") || !strings.Contains(recorder.Body.String(), "Supplemental warning") {
		t.Fatalf("missing deterministic/supplemental response: %s", recorder.Body.String())
	}
}
