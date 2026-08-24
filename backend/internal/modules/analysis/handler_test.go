package analysis

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newAnalysisHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.RiskRule{}, &database.AnalysisRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.RiskRule{
		Code:           "test_safe_account",
		Name:           "安全账户风险",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "安全账户",
		Weight:         45,
		Severity:       "high",
		Enabled:        true,
		Explanation:    "要求向所谓安全账户转账是高风险信号。",
		Recommendation: "通过官方渠道独立核验，不要按对方要求转账。",
		Version:        1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

type analysisEnvelope struct {
	Success bool              `json:"success"`
	Data    riskengine.Result `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func serveAnalysisRequest(t *testing.T, db *gorm.DB, path string, body any) (*httptest.ResponseRecorder, analysisEnvelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r.Group("/api/v1"), db)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	var envelope analysisEnvelope
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, resp.Body.String())
	}
	return resp, envelope
}

func analysisRecordCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&database.AnalysisRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPreviewAnalysisReturnsExplainableResultWithoutHistoryWrite(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	resp, payload := serveAnalysisRequest(t, db, "/api/v1/analysis/preview", map[string]string{
		"text": "客服称账户异常，需要转账到安全账户。",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if !payload.Success {
		t.Fatalf("expected success response: %s", resp.Body.String())
	}
	if payload.Data.RiskScore <= 0 || len(payload.Data.MatchedRules) != 1 {
		t.Fatalf("expected matched explainable rule, got %+v", payload.Data)
	}
	matched := payload.Data.MatchedRules[0]
	if matched.RuleCode != "test_safe_account" || matched.RuleVersion != 1 || matched.Evidence == "" || matched.Explanation == "" || matched.Recommendation == "" {
		t.Fatalf("expected exact rule version/provenance/evidence in result, got %+v", matched)
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("preview must not create analysis history, got %d rows", got)
	}
}

func TestPreviewAnalysisInvalidRequestDoesNotWriteHistory(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	resp, payload := serveAnalysisRequest(t, db, "/api/v1/analysis/preview", map[string]string{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	if payload.Error == nil || payload.Error.Code != "invalid_analysis_request" {
		t.Fatalf("expected invalid_analysis_request, got %s", resp.Body.String())
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("invalid preview must not create analysis history, got %d rows", got)
	}
}

func TestPersistedAnalysisRetainsExactRuleVersionAndSharesResultLogic(t *testing.T) {
	db := newAnalysisHandlerTestDB(t)
	body := map[string]string{"text": "客服称账户异常，需要转账到安全账户。"}

	previewResp, preview := serveAnalysisRequest(t, db, "/api/v1/analysis/preview", body)
	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview failed: %d %s", previewResp.Code, previewResp.Body.String())
	}
	if got := analysisRecordCount(t, db); got != 0 {
		t.Fatalf("preview must leave history empty, got %d rows", got)
	}

	persistedResp, persisted := serveAnalysisRequest(t, db, "/api/v1/analysis/text", body)
	if persistedResp.Code != http.StatusOK {
		t.Fatalf("persisted analysis failed: %d %s", persistedResp.Code, persistedResp.Body.String())
	}
	if !reflect.DeepEqual(preview.Data, persisted.Data) {
		t.Fatalf("preview and persisted analysis must share result logic\npreview=%+v\npersisted=%+v", preview.Data, persisted.Data)
	}
	if len(persisted.Data.MatchedRules) != 1 || persisted.Data.MatchedRules[0].RuleVersion != 1 {
		t.Fatalf("persisted response must identify exact rule version: %+v", persisted.Data.MatchedRules)
	}
	if got := analysisRecordCount(t, db); got != 1 {
		t.Fatalf("persisted /analysis/text must create one history row, got %d", got)
	}
	var record database.AnalysisRecord
	if err := db.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.InputText != body["text"] || record.RiskScore != persisted.Data.RiskScore || record.RiskLevel != persisted.Data.RiskLevel {
		t.Fatalf("persisted history changed semantics: %+v", record)
	}
	var stored []riskengine.MatchedRule
	if err := json.Unmarshal(record.MatchedRules, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].RuleCode != "test_safe_account" || stored[0].RuleVersion != 1 {
		t.Fatalf("new AnalysisRecord must persist exact matched rule version: %+v", stored)
	}
}