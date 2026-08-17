package rule

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newSubmissionHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}); err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Category{Code: "fake_customer_service", Name: "冒充客服", SeverityDefault: "high"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func validSubmissionDraftJSON(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(DraftRequest{
		Code:           "community_safe_account_request",
		Name:           "安全账户诱导转账",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "安全账户",
		Weight:         40,
		Severity:       "high",
		Explanation:    "Synthetic anti-fraud rule example.",
		Recommendation: "Verify the request independently before transferring funds.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func marshalSubmissionJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func serveSubmissionHandler(db *gorm.DB, contentType string, body []byte) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/submit", SubmissionCreateHandler(db))
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	return resp
}

func assertSubmissionCounts(t *testing.T, db *gorm.DB, submissions, rules int64) {
	t.Helper()
	var submissionCount int64
	var ruleCount int64
	if err := db.Model(&database.RuleSubmission{}).Count(&submissionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&database.RiskRule{}).Count(&ruleCount).Error; err != nil {
		t.Fatal(err)
	}
	if submissionCount != submissions || ruleCount != rules {
		t.Fatalf("unexpected write counts: submissions=%d rules=%d", submissionCount, ruleCount)
	}
}

func TestSubmissionCreateHandlerCreatesOnePendingSubmission(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	resp := serveSubmissionHandler(db, "application/json; charset=utf-8", validSubmissionDraftJSON(t))
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionCounts(t, db, 1, 0)

	var item database.RuleSubmission
	if err := db.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != PendingSubmissionStatus || item.DraftDigest == nil {
		t.Fatalf("expected digested pending submission, got %+v", item)
	}
	if strings.Contains(resp.Body.String(), "draft_digest") {
		t.Fatal("server-owned draft digest must not be exposed in public response")
	}
}

func TestSubmissionCreateHandlerExactReplayReturns200AndSameBody(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	body := validSubmissionDraftJSON(t)
	first := serveSubmissionHandler(db, "application/json", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first request 201, got %d: %s", first.Code, first.Body.String())
	}
	second := serveSubmissionHandler(db, "application/json", body)
	if second.Code != http.StatusOK {
		t.Fatalf("expected exact replay 200, got %d: %s", second.Code, second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("exact replay must return the existing submission payload\nfirst: %s\nsecond: %s", first.Body.String(), second.Body.String())
	}
	assertSubmissionCounts(t, db, 1, 0)
}

func TestSubmissionCreateHandlerJSONPropertyOrderDoesNotChangeReplayIdentity(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	firstBody := validSubmissionDraftJSON(t)
	var reordered map[string]any
	if err := json.Unmarshal(firstBody, &reordered); err != nil {
		t.Fatal(err)
	}
	secondBody := marshalSubmissionJSON(t, reordered)
	if bytes.Equal(firstBody, secondBody) {
		t.Fatal("test fixture must use different raw JSON serialization")
	}
	first := serveSubmissionHandler(db, "application/json", firstBody)
	second := serveSubmissionHandler(db, "application/json", secondBody)
	if first.Code != http.StatusCreated || second.Code != http.StatusOK {
		t.Fatalf("field-order replay semantics failed: first=%d second=%d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatal("different JSON property order must resolve to the same pending submission")
	}
	assertSubmissionCounts(t, db, 1, 0)
}

func TestSubmissionCreateHandlerRejectsClientDraftDigestWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	body := marshalSubmissionJSON(t, map[string]any{
		"code": "synthetic", "name": "Synthetic", "category_code": "fake_customer_service",
		"rule_type": "keyword", "pattern": "synthetic", "weight": 10, "severity": "low",
		"draft_digest": strings.Repeat("a", 64),
	})
	resp := serveSubmissionHandler(db, "application/json", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("client-provided draft_digest must be rejected, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionCounts(t, db, 0, 0)
}

func TestSubmissionCreateHandlerRejectsInvalidDraftWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	body := marshalSubmissionJSON(t, map[string]any{
		"code": "bad-category", "name": "Bad category", "category_code": "missing",
		"rule_type": "keyword", "pattern": "synthetic", "weight": 10, "severity": "low",
	})
	resp := serveSubmissionHandler(db, "application/json", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionCounts(t, db, 0, 0)
}

func TestSubmissionCreateHandlerRejectsUnsupportedContentTypeWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	resp := serveSubmissionHandler(db, "text/plain", validSubmissionDraftJSON(t))
	if resp.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", resp.Code)
	}
	assertSubmissionCounts(t, db, 0, 0)
}

func TestSubmissionCreateHandlerRejectsOversizedBodyWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	body := []byte("{\"code\":\"" + strings.Repeat("a", int(MaxSubmissionRequestBodyBytes)) + "\"}")
	resp := serveSubmissionHandler(db, "application/json", body)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionCounts(t, db, 0, 0)
}

func TestSubmissionCreateHandlerRejectsMalformedJSONWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	resp := serveSubmissionHandler(db, "application/json", []byte("{\"code\":"))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
	assertSubmissionCounts(t, db, 0, 0)
}

func TestSubmissionCreateHandlerRejectsUnknownFieldWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	body := marshalSubmissionJSON(t, map[string]any{
		"code": "synthetic", "name": "Synthetic", "category_code": "fake_customer_service",
		"rule_type": "keyword", "pattern": "synthetic", "weight": 10, "severity": "low",
		"unexpected": "reject-me",
	})
	resp := serveSubmissionHandler(db, "application/json", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionCounts(t, db, 0, 0)
}

func TestSubmissionCreateHandlerRejectsTrailingJSONWithoutWrites(t *testing.T) {
	db := newSubmissionHandlerTestDB(t)
	body := append(validSubmissionDraftJSON(t), []byte(" {\"second\":true}")...)
	resp := serveSubmissionHandler(db, "application/json", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionCounts(t, db, 0, 0)
}
