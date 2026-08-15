package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestSubmissionRouteRemainsUnregisteredWhenConfigEnabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}, &database.ScamCase{}, &database.AnalysisRecord{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		CORSAllowOrigins:         []string{"*"},
		AppPort:                  "8080",
		RuleSubmissionsEnabled:   true,
		RuleSubmissionWriteToken: "0123456789abcdef0123456789abcdef",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to be valid: %v", err)
	}

	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions", strings.NewReader(`{"code":"synthetic"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("submission route must remain unregistered in config-only slice, got %d", resp.Code)
	}
}
