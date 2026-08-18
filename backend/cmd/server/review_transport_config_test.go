package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"go.uber.org/zap"
)

func TestReviewRouteRemainsUnregisteredWhenControlledConfigEnabled(t *testing.T) {
	db := newSubmissionTransportTestDB(t)
	if err := db.AutoMigrate(&database.RuleSubmissionReviewEvent{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		CORSAllowOrigins:               []string{"*"},
		AppPort:                        "8080",
		RuleSubmissionReviewsEnabled:   true,
		RuleSubmissionReviewToken:      "review-0123456789abcdef0123456789abcdef",
		RuleSubmissionReviewActorLabel: "maintainer-ci",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected review config to be valid: %v", err)
	}

	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions/1/reviews", strings.NewReader(`{"decision":"approved","reason":"synthetic"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("review route must remain unavailable until authorization transport is implemented, got %d", resp.Code)
	}
	assertSubmissionTransportWrites(t, db, 0, 0)

	var eventCount int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("unregistered review route must create zero review events, got %d", eventCount)
	}
}
