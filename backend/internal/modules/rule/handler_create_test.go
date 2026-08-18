package rule

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/gin-gonic/gin"
)

func TestDirectRuleCreatePreservesEnabledIntent(t *testing.T) {
	tests := []struct {
		name            string
		code            string
		enabledFragment string
		wantEnabled     bool
	}{
		{name: "omitted defaults true", code: "direct_enabled_omitted", wantEnabled: true},
		{name: "explicit true stays true", code: "direct_enabled_true", enabledFragment: `,"enabled":true`, wantEnabled: true},
		{name: "explicit false stays false", code: "direct_enabled_false", enabledFragment: `,"enabled":false`, wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := reviewTestDB(t)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			Register(router.Group("/api/v1"), db)

			body := `{"code":"` + tt.code + `","name":"Direct enabled regression","category_code":"fake_customer_service","rule_type":"keyword","pattern":"direct enabled regression signal","weight":20,"severity":"high","explanation":"Regression fixture","recommendation":"Verify via official channels."` + tt.enabledFragment + `}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != http.StatusCreated {
				t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, resp.Code, resp.Body.String())
			}

			var envelope struct {
				Success bool              `json:"success"`
				Data    database.RiskRule `json:"data"`
			}
			if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode create response: %v", err)
			}
			if !envelope.Success {
				t.Fatalf("expected successful create response: %s", resp.Body.String())
			}
			if envelope.Data.Enabled != tt.wantEnabled {
				t.Fatalf("response enabled mismatch: want %v got %v", tt.wantEnabled, envelope.Data.Enabled)
			}

			var stored database.RiskRule
			if err := db.Where("code = ?", tt.code).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Enabled != tt.wantEnabled {
				t.Fatalf("stored enabled mismatch: want %v got %v", tt.wantEnabled, stored.Enabled)
			}
			if stored.SourceSubmissionID != nil {
				t.Fatalf("direct rule create must not set publication provenance, got source_submission_id=%d", *stored.SourceSubmissionID)
			}
		})
	}
}
