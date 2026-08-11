package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func testRouter(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.ScamCase{}, &database.AnalysisRecord{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&database.RiskRule{
		Code: "safe_account_transfer", Name: "安全账户转账", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "安全账户", Weight: 30, Severity: "critical", Enabled: true,
		Explanation: "安全账户是典型诈骗话术。", Recommendation: "不要向陌生账户转账。",
	})
	db.Create(&database.Category{Code: "fake_customer_service", Name: "冒充客服", SeverityDefault: "high"})
	db.Create(&database.ScamCase{Title: "冒充客服案例", CategoryCode: "fake_customer_service", Content: "客服要求转账到安全账户", Anonymized: true})
	router := newRouter(config.Config{CORSAllowOrigins: []string{"*"}, AppPort: "8080"}, zap.NewNop(), &database.Store{DB: db})
	return httptest.NewServer(router)
}

func TestHealthAPI(t *testing.T) {
	server := testRouter(t)
	defer server.Close()
	resp, err := http.Get(server.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAnalysisAPI(t *testing.T) {
	server := testRouter(t)
	defer server.Close()
	body, _ := json.Marshal(map[string]string{"text": "客服说账户异常，需要转账到安全账户"})
	resp, err := http.Post(server.URL+"/api/v1/analysis/text", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			RiskScore int `json:"risk_score"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.RiskScore == 0 {
		t.Fatalf("expected successful risky response: %+v", payload)
	}
}

func TestAnalysisStatsAPI(t *testing.T) {
	server := testRouter(t)
	defer server.Close()
	resp, err := http.Get(server.URL + "/api/v1/analysis/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Categories            int64            `json:"categories"`
			Rules                 int64            `json:"rules"`
			Cases                 int64            `json:"cases"`
			RiskLevelDistribution map[string]int64 `json:"risk_level_distribution"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Categories != 1 || payload.Data.Rules != 1 || payload.Data.Cases != 1 {
		t.Fatalf("unexpected stats response: %+v", payload)
	}
	if _, ok := payload.Data.RiskLevelDistribution["critical"]; !ok {
		t.Fatalf("expected risk level buckets: %+v", payload.Data.RiskLevelDistribution)
	}
}

func TestRuleSearchAPI(t *testing.T) {
	server := testRouter(t)
	defer server.Close()
	resp, err := http.Get(server.URL + "/api/v1/rules?q=安全账户")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Success bool                `json:"success"`
		Data    []database.RiskRule `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || len(payload.Data) != 1 {
		t.Fatalf("expected one matched rule: %+v", payload)
	}
}
