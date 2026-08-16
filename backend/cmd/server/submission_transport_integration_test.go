package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func submissionTransportRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("AFKH_REDIS_INTEGRATION_ADDR")
	if addr == "" {
		t.Skip("AFKH_REDIS_INTEGRATION_ADDR is not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("ping integration Redis: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("reset integration Redis DB: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.FlushDB(ctx).Err()
		_ = client.Close()
	})
	return client
}

func validSubmissionTransportBody(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"code":           "transport_safe_account_request",
		"name":           "安全账户诱导转账",
		"category_code":  "fake_customer_service",
		"rule_type":      "keyword",
		"pattern":        "安全账户",
		"weight":         40,
		"severity":       "high",
		"explanation":    "Synthetic anti-fraud transport test.",
		"recommendation": "Verify requests independently before transferring funds.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func performSubmissionTransportRequest(router http.Handler, token, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions", strings.NewReader(body))
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

func TestControlledSubmissionTransportIntegrationCreatesPendingAndRateLimits(t *testing.T) {
	db := newSubmissionTransportTestDB(t)
	client := submissionTransportRedisClient(t)
	cfg := controlledSubmissionConfig()
	cfg.RuleSubmissionCredentialLimit = 1
	cfg.RuleSubmissionGlobalLimit = 5
	cfg.RuleSubmissionRateWindow = time.Minute

	core, observed := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	router := newRouter(cfg, logger, &database.Store{DB: db, Redis: client})
	body := validSubmissionTransportBody(t)

	wrongToken := performSubmissionTransportRequest(router, "wrong-token-value", "application/json", body)
	if wrongToken.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", wrongToken.Code)
	}
	assertSubmissionTransportWrites(t, db, 0, 0)

	created := performSubmissionTransportRequest(router, submissionTransportTestToken, "application/json", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("valid submission: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	assertSubmissionTransportWrites(t, db, 1, 0)
	if !strings.Contains(created.Body.String(), `"status":"pending"`) {
		t.Fatalf("created response must expose server-assigned pending status: %s", created.Body.String())
	}

	limited := performSubmissionTransportRequest(router, submissionTransportTestToken, "application/json", body)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("second attempt: expected 429, got %d: %s", limited.Code, limited.Body.String())
	}
	assertSubmissionTransportWrites(t, db, 1, 0)

	for _, entry := range observed.All() {
		serialized, err := json.Marshal(entry.ContextMap())
		if err != nil {
			t.Fatal(err)
		}
		logText := entry.Message + string(serialized)
		if strings.Contains(logText, submissionTransportTestToken) || strings.Contains(logText, "安全账户") {
			t.Fatalf("request log leaked credential or submission body: %s", logText)
		}
	}
}
