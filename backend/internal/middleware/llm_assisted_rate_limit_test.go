package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type fakeLLMAssistedRateBackend struct {
	allowed      bool
	err          error
	calls        int
	credentialID string
	config       LLMAssistedRateConfig
}

func (f *fakeLLMAssistedRateBackend) Allow(_ context.Context, credentialID string, config LLMAssistedRateConfig) (bool, error) {
	f.calls++
	f.credentialID = credentialID
	f.config = config
	return f.allowed, f.err
}

func TestLLMAssistedAnalysisRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := LLMAssistedRateConfig{CredentialLimit: 10, GlobalLimit: 50, Window: time.Minute}

	tests := []struct {
		name       string
		backend    LLMAssistedRateBackend
		token      string
		wantStatus int
		wantCalled bool
	}{
		{name: "nil backend", token: testLLMAssistedHTTPToken, wantStatus: http.StatusServiceUnavailable},
		{name: "blank token", backend: &fakeLLMAssistedRateBackend{allowed: true}, wantStatus: http.StatusServiceUnavailable},
		{name: "backend error", backend: &fakeLLMAssistedRateBackend{err: errors.New("redis unavailable")}, token: testLLMAssistedHTTPToken, wantStatus: http.StatusServiceUnavailable},
		{name: "denied", backend: &fakeLLMAssistedRateBackend{allowed: false}, token: testLLMAssistedHTTPToken, wantStatus: http.StatusTooManyRequests},
		{name: "allowed", backend: &fakeLLMAssistedRateBackend{allowed: true}, token: testLLMAssistedHTTPToken, wantStatus: http.StatusOK, wantCalled: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.POST("/assisted", LLMAssistedAnalysisRateLimit(tc.backend, tc.token, config), func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/assisted", nil)
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if called != tc.wantCalled {
				t.Fatalf("handler called=%v want=%v", called, tc.wantCalled)
			}
			if strings.Contains(recorder.Body.String(), tc.token) && tc.token != "" {
				t.Fatal("rate-limit response reflected route token")
			}
		})
	}
}

func TestLLMAssistedAnalysisRateLimitUsesCredentialDigest(t *testing.T) {
	backend := &fakeLLMAssistedRateBackend{allowed: true}
	config := LLMAssistedRateConfig{CredentialLimit: 3, GlobalLimit: 5, Window: 2 * time.Minute}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/assisted", LLMAssistedAnalysisRateLimit(backend, testLLMAssistedHTTPToken, config), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/assisted", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if backend.calls != 1 {
		t.Fatalf("backend calls=%d", backend.calls)
	}
	if backend.credentialID == "" || backend.credentialID == testLLMAssistedHTTPToken {
		t.Fatalf("credential id must be a non-raw digest, got %q", backend.credentialID)
	}
	if strings.Contains(backend.credentialID, testLLMAssistedHTTPToken) {
		t.Fatal("credential digest contains raw token")
	}
	if backend.config != config {
		t.Fatalf("config=%+v want=%+v", backend.config, config)
	}
}

func TestRedisLLMAssistedRateBackendIntegration(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("AFKH_REDIS_INTEGRATION_ADDR"))
	if addr == "" {
		t.Skip("AFKH_REDIS_INTEGRATION_ADDR not configured")
	}

	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}

	credentialID := llmAssistedCredentialID("integration-assisted-token")
	credentialKey := "afkh:llm-assisted-analysis:rate:credential:" + credentialID
	globalKey := "afkh:llm-assisted-analysis:rate:global"
	ruleCredentialKey := "afkh:rule-submission:rate:credential:" + credentialID
	ruleGlobalKey := "afkh:rule-submission:rate:global"
	if err := client.Del(ctx, credentialKey, globalKey, ruleCredentialKey, ruleGlobalKey).Err(); err != nil {
		t.Fatalf("clean redis keys: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(), credentialKey, globalKey, ruleCredentialKey, ruleGlobalKey).Err()
	})

	backend := RedisLLMAssistedRateBackend{Client: client}
	config := LLMAssistedRateConfig{CredentialLimit: 2, GlobalLimit: 3, Window: time.Minute}
	for i, want := range []bool{true, true, false} {
		allowed, err := backend.Allow(ctx, credentialID, config)
		if err != nil {
			t.Fatalf("allow call %d: %v", i+1, err)
		}
		if allowed != want {
			t.Fatalf("allow call %d=%v want=%v", i+1, allowed, want)
		}
	}

	if exists, err := client.Exists(ctx, credentialKey, globalKey).Result(); err != nil || exists != 2 {
		t.Fatalf("LLM rate keys existence=%d err=%v", exists, err)
	}
	if exists, err := client.Exists(ctx, ruleCredentialKey, ruleGlobalKey).Result(); err != nil || exists != 0 {
		t.Fatalf("rule-submission namespace collision existence=%d err=%v", exists, err)
	}
}

func TestRedisLLMAssistedRateBackendGlobalLimit(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("AFKH_REDIS_INTEGRATION_ADDR"))
	if addr == "" {
		t.Skip("AFKH_REDIS_INTEGRATION_ADDR not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	ctx := context.Background()
	globalKey := "afkh:llm-assisted-analysis:rate:global"
	firstID := llmAssistedCredentialID("integration-global-a")
	secondID := llmAssistedCredentialID("integration-global-b")
	keys := []string{
		globalKey,
		"afkh:llm-assisted-analysis:rate:credential:" + firstID,
		"afkh:llm-assisted-analysis:rate:credential:" + secondID,
	}
	if err := client.Del(ctx, keys...).Err(); err != nil {
		t.Fatalf("clean redis keys: %v", err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), keys...).Err() })

	backend := RedisLLMAssistedRateBackend{Client: client}
	config := LLMAssistedRateConfig{CredentialLimit: 2, GlobalLimit: 2, Window: time.Minute}
	if allowed, err := backend.Allow(ctx, firstID, config); err != nil || !allowed {
		t.Fatalf("first global call allowed=%v err=%v", allowed, err)
	}
	if allowed, err := backend.Allow(ctx, secondID, config); err != nil || !allowed {
		t.Fatalf("second global call allowed=%v err=%v", allowed, err)
	}
	if allowed, err := backend.Allow(ctx, firstID, config); err != nil || allowed {
		t.Fatalf("third global call allowed=%v err=%v", allowed, err)
	}
}
