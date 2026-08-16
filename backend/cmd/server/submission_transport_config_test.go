package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const submissionTransportTestToken = "0123456789abcdef0123456789abcdef"

func newSubmissionTransportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}, &database.ScamCase{}, &database.AnalysisRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Category{Code: "fake_customer_service", Name: "冒充客服", SeverityDefault: "high"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func controlledSubmissionConfig() config.Config {
	return config.Config{
		CORSAllowOrigins:              []string{"*"},
		AppPort:                       "8080",
		RuleSubmissionsEnabled:        true,
		RuleSubmissionWriteToken:      submissionTransportTestToken,
		RuleSubmissionCredentialLimit: 2,
		RuleSubmissionGlobalLimit:     10,
		RuleSubmissionRateWindow:      time.Minute,
	}
}

func assertSubmissionTransportWrites(t *testing.T, db *gorm.DB, submissions, rules int64) {
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

func TestSubmissionRouteUnregisteredWhenDisabled(t *testing.T) {
	db := newSubmissionTransportTestDB(t)
	router := newRouter(config.Config{CORSAllowOrigins: []string{"*"}, AppPort: "8080"}, zap.NewNop(), &database.Store{DB: db})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions", strings.NewReader(`{"code":"synthetic"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("disabled submission route must be unavailable, got %d", resp.Code)
	}
	assertSubmissionTransportWrites(t, db, 0, 0)
}

func TestSubmissionRouteRejectsMissingAuthorizationBeforeRedis(t *testing.T) {
	db := newSubmissionTransportTestDB(t)
	cfg := controlledSubmissionConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to be valid: %v", err)
	}
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("missing authorization must return 401 before Redis is evaluated, got %d", resp.Code)
	}
	assertSubmissionTransportWrites(t, db, 0, 0)
}

func TestSubmissionRouteFailsClosedWhenRedisUnavailable(t *testing.T) {
	db := newSubmissionTransportTestDB(t)
	cfg := controlledSubmissionConfig()
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+submissionTransportTestToken)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing Redis protection must fail closed with 503, got %d", resp.Code)
	}
	assertSubmissionTransportWrites(t, db, 0, 0)
}
