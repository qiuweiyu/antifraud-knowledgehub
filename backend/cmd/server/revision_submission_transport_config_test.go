package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newRevisionSubmissionTransportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newSubmissionTransportTestDB(t)
	if err := db.AutoMigrate(&database.RiskRuleVersion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func createRevisionSubmissionTransportTarget(t *testing.T, db *gorm.DB, code string) database.RiskRule {
	t.Helper()
	target := database.RiskRule{
		Code:           code,
		Name:           "Transport revision target",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "synthetic transport signal",
		Weight:         35,
		Severity:       "high",
		Enabled:        true,
		Explanation:    "Synthetic transport explanation",
		Recommendation: "Verify independently.",
		Version:        1,
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	version, err := database.BuildRiskRuleVersion(target, 1, database.RiskRuleVersionSourceLegacyBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	return target
}

func revisionSubmissionTransportBody() string {
	return `{"base_version":1,"name":"Transport revision target revised","category_code":"fake_customer_service","rule_type":"keyword","pattern":"synthetic transport signal","weight":35,"severity":"high","enabled":true,"explanation":"Synthetic transport explanation","recommendation":"Verify independently."}`
}

func performRevisionSubmissionTransportRequest(router http.Handler, targetID uint, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules/"+strconv.FormatUint(uint64(targetID), 10)+"/revision-submissions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestRevisionSubmissionRouteUnregisteredWhenSubmissionsDisabled(t *testing.T) {
	db := newRevisionSubmissionTransportTestDB(t)
	target := createRevisionSubmissionTransportTarget(t, db, "revision_route_disabled")
	router := newRouter(config.Config{CORSAllowOrigins: []string{"*"}, AppPort: "8080"}, zap.NewNop(), &database.Store{DB: db})
	resp := performRevisionSubmissionTransportRequest(router, target.ID, submissionTransportTestToken, revisionSubmissionTransportBody())
	if resp.Code != http.StatusNotFound {
		t.Fatalf("disabled revision submission route must be unavailable, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionTransportWrites(t, db, 0, 1)
}

func TestRevisionSubmissionRouteRejectsMissingAuthorizationBeforeRedisAndJSON(t *testing.T) {
	db := newRevisionSubmissionTransportTestDB(t)
	target := createRevisionSubmissionTransportTarget(t, db, "revision_auth_first")
	cfg := controlledSubmissionConfig()
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db, Redis: nil})

	// Deliberately malformed/forbidden JSON proves auth runs before Redis and handler parsing.
	resp := performRevisionSubmissionTransportRequest(router, target.ID, "", `{"code":"client-must-not-control-this"`)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("missing authorization must return 401 before Redis/JSON parsing, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionTransportWrites(t, db, 0, 1)
}

func TestRevisionSubmissionRouteFailsClosedWhenRedisUnavailable(t *testing.T) {
	db := newRevisionSubmissionTransportTestDB(t)
	target := createRevisionSubmissionTransportTarget(t, db, "revision_redis_closed")
	cfg := controlledSubmissionConfig()
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db, Redis: nil})

	resp := performRevisionSubmissionTransportRequest(router, target.ID, submissionTransportTestToken, revisionSubmissionTransportBody())
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing Redis protection must fail closed with 503, got %d: %s", resp.Code, resp.Body.String())
	}
	assertSubmissionTransportWrites(t, db, 0, 1)
}
