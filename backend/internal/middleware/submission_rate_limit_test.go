package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type fakeSubmissionRateBackend struct {
	allowed bool
	err     error
	calls   int
	keys    []string
}

func (f *fakeSubmissionRateBackend) Allow(_ context.Context, credentialKey string, _, _ int64, _ time.Duration) (bool, error) {
	f.calls++
	f.keys = append(f.keys, credentialKey)
	return f.allowed, f.err
}

func testSubmissionRateConfig() SubmissionRateConfig {
	return SubmissionRateConfig{CredentialLimit: 5, GlobalLimit: 50, Window: 10 * time.Minute}
}

func runRateLimitedRequest(t *testing.T, middleware gin.HandlerFunc, headers map[string]string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlerCalled := false
	router.GET("/probe", middleware, func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder, handlerCalled
}

func decodeRateLimitError(t *testing.T, recorder *httptest.ResponseRecorder) response.Envelope {
	t.Helper()
	var payload response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

func TestSubmissionWriteRateLimitAllowsRequest(t *testing.T) {
	backend := &fakeSubmissionRateBackend{allowed: true}
	recorder, called := runRateLimitedRequest(t, SubmissionWriteRateLimit(backend, "verified-write-token-abcdefghijklmnopqrstuvwxyz", testSubmissionRateConfig()), nil)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if !called {
		t.Fatal("expected downstream handler to run")
	}
	if backend.calls != 1 {
		t.Fatalf("expected one limiter call, got %d", backend.calls)
	}
}

func TestSubmissionWriteRateLimitRejectsExceededLimit(t *testing.T) {
	backend := &fakeSubmissionRateBackend{allowed: false}
	recorder, called := runRateLimitedRequest(t, SubmissionWriteRateLimit(backend, "verified-write-token-abcdefghijklmnopqrstuvwxyz", testSubmissionRateConfig()), nil)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", recorder.Code)
	}
	if called {
		t.Fatal("downstream handler must not run when rate limited")
	}
	payload := decodeRateLimitError(t, recorder)
	if payload.Error == nil || payload.Error.Code != "rate_limited" {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}

func TestSubmissionWriteRateLimitFailsClosedOnBackendError(t *testing.T) {
	backend := &fakeSubmissionRateBackend{err: errors.New("redis unavailable")}
	recorder, called := runRateLimitedRequest(t, SubmissionWriteRateLimit(backend, "verified-write-token-abcdefghijklmnopqrstuvwxyz", testSubmissionRateConfig()), nil)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
	if called {
		t.Fatal("downstream handler must not run when limiter backend fails")
	}
	payload := decodeRateLimitError(t, recorder)
	if payload.Error == nil || payload.Error.Code != "rate_limiter_unavailable" {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}

func TestSubmissionWriteRateLimitFailsClosedOnInvalidConfiguration(t *testing.T) {
	backend := &fakeSubmissionRateBackend{allowed: true}
	cases := []struct {
		name       string
		credential string
		cfg        SubmissionRateConfig
	}{
		{name: "blank credential", credential: "   ", cfg: testSubmissionRateConfig()},
		{name: "zero credential limit", credential: "verified-write-token-abcdefghijklmnopqrstuvwxyz", cfg: SubmissionRateConfig{CredentialLimit: 0, GlobalLimit: 50, Window: 10 * time.Minute}},
		{name: "zero global limit", credential: "verified-write-token-abcdefghijklmnopqrstuvwxyz", cfg: SubmissionRateConfig{CredentialLimit: 5, GlobalLimit: 0, Window: 10 * time.Minute}},
		{name: "invalid window", credential: "verified-write-token-abcdefghijklmnopqrstuvwxyz", cfg: SubmissionRateConfig{CredentialLimit: 5, GlobalLimit: 50, Window: 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend.calls = 0
			recorder, called := runRateLimitedRequest(t, SubmissionWriteRateLimit(backend, tc.credential, tc.cfg), nil)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", recorder.Code)
			}
			if called {
				t.Fatal("downstream handler must not run for invalid limiter configuration")
			}
			if backend.calls != 0 {
				t.Fatalf("backend must not be called for invalid configuration, got %d calls", backend.calls)
			}
		})
	}
}

func TestSubmissionCredentialRateKeyIsStableAndDoesNotExposeToken(t *testing.T) {
	token := "verified-write-token-abcdefghijklmnopqrstuvwxyz"
	first := submissionCredentialRateKey(token)
	second := submissionCredentialRateKey(token)
	other := submissionCredentialRateKey(token + "-other")
	if first != second {
		t.Fatal("expected stable credential key")
	}
	if first == other {
		t.Fatal("different credentials must produce different rate keys")
	}
	if strings.Contains(first, token) {
		t.Fatal("rate key must not contain the raw credential")
	}
}

func TestSubmissionWriteRateLimitIgnoresForwardedFor(t *testing.T) {
	backend := &fakeSubmissionRateBackend{allowed: true}
	middleware := SubmissionWriteRateLimit(backend, "verified-write-token-abcdefghijklmnopqrstuvwxyz", testSubmissionRateConfig())
	for _, forwardedFor := range []string{"198.51.100.10", "203.0.113.99"} {
		recorder, called := runRateLimitedRequest(t, middleware, map[string]string{"X-Forwarded-For": forwardedFor})
		if recorder.Code != http.StatusNoContent || !called {
			t.Fatalf("expected allowed request for forwarded value %q", forwardedFor)
		}
	}
	if len(backend.keys) != 2 || backend.keys[0] != backend.keys[1] {
		t.Fatalf("forwarded headers must not affect credential rate key: %+v", backend.keys)
	}
}

func TestRedisSubmissionRateBackendReturnsErrorWhenRedisIsUnavailable(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:       "unused",
		MaxRetries: 0,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("redis unavailable")
		},
	})
	defer client.Close()

	backend := RedisSubmissionRateBackend{Client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := backend.Allow(ctx, "credential-key", 5, 50, 10*time.Minute); err == nil {
		t.Fatal("expected Redis backend failure")
	}
}

func TestRedisSubmissionRateBackendRejectsNilClient(t *testing.T) {
	backend := RedisSubmissionRateBackend{}
	if _, err := backend.Allow(context.Background(), "credential-key", 5, 50, 10*time.Minute); err == nil {
		t.Fatal("expected nil Redis client failure")
	}
}
