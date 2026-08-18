package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/middleware"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/analysis"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const serverAssistedToken = "abcdef0123456789abcdef0123456789"

type serverAssistedService struct {
	calls   int
	inputs  []llmassist.Input
	outcome llmassist.Outcome
}

func (s *serverAssistedService) Assist(_ context.Context, input llmassist.Input) llmassist.Outcome {
	s.calls++
	s.inputs = append(s.inputs, input)
	return s.outcome
}

type serverAssistedRateBackend struct {
	calls   int
	allowed bool
	err     error
}

func (b *serverAssistedRateBackend) Allow(_ context.Context, _ string, _ middleware.LLMAssistedRateConfig) (bool, error) {
	b.calls++
	return b.allowed, b.err
}

func newAssistedRouterTestStore(t *testing.T) *database.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open assisted route test DB: %v", err)
	}
	if err := db.AutoMigrate(&database.RiskRule{}, &database.AnalysisRecord{}); err != nil {
		t.Fatalf("migrate assisted route test DB: %v", err)
	}
	if err := db.Create(&database.RiskRule{
		Code:           "server_assisted_safe_account",
		Name:           "安全账户风险",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "transfer now",
		Weight:         45,
		Severity:       "high",
		Enabled:        true,
		Explanation:    "Synthetic server assisted-analysis rule.",
		Recommendation: "Verify through an independent official channel.",
	}).Error; err != nil {
		t.Fatalf("seed assisted route test rule: %v", err)
	}
	return &database.Store{DB: db}
}

func serverAssistedConfig() config.Config {
	return config.Config{
		LLMAssistedAnalysisHTTPEnabled:     true,
		LLMAssistedAnalysisToken:           serverAssistedToken,
		LLMAssistedAnalysisCredentialLimit: 10,
		LLMAssistedAnalysisGlobalLimit:     50,
		LLMAssistedAnalysisRateWindow:      time.Minute,
		LLMAssistanceEnabled:               true,
		LLMAssistanceProvider:              llmassist.ProviderOpenAI,
		LLMAssistanceModel:                 "server-configured-model",
		LLMAssistanceTimeout:               time.Second,
		OpenAIAPIKey:                       "server-provider-secret",
	}
}

func newAssistedRouteTestRouter(t *testing.T, service analysis.AssistanceService, backend middleware.LLMAssistedRateBackend) (*gin.Engine, config.Config) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	cfg := serverAssistedConfig()
	registerLLMAssistedAnalysisRoute(v1, cfg, newAssistedRouterTestStore(t), service, backend)
	return r, cfg
}

func TestNewRouterDoesNotRegisterAssistedRouteWhenDisabled(t *testing.T) {
	cfg := config.Config{}
	r := newRouter(cfg, zap.NewNop(), newAssistedRouterTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/assisted", strings.NewReader(`{"text":"transfer now"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+serverAssistedToken)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled assisted route status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAssistedRouteAuthorizationRunsBeforeRateLimit(t *testing.T) {
	service := &serverAssistedService{outcome: llmassist.Outcome{Status: llmassist.StatusAvailable}}
	backend := &serverAssistedRateBackend{allowed: true}
	r, _ := newAssistedRouteTestRouter(t, service, backend)

	for _, header := range []string{"", "Basic bad", "Bearer wrong-token"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/assisted", strings.NewReader(`{"text":"transfer now"}`))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("header=%q status=%d body=%s", header, recorder.Code, recorder.Body.String())
		}
	}
	if backend.calls != 0 {
		t.Fatalf("unauthorized requests must perform zero rate-backend calls, got %d", backend.calls)
	}
	if service.calls != 0 {
		t.Fatalf("unauthorized requests must perform zero assistance calls, got %d", service.calls)
	}
}

func TestAssistedRouteRateLimitRunsBeforeBodyParsing(t *testing.T) {
	service := &serverAssistedService{outcome: llmassist.Outcome{Status: llmassist.StatusAvailable}}
	backend := &serverAssistedRateBackend{allowed: false}
	r, cfg := newAssistedRouteTestRouter(t, service, backend)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/assisted", strings.NewReader("not-json"))
	req.Header.Set("Authorization", "Bearer "+cfg.LLMAssistedAnalysisToken)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-denied request status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if backend.calls != 1 {
		t.Fatalf("rate backend calls=%d", backend.calls)
	}
	if service.calls != 0 {
		t.Fatalf("rate-denied request must perform zero assistance calls, got %d", service.calls)
	}
}

func TestAssistedRouteRateBackendFailureFailsClosed(t *testing.T) {
	service := &serverAssistedService{outcome: llmassist.Outcome{Status: llmassist.StatusAvailable}}
	backend := &serverAssistedRateBackend{err: errors.New("redis unavailable")}
	r, cfg := newAssistedRouteTestRouter(t, service, backend)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/assisted", strings.NewReader(`{"text":"transfer now"}`))
	req.Header.Set("Authorization", "Bearer "+cfg.LLMAssistedAnalysisToken)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("rate-backend failure status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.calls != 0 {
		t.Fatalf("rate-backend failure must perform zero assistance calls, got %d", service.calls)
	}
	if strings.Contains(recorder.Body.String(), cfg.LLMAssistedAnalysisToken) || strings.Contains(recorder.Body.String(), cfg.OpenAIAPIKey) {
		t.Fatal("rate failure reflected a secret")
	}
}

func TestAssistedRouteSuccessUsesServerProviderModel(t *testing.T) {
	service := &serverAssistedService{outcome: llmassist.Outcome{
		Status: llmassist.StatusAvailable,
		Assistance: llmassist.Assistance{
			Summary:      "supplemental",
			Observations: []string{"observe"},
			Limitations:  []string{"limited"},
		},
	}}
	backend := &serverAssistedRateBackend{allowed: true}
	r, cfg := newAssistedRouteTestRouter(t, service, backend)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/assisted", strings.NewReader(`{"text":"transfer now"}`))
	req.Header.Set("Authorization", "Bearer "+cfg.LLMAssistedAnalysisToken)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if backend.calls != 1 || service.calls != 1 {
		t.Fatalf("backend/service calls=%d/%d", backend.calls, service.calls)
	}
	if len(service.inputs) != 1 || service.inputs[0].RuleResult.RiskScore <= 0 || len(service.inputs[0].RuleResult.MatchedRules) != 1 {
		t.Fatalf("service must receive deterministic result first: %+v", service.inputs)
	}
	if !strings.Contains(recorder.Body.String(), `"provider":"openai"`) || !strings.Contains(recorder.Body.String(), `"model":"server-configured-model"`) {
		t.Fatalf("server provider/model metadata missing: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), cfg.LLMAssistedAnalysisToken) || strings.Contains(recorder.Body.String(), cfg.OpenAIAPIKey) {
		t.Fatal("successful response reflected a secret")
	}
}

func TestBuildLLMAssistedAnalysisServiceConstructsRegisteredProvidersWithoutNetwork(t *testing.T) {
	tests := []config.Config{
		{
			LLMAssistanceProvider: llmassist.ProviderOpenAI,
			LLMAssistanceModel:    "openai-runtime-model",
			LLMAssistanceTimeout:  time.Second,
			OpenAIAPIKey:          "openai-test-secret",
		},
		{
			LLMAssistanceProvider: llmassist.ProviderGemini,
			LLMAssistanceModel:    "gemini-runtime-model",
			LLMAssistanceTimeout:  time.Second,
			GeminiAPIKey:          "gemini-test-secret",
		},
		{
			LLMAssistanceProvider: llmassist.ProviderDeepSeek,
			LLMAssistanceModel:    "deepseek-runtime-model",
			LLMAssistanceTimeout:  time.Second,
			DeepSeekAPIKey:        "deepseek-test-secret",
		},
	}
	for _, cfg := range tests {
		service, err := buildLLMAssistedAnalysisService(cfg)
		if err != nil {
			t.Fatalf("build %s service: %v", cfg.LLMAssistanceProvider, err)
		}
		if service == nil {
			t.Fatalf("build %s returned nil service", cfg.LLMAssistanceProvider)
		}
	}
}
