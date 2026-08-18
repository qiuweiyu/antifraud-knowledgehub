package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/rule"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	publicationTransportTestToken = "publish-0123456789abcdef0123456789abcdef"
	publicationTransportActor     = "publisher-ci"
)

func newPublicationTransportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newReviewTransportTestDB(t)
	if err := db.AutoMigrate(&database.RuleSubmissionPublicationEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func controlledPublicationConfig() config.Config {
	return config.Config{
		CORSAllowOrigins:                    []string{"*"},
		AppPort:                             "8080",
		RuleSubmissionWriteToken:            submissionTransportTestToken,
		RuleSubmissionReviewToken:           reviewTransportTestToken,
		RuleSubmissionPublicationsEnabled:   true,
		RuleSubmissionPublicationToken:      publicationTransportTestToken,
		RuleSubmissionPublicationActorLabel: publicationTransportActor,
	}
}

func createPublicationTransportApprovedSubmission(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	pending := createReviewTransportPendingSubmission(t, db, code)
	outcome, err := rule.ReviewPendingSubmission(db, pending.ID, rule.SubmissionReviewCommand{
		Decision:   rule.ApprovedSubmissionStatus,
		Reason:     "Synthetic approved publication transport fixture.",
		ActorLabel: reviewTransportActorLabel,
	})
	if err != nil {
		t.Fatal(err)
	}
	return outcome.Submission
}

func performPublicationTransportRequest(router http.Handler, id, authorization, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions/"+id+"/publications", strings.NewReader(body))
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func assertPublicationTransportState(t *testing.T, db *gorm.DB, submissionID uint, wantRules, wantPublicationEvents int64) {
	t.Helper()
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		t.Fatal(err)
	}
	if submission.Status != rule.ApprovedSubmissionStatus {
		t.Fatalf("publication transport must preserve approved submission status, got %q", submission.Status)
	}
	var reviewCount int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submissionID).Count(&reviewCount).Error; err != nil {
		t.Fatal(err)
	}
	if reviewCount != 1 {
		t.Fatalf("publication transport must preserve exactly one approved review event, got %d", reviewCount)
	}
	var ruleCount int64
	if err := db.Model(&database.RiskRule{}).Count(&ruleCount).Error; err != nil {
		t.Fatal(err)
	}
	if ruleCount != wantRules {
		t.Fatalf("unexpected RiskRule count: want=%d got=%d", wantRules, ruleCount)
	}
	var publicationCount int64
	if err := db.Model(&database.RuleSubmissionPublicationEvent{}).Where("submission_id = ?", submissionID).Count(&publicationCount).Error; err != nil {
		t.Fatal(err)
	}
	if publicationCount != wantPublicationEvents {
		t.Fatalf("unexpected publication event count: want=%d got=%d", wantPublicationEvents, publicationCount)
	}
}

func TestPublicationRouteUnregisteredWhenDisabled(t *testing.T) {
	db := newPublicationTransportTestDB(t)
	submission := createPublicationTransportApprovedSubmission(t, db, "publication_route_disabled")
	router := newRouter(config.Config{CORSAllowOrigins: []string{"*"}, AppPort: "8080"}, zap.NewNop(), &database.Store{DB: db})
	resp := performPublicationTransportRequest(router, strconv.FormatUint(uint64(submission.ID), 10), "Bearer "+publicationTransportTestToken, "application/json", `{}`)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("disabled publication route must be unavailable, got %d: %s", resp.Code, resp.Body.String())
	}
	assertPublicationTransportState(t, db, submission.ID, 0, 0)
}

func TestPublicationRouteRejectsUnauthorizedCredentialsBeforeHandler(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing"},
		{name: "malformed scheme", authorization: "Basic abc"},
		{name: "malformed bearer whitespace", authorization: "Bearer bad token"},
		{name: "wrong", authorization: "Bearer wrong-publication-token"},
		{name: "submission write token", authorization: "Bearer " + submissionTransportTestToken},
		{name: "review token", authorization: "Bearer " + reviewTransportTestToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newPublicationTransportTestDB(t)
			submission := createPublicationTransportApprovedSubmission(t, db, "publication_auth_"+strings.ReplaceAll(tt.name, " ", "_"))
			cfg := controlledPublicationConfig()
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected publication config to be valid: %v", err)
			}
			router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
			resp := performPublicationTransportRequest(router, "not-an-id", tt.authorization, "text/plain", "not-json")
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("unauthorized publication request must return 401 before handler parsing, got %d: %s", resp.Code, resp.Body.String())
			}
			for _, secret := range []string{publicationTransportTestToken, submissionTransportTestToken, reviewTransportTestToken} {
				if strings.Contains(resp.Body.String(), secret) {
					t.Fatalf("authorization response must not reflect credential material: %s", resp.Body.String())
				}
			}
			assertPublicationTransportState(t, db, submission.ID, 0, 0)
		})
	}
}

func TestPublicationRouteUsesIndependentAuthorizationWithoutRedisDependency(t *testing.T) {
	db := newPublicationTransportTestDB(t)
	submission := createPublicationTransportApprovedSubmission(t, db, "publication_no_redis")
	cfg := controlledPublicationConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected publication config to be valid: %v", err)
	}
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db, Redis: nil})
	resp := performPublicationTransportRequest(router, strconv.FormatUint(uint64(submission.ID), 10), "Bearer "+publicationTransportTestToken, "text/plain", "not-json")
	if resp.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("authorized publication request must reach handler without Redis and return 415, got %d: %s", resp.Code, resp.Body.String())
	}
	assertPublicationTransportState(t, db, submission.ID, 0, 0)
}

func TestPublicationRouteCompletesApprovedSubmissionAndReplays(t *testing.T) {
	db := newPublicationTransportTestDB(t)
	submission := createPublicationTransportApprovedSubmission(t, db, "publication_route_success")
	cfg := controlledPublicationConfig()
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db, Redis: nil})
	id := strconv.FormatUint(uint64(submission.ID), 10)

	first := performPublicationTransportRequest(router, id, "Bearer "+publicationTransportTestToken, "application/json", `{}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first controlled publication must return 201, got %d: %s", first.Code, first.Body.String())
	}
	assertPublicationTransportState(t, db, submission.ID, 1, 1)

	replay := performPublicationTransportRequest(router, id, "Bearer "+publicationTransportTestToken, "application/json", `{}`)
	if replay.Code != http.StatusOK {
		t.Fatalf("exact controlled publication replay must return 200, got %d: %s", replay.Code, replay.Body.String())
	}
	if !strings.Contains(replay.Body.String(), `"replay":true`) {
		t.Fatalf("replay response must explicitly report replay=true: %s", replay.Body.String())
	}
	assertPublicationTransportState(t, db, submission.ID, 1, 1)
}
