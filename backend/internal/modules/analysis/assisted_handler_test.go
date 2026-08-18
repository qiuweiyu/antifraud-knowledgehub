package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type countingAssistedProvider struct {
	calls      int
	inputs     []llmassist.Input
	assistance llmassist.Assistance
	err        error
}

func (p *countingAssistedProvider) Assist(_ context.Context, input llmassist.Input) (llmassist.Assistance, error) {
	p.calls++
	p.inputs = append(p.inputs, input)
	if p.err != nil {
		return llmassist.Assistance{}, p.err
	}
	return p.assistance, nil
}

type assistedEnvelope struct {
	Success bool                     `json:"success"`
	Data    AssistedAnalysisResponse `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newAssistedService(t *testing.T, provider *countingAssistedProvider) llmassist.Service {
	t.Helper()
	service, err := llmassist.NewService(provider, time.Second)
	if err != nil {
		t.Fatalf("construct assistance service: %v", err)
	}
	return service
}

func serveAssistedRaw(t *testing.T, db *gorm.DB, service assistedAnalysisService, provider, model, contentType, rawBody string) (*httptest.ResponseRecorder, assistedEnvelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/analysis/assisted", AssistedAnalysisHandler(db, service, provider, model))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/assisted", strings.NewReader(rawBody))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	var envelope assistedEnvelope
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
		}
	}
	return recorder, envelope
}

func TestAssistedAnalysisReturnsDeterministicResultAndAvailableAssistanceWithoutPersistence(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	provider := &countingAssistedProvider{assistance: llmassist.Assistance{
		Summary:      "Supplemental warning",
		Observations: []string{"The transfer request is unusual"},
		Limitations:  []string{"Sender identity is not independently verified"},
	}}
	service := newAssistedService(t, provider)
	text := "客服称账户异常，需要转账到安全账户。"
	raw := `{"text":"` + text + `"}`

	recorder, envelope := serveAssistedRaw(t, db, service, "gemini", "gemini-runtime-model", "application/json; charset=utf-8", raw)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !envelope.Success {
		t.Fatalf("expected success: %s", recorder.Body.String())
	}
	if envelope.Data.RuleResult.RiskScore <= 0 || len(envelope.Data.RuleResult.MatchedRules) != 1 {
		t.Fatalf("expected deterministic rule match: %+v", envelope.Data.RuleResult)
	}
	if envelope.Data.LLMAssistance.Status != llmassist.StatusAvailable {
		t.Fatalf("assistance status=%s", envelope.Data.LLMAssistance.Status)
	}
	if envelope.Data.LLMAssistance.Provider != "gemini" || envelope.Data.LLMAssistance.Model != "gemini-runtime-model" {
		t.Fatalf("provider metadata must be server-owned: %+v", envelope.Data.LLMAssistance)
	}
	if envelope.Data.LLMAssistance.Assistance.Summary != "Supplemental warning" {
		t.Fatalf("unexpected assistance: %+v", envelope.Data.LLMAssistance.Assistance)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
	if len(provider.inputs) != 1 || provider.inputs[0].Text != text {
		t.Fatalf("unexpected provider input: %+v", provider.inputs)
	}
	if provider.inputs[0].RuleResult.RiskScore != envelope.Data.RuleResult.RiskScore || len(provider.inputs[0].RuleResult.MatchedRules) != 1 {
		t.Fatalf("provider must receive already-computed deterministic result: %+v", provider.inputs[0].RuleResult)
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("assisted analysis must not persist AnalysisRecord, got %d", got)
	}
}

func TestAssistedAnalysisProviderFailureKeepsDeterministicHTTP200(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	provider := &countingAssistedProvider{err: errors.New("provider unavailable")}
	service := newAssistedService(t, provider)

	recorder, envelope := serveAssistedRaw(t, db, service, "deepseek", "deepseek-runtime-model", "application/json", `{"text":"请转账到安全账户"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if envelope.Data.RuleResult.RiskScore <= 0 || len(envelope.Data.RuleResult.MatchedRules) != 1 {
		t.Fatalf("deterministic result must survive provider failure: %+v", envelope.Data.RuleResult)
	}
	if envelope.Data.LLMAssistance.Status != llmassist.StatusUnavailable {
		t.Fatalf("provider failure must become unavailable: %+v", envelope.Data.LLMAssistance)
	}
	if envelope.Data.LLMAssistance.Provider != "deepseek" || envelope.Data.LLMAssistance.Model != "deepseek-runtime-model" {
		t.Fatalf("server provider/model metadata missing: %+v", envelope.Data.LLMAssistance)
	}
	if provider.calls != 1 {
		t.Fatalf("provider failure path must not retry, calls=%d", provider.calls)
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("provider failure must not persist AnalysisRecord, got %d", got)
	}
}

func TestAssistedAnalysisRejectsInvalidTransportBeforeProvider(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "missing content type", contentType: "", body: `{"text":"test"}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "wrong content type", contentType: "text/plain", body: `{"text":"test"}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "empty body", contentType: "application/json", body: "", wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "null", contentType: "application/json", body: "null", wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "array", contentType: "application/json", body: `[]`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "scalar", contentType: "application/json", body: `"text"`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "unknown field", contentType: "application/json", body: `{"text":"x","provider":"openai"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "trailing JSON", contentType: "application/json", body: `{"text":"x"}{"text":"y"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "missing text", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "blank text", contentType: "application/json", body: `{"text":"   "}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "text over source limit", contentType: "application/json", body: `{"text":"` + strings.Repeat("x", maxAssistedAnalysisTextBytes+1) + `"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_assisted_analysis_request"},
		{name: "body over transport limit", contentType: "application/json", body: `{"text":"` + strings.Repeat("x", maxAssistedAnalysisBodyBytes) + `"}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "assisted_analysis_request_too_large"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newAnalysisHandlerTestDB(t)
			provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "unused"}}
			service := newAssistedService(t, provider)
			recorder, envelope := serveAssistedRaw(t, db, service, "openai", "server-model", tc.contentType, tc.body)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if envelope.Error == nil || envelope.Error.Code != tc.wantCode {
				t.Fatalf("error=%+v want=%s body=%s", envelope.Error, tc.wantCode, recorder.Body.String())
			}
			if provider.calls != 0 {
				t.Fatalf("invalid transport must perform zero provider calls, got %d", provider.calls)
			}
			if got := analysisRecordCount(t, db); got != 0 {
				t.Fatalf("invalid transport must not persist AnalysisRecord, got %d", got)
			}
		})
	}
}

func TestAssistedAnalysisDBFailureStopsBeforeProvider(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	if err := db.Migrator().DropTable("risk_rules"); err != nil {
		t.Fatalf("drop risk_rules: %v", err)
	}
	provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "unused"}}
	service := newAssistedService(t, provider)

	recorder, envelope := serveAssistedRaw(t, db, service, "openai", "server-model", "application/json", `{"text":"请转账到安全账户"}`)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if envelope.Error == nil || envelope.Error.Code != "analysis_unavailable" {
		t.Fatalf("expected analysis_unavailable, body=%s", recorder.Body.String())
	}
	if provider.calls != 0 {
		t.Fatalf("DB failure must perform zero provider calls, got %d", provider.calls)
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("DB failure must not persist AnalysisRecord, got %d", got)
	}
}

func TestAssistedAnalysisNilDependenciesFailClosed(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	recorder, envelope := serveAssistedRaw(t, db, nil, "openai", "server-model", "application/json", `{"text":"test"}`)
	if recorder.Code != http.StatusServiceUnavailable || envelope.Error == nil || envelope.Error.Code != "analysis_unavailable" {
		t.Fatalf("nil service must fail closed: %d %s", recorder.Code, recorder.Body.String())
	}

	provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "unused"}}
	service := newAssistedService(t, provider)
	recorder, envelope = serveAssistedRaw(t, nil, service, "openai", "server-model", "application/json", `{"text":"test"}`)
	if recorder.Code != http.StatusServiceUnavailable || envelope.Error == nil || envelope.Error.Code != "analysis_unavailable" {
		t.Fatalf("nil DB must fail closed: %d %s", recorder.Code, recorder.Body.String())
	}
	if provider.calls != 0 {
		t.Fatalf("nil DB must perform zero provider calls, got %d", provider.calls)
	}
}

func TestAssistedAnalysisRuleResultMatchesPreviewLogic(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	provider := &countingAssistedProvider{assistance: llmassist.Assistance{Summary: "Supplemental"}}
	service := newAssistedService(t, provider)
	text := "客服称账户异常，需要转账到安全账户。"

	_, preview := serveAnalysisRequest(t, db, "/api/v1/analysis/preview", map[string]string{"text": text})
	recorder, assisted := serveAssistedRaw(t, db, service, "openai", "server-model", "application/json", `{"text":"`+text+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("assisted status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !sameDeterministicResult(preview.Data, assisted.Data.RuleResult) {
		t.Fatalf("assisted deterministic result drifted from preview\npreview=%+v\nassisted=%+v", preview.Data, assisted.Data.RuleResult)
	}
}

func sameDeterministicResult(a, b riskengine.Result) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}
