package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/rule"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	reviewTransportTestToken = "review-0123456789abcdef0123456789abcdef"
	reviewTransportActorLabel = "maintainer-ci"
)

func newReviewTransportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newSubmissionTransportTestDB(t)
	if err := db.AutoMigrate(&database.RuleSubmissionReviewEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func controlledReviewConfig() config.Config {
	return config.Config{
		CORSAllowOrigins:               []string{"*"},
		AppPort:                        "8080",
		RuleSubmissionWriteToken:       submissionTransportTestToken,
		RuleSubmissionReviewsEnabled:   true,
		RuleSubmissionReviewToken:      reviewTransportTestToken,
		RuleSubmissionReviewActorLabel: reviewTransportActorLabel,
	}
}

func createReviewTransportPendingSubmission(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	submission, result, created, err := rule.CreateOrReplayPendingSubmission(db, rule.DraftRequest{
		Code:           code,
		Name:           "Controlled review transport fixture",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "synthetic review transport signal",
		Weight:         40,
		Severity:       "high",
		Explanation:    "Synthetic anti-fraud review transport test.",
		Recommendation: "Verify the request independently.",
	})
	if err != nil || !result.Valid || !created {
		t.Fatalf("create pending review fixture: created=%v result=%+v err=%v", created, result, err)
	}
	return submission
}

func assertReviewTransportPendingNoEventNoRule(t *testing.T, db *gorm.DB, submissionID uint) {
	t.Helper()
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		t.Fatal(err)
	}
	if submission.Status != rule.PendingSubmissionStatus {
		t.Fatalf("expected submission %d to remain pending, got %q", submissionID, submission.Status)
	}
	var eventCount int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submissionID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("expected zero review events for submission %d, got %d", submissionID, eventCount)
	}
	var ruleCount int64
	if err := db.Model(&database.RiskRule{}).Count(&ruleCount).Error; err != nil {
		t.Fatal(err)
	}
	if ruleCount != 0 {
		t.Fatalf("review transport must not create RiskRule rows, got %d", ruleCount)
	}
}

func performReviewTransportRequest(router http.Handler, id, token, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions/"+id+"/reviews", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestReviewRouteUnregisteredWhenDisabled(t *testing.T) {
	db := newReviewTransportTestDB(t)
	submission := createReviewTransportPendingSubmission(t, db, "review_route_disabled")
	router := newRouter(config.Config{CORSAllowOrigins: []string{"*"}, AppPort: "8080"}, zap.NewNop(), &database.Store{DB: db})
	resp := performReviewTransportRequest(router, "1", reviewTransportTestToken, "application/json", `{"decision":"approved","reason":"synthetic"}`)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("disabled review route must be unavailable, got %d", resp.Code)
	}
	assertReviewTransportPendingNoEventNoRule(t, db, submission.ID)
}

func TestReviewRouteRejectsUnauthorizedCredentialsBeforeHandler(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "missing", token: ""},
		{name: "wrong", token: "wrong-review-token"},
		{name: "submission write token", token: submissionTransportTestToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newReviewTransportTestDB(t)
			submission := createReviewTransportPendingSubmission(t, db, "review_auth_"+strings.ReplaceAll(tt.name, " ", "_"))
			cfg := controlledReviewConfig()
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected review config to be valid: %v", err)
			}
			router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
			resp := performReviewTransportRequest(router, "not-an-id", tt.token, "text/plain", "not-json")

			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("unauthorized request must return 401 before handler parsing, got %d: %s", resp.Code, resp.Body.String())
			}
			if strings.Contains(resp.Body.String(), reviewTransportTestToken) || strings.Contains(resp.Body.String(), submissionTransportTestToken) {
				t.Fatalf("authorization error must not reflect credential material: %s", resp.Body.String())
			}
			assertReviewTransportPendingNoEventNoRule(t, db, submission.ID)
		})
	}
}

func TestReviewRouteUsesIndependentAuthorizationWithoutRedisDependency(t *testing.T) {
	db := newReviewTransportTestDB(t)
	submission := createReviewTransportPendingSubmission(t, db, "review_no_redis")
	cfg := controlledReviewConfig()
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
	resp := performReviewTransportRequest(router, "1", reviewTransportTestToken, "text/plain", "not-json")

	if resp.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("authorized review request must reach handler without Redis and return 415, got %d: %s", resp.Code, resp.Body.String())
	}
	assertReviewTransportPendingNoEventNoRule(t, db, submission.ID)
}
