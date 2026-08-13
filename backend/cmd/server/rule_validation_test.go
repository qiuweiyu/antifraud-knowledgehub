package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
)

func TestRuleDraftValidationAPI(t *testing.T) {
	server := testRouter(t)
	defer server.Close()

	tests := []struct {
		name      string
		draft     map[string]any
		wantValid bool
		field     string
		code      string
	}{
		{
			name: "valid rule draft",
			draft: map[string]any{
				"code":          "community_safe_channel",
				"name":          "Official channel check",
				"category_code": "fake_customer_service",
				"rule_type":     "keyword",
				"pattern":       "official channel",
				"weight":        20,
				"severity":      "medium",
			},
			wantValid: true,
		},
		{
			name: "missing required field",
			draft: map[string]any{
				"code":          "missing_pattern",
				"name":          "Missing pattern",
				"category_code": "fake_customer_service",
				"rule_type":     "keyword",
				"weight":        20,
				"severity":      "medium",
			},
			field: "pattern",
			code:  "required",
		},
		{
			name: "duplicate rule code",
			draft: map[string]any{
				"code":          "safe_account_transfer",
				"name":          "Duplicate code",
				"category_code": "fake_customer_service",
				"rule_type":     "keyword",
				"pattern":       "official channel",
				"weight":        20,
				"severity":      "medium",
			},
			field: "code",
			code:  "duplicate_code",
		},
		{
			name: "nonexistent category",
			draft: map[string]any{
				"code":          "missing_category",
				"name":          "Missing category",
				"category_code": "not_a_category",
				"rule_type":     "keyword",
				"pattern":       "official channel",
				"weight":        20,
				"severity":      "medium",
			},
			field: "category_code",
			code:  "category_not_found",
		},
		{
			name: "invalid severity",
			draft: map[string]any{
				"code":          "invalid_severity",
				"name":          "Invalid severity",
				"category_code": "fake_customer_service",
				"rule_type":     "keyword",
				"pattern":       "official channel",
				"weight":        20,
				"severity":      "urgent",
			},
			field: "severity",
			code:  "unsupported_severity",
		},
		{
			name: "unsupported rule type",
			draft: map[string]any{
				"code":          "unsupported_type",
				"name":          "Unsupported type",
				"category_code": "fake_customer_service",
				"rule_type":     "external_ai",
				"pattern":       "official channel",
				"weight":        20,
				"severity":      "medium",
			},
			field: "rule_type",
			code:  "unsupported_rule_type",
		},
		{
			name: "invalid regex",
			draft: map[string]any{
				"code":          "invalid_regex",
				"name":          "Invalid regex",
				"category_code": "fake_customer_service",
				"rule_type":     "regex",
				"pattern":       "[",
				"weight":        20,
				"severity":      "medium",
			},
			field: "pattern",
			code:  "invalid_regex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := validateRuleDraft(t, server.URL, tt.draft)
			if !payload.Success {
				t.Fatalf("expected success envelope: %+v", payload)
			}
			if payload.Data.Valid != tt.wantValid {
				t.Fatalf("expected valid=%v: %+v", tt.wantValid, payload.Data)
			}
			if tt.wantValid {
				if len(payload.Data.Errors) != 0 {
					t.Fatalf("expected no errors: %+v", payload.Data.Errors)
				}
				return
			}
			if !hasValidationError(payload.Data.Errors, tt.field, tt.code) {
				t.Fatalf("expected %s/%s error: %+v", tt.field, tt.code, payload.Data.Errors)
			}
		})
	}
}

func TestRuleDraftValidationDoesNotCreateRule(t *testing.T) {
	server := testRouter(t)
	defer server.Close()

	before := countRules(t, server.URL)
	payload := validateRuleDraft(t, server.URL, map[string]any{
		"code":          "validate_only_rule",
		"name":          "Validate only rule",
		"category_code": "fake_customer_service",
		"rule_type":     "keyword",
		"pattern":       "official channel",
		"weight":        20,
		"severity":      "medium",
	})
	if !payload.Success || !payload.Data.Valid {
		t.Fatalf("expected valid draft: %+v", payload)
	}
	after := countRules(t, server.URL)
	if after != before {
		t.Fatalf("validate endpoint must not create records: before=%d after=%d", before, after)
	}
}

func TestCreateRuleUsesDraftValidator(t *testing.T) {
	server := testRouter(t)
	defer server.Close()

	body, _ := json.Marshal(map[string]any{
		"code":          "create_invalid_category",
		"name":          "Create invalid category",
		"category_code": "not_a_category",
		"rule_type":     "keyword",
		"pattern":       "official channel",
		"weight":        20,
		"severity":      "medium",
	})
	resp, err := http.Post(server.URL+"/api/v1/rules", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected create to reject invalid category with 400, got %d", resp.StatusCode)
	}
}

type ruleDraftPayload struct {
	Success bool `json:"success"`
	Data    struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"errors"`
		Warnings []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"warnings"`
	} `json:"data"`
}

func validateRuleDraft(t *testing.T, baseURL string, draft map[string]any) ruleDraftPayload {
	t.Helper()
	body, _ := json.Marshal(draft)
	resp, err := http.Post(baseURL+"/api/v1/rules/validate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload ruleDraftPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func hasValidationError(errors []struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}, field, code string) bool {
	for _, item := range errors {
		if item.Field == field && item.Code == code {
			return true
		}
	}
	return false
}

func countRules(t *testing.T, baseURL string) int {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/v1/rules")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Success bool                `json:"success"`
		Data    []database.RiskRule `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success {
		t.Fatalf("expected successful rule list: %+v", payload)
	}
	return len(payload.Data)
}
