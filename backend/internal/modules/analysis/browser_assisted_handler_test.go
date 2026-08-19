package analysis

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const testBrowserProfileSecret = "super-secret-browser-profile-key"

type browserAssistedEnvelope struct {
	Success bool                            `json:"success"`
	Data    BrowserAssistedAnalysisResponse `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newBrowserProfileRegistry(t *testing.T, provider *countingAssistedProvider) *llmassist.ProfileRegistry {
	t.Helper()
	providers := llmassist.NewRegistry()
	if err := providers.Register("fake", func(llmassist.ProviderConfig) (llmassist.Provider, error) {
		return provider, nil
	}); err != nil {
		t.Fatalf("register fake provider: %v", err)
	}
	registry, err := llmassist.NewProfileRegistry(providers, []llmassist.ProfileDefinition{
		{
			ID:                  "default",
			DisplayName:         "Default AI Assistance",
			Provider:            "fake",
			ProviderDisplayName: "Fake Provider",
			Model:               "fake-model",
			ModelDisplayName:    "Fake Model",
			APIKey:              testBrowserProfileSecret,
			Timeout:             time.Second,
			Enabled:             true,
			Disclosure:          "Submitted text is transferred to a third-party AI provider.",
		},
	})
	if err != nil {
		t.Fatalf("construct browser profile registry: %v", err)
	}
	return registry
}

func serveBrowserAssistedRaw(t *testing.T, db *gorm.DB, registry BrowserProfileRegistry, contentType, rawBody string) (*httptest.ResponseRecorder, browserAssistedEnvelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/browser/analysis/assisted", BrowserAssistedAnalysisHandler(db, registry))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/browser/analysis/assisted", strings.NewReader(rawBody))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	var envelope browserAssistedEnvelope
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
		}
	}
	return recorder, envelope
}

func TestBrowserAssistedProfilesExposeOnlyBoundedPublicMetadata(t *testing.T) {
	provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "unused"}}
	registry := newBrowserProfileRegistry(t, provider)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/profiles", BrowserAssistedProfilesHandler(registry))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/profiles", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{testBrowserProfileSecret, "api_key", "base_url", "endpoint", "authorization"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("profile response exposed forbidden material %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{"default", "Default AI Assistance", "Fake Provider", "Fake Model", "third-party AI provider"} {
		if !strings.Contains(body, required) {
			t.Fatalf("profile response missing %q: %s", required, body)
		}
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("profile response must be no-store")
	}
	if provider.calls != 0 {
		t.Fatalf("profile metadata must not call provider, calls=%d", provider.calls)
	}
}

func TestBrowserAssistedAnalysisDeterministicFirstExactlyOneProviderCall(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	provider := &countingAssistedProvider{assistance: llmassist.Assistance{
		Summary:      "Supplemental warning",
		Observations: []string{"Transfer pressure is suspicious"},
		Limitations:  []string{"Identity is not independently verified"},
	}}
	registry := newBrowserProfileRegistry(t, provider)
	text := "客服称账户异常，需要转账到安全账户。"
	recorder, envelope := serveBrowserAssistedRaw(t, db, registry, "application/json; charset=utf-8", `{"text":"`+text+`","profile_id":"default"}`)
	if recorder.Code != http.StatusOK || !envelope.Success {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if envelope.Data.RuleResult.RiskScore <= 0 || len(envelope.Data.RuleResult.MatchedRules) != 1 {
		t.Fatalf("deterministic result missing: %+v", envelope.Data.RuleResult)
	}
	if envelope.Data.LLMAssistance.Status != llmassist.StatusAvailable || envelope.Data.LLMAssistance.Assistance.Summary != "Supplemental warning" {
		t.Fatalf("unexpected assistance: %+v", envelope.Data.LLMAssistance)
	}
	if envelope.Data.LLMAssistance.Profile.ID != "default" || envelope.Data.LLMAssistance.Profile.ProviderDisplayName != "Fake Provider" {
		t.Fatalf("unexpected public profile metadata: %+v", envelope.Data.LLMAssistance.Profile)
	}
	if provider.calls != 1 || len(provider.inputs) != 1 {
		t.Fatalf("provider must be called exactly once, calls=%d inputs=%d", provider.calls, len(provider.inputs))
	}
	if provider.inputs[0].RuleResult.RiskScore != envelope.Data.RuleResult.RiskScore {
		t.Fatalf("provider did not receive authoritative deterministic result: %+v", provider.inputs[0].RuleResult)
	}
	if strings.Contains(recorder.Body.String(), testBrowserProfileSecret) {
		t.Fatal("browser assisted response exposed profile credential")
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("browser assisted analysis must not persist AnalysisRecord, got %d", got)
	}
}

func TestBrowserAssistedProviderFailurePreservesDeterministicResultWithoutRetry(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	provider := &countingAssistedProvider{err: errors.New("provider unavailable")}
	registry := newBrowserProfileRegistry(t, provider)
	recorder, envelope := serveBrowserAssistedRaw(t, db, registry, "application/json", `{"text":"请转账到安全账户","profile_id":"default"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if envelope.Data.RuleResult.RiskScore <= 0 || envelope.Data.LLMAssistance.Status != llmassist.StatusUnavailable {
		t.Fatalf("deterministic result must survive provider failure: %+v", envelope.Data)
	}
	if provider.calls != 1 {
		t.Fatalf("provider failure must not retry, calls=%d", provider.calls)
	}
}

func TestBrowserAssistedInvalidRequestOrProfilePerformsZeroProviderCalls(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "missing content type", contentType: "", body: `{"text":"x","profile_id":"default"}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "unknown client routing field", contentType: "application/json", body: `{"text":"x","profile_id":"default","provider":"openai"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_browser_assisted_request"},
		{name: "missing profile", contentType: "application/json", body: `{"text":"x"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_profile"},
		{name: "unknown profile", contentType: "application/json", body: `{"text":"x","profile_id":"attacker-model"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_profile"},
		{name: "blank text", contentType: "application/json", body: `{"text":"  ","profile_id":"default"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_browser_assisted_request"},
		{name: "body too large", contentType: "application/json", body: `{"text":"` + strings.Repeat("x", maxAssistedAnalysisBodyBytes) + `","profile_id":"default"}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "browser_assisted_request_too_large"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newAnalysisHandlerTestDB(t)
			provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "unused"}}
			registry := newBrowserProfileRegistry(t, provider)
			recorder, envelope := serveBrowserAssistedRaw(t, db, registry, tc.contentType, tc.body)
			if recorder.Code != tc.wantStatus || envelope.Error == nil || envelope.Error.Code != tc.wantCode {
				t.Fatalf("status=%d error=%+v body=%s", recorder.Code, envelope.Error, recorder.Body.String())
			}
			if provider.calls != 0 {
				t.Fatalf("rejected request must perform zero provider calls, got %d", provider.calls)
			}
		})
	}
}

func TestBrowserAssistedDBFailureStopsBeforeProvider(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	if err := db.Migrator().DropTable("risk_rules"); err != nil {
		t.Fatalf("drop risk_rules: %v", err)
	}
	provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "unused"}}
	registry := newBrowserProfileRegistry(t, provider)
	recorder, envelope := serveBrowserAssistedRaw(t, db, registry, "application/json", `{"text":"请转账到安全账户","profile_id":"default"}`)
	if recorder.Code != http.StatusServiceUnavailable || envelope.Error == nil || envelope.Error.Code != "analysis_unavailable" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if provider.calls != 0 {
		t.Fatalf("DB failure must perform zero provider calls, got %d", provider.calls)
	}
}
