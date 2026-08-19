package browserauth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type fakeSessionBackend struct {
	created   int
	deleted   int
	validate  int
	token     string
	principal Principal
	record    SessionRecord
	err       error
}

func (f *fakeSessionBackend) Create(context.Context, Principal) (string, string, SessionRecord, error) {
	f.created++
	if f.err != nil {
		return "", "", SessionRecord{}, f.err
	}
	return f.token, CSRFToken(f.token), f.record, nil
}

func (f *fakeSessionBackend) Validate(context.Context, string, *GrantRegistry) (SessionRecord, Principal, error) {
	f.validate++
	return f.record, f.principal, f.err
}

func (f *fakeSessionBackend) Delete(context.Context, string) error {
	f.deleted++
	return f.err
}

type fakeExchangeLimiter struct {
	calls   int
	source  string
	allowed bool
	err     error
}

func (f *fakeExchangeLimiter) Allow(_ context.Context, source string, _ ExchangeRateConfig) (bool, error) {
	f.calls++
	f.source = source
	return f.allowed, f.err
}

func testSessionToken() string {
	return sessionTokenPrefix + base64.RawURLEncoding.EncodeToString(bytesRepeat(8, sessionEntropy))
}

func testHTTPHandler(t *testing.T, production bool) (*gin.Engine, *fakeSessionBackend, *fakeExchangeLimiter, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rawGrant := testRawGrant(6)
	registry, err := NewGrantRegistry([]GrantDefinition{testGrantDefinition("beta-user", 1, rawGrant, true)})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	principal, _ := registry.Resolve("beta-user")
	now := time.Unix(1_800_000_000, 0).UTC()
	sessions := &fakeSessionBackend{
		token:     testSessionToken(),
		principal: principal,
		record: SessionRecord{
			PrincipalID:         principal.ID,
			PrincipalGeneration: principal.Generation,
			IssuedUnix:          now.Unix(),
			ExpiresUnix:         now.Add(SessionTTL).Unix(),
		},
	}
	limiter := &fakeExchangeLimiter{allowed: true}
	origin := "https://antifraud.example"
	if !production {
		origin = "http://127.0.0.1:5173"
	}
	handler := &SessionHTTPHandler{
		Registry: registry,
		Sessions: sessions,
		Limiter:  limiter,
		Config: SessionHTTPConfig{
			Origin:       origin,
			Production:   production,
			ExchangeRate: ExchangeRateConfig{SourceLimit: 5, GlobalLimit: 50, Window: time.Minute},
		},
	}
	router := gin.New()
	handler.Register(router.Group("/api/v1"))
	return router, sessions, limiter, rawGrant
}

func TestSessionExchangeSetsProductionCookieAndIgnoresUntrustedForwardedIP(t *testing.T) {
	router, sessions, limiter, rawGrant := testHTTPHandler(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/browser/session/exchange", strings.NewReader(`{"access_grant":"`+rawGrant+`"}`))
	request.RemoteAddr = "203.0.113.10:4567"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://antifraud.example")
	request.Header.Set("X-Forwarded-For", "198.51.100.99")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if sessions.created != 1 || limiter.calls != 1 || limiter.source != "203.0.113.10" {
		t.Fatalf("created=%d limiter=%d source=%q", sessions.created, limiter.calls, limiter.source)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != productionCookieName || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected production cookie: %+v", cookie)
	}
	if strings.Contains(recorder.Body.String(), rawGrant) {
		t.Fatal("response must not expose raw grant")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session response must be no-store")
	}
}

func TestSessionExchangeRejectsOriginBeforeLimiterOrGrantWork(t *testing.T) {
	router, sessions, limiter, rawGrant := testHTTPHandler(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/browser/session/exchange", strings.NewReader(`{"access_grant":"`+rawGrant+`"}`))
	request.RemoteAddr = "203.0.113.10:4567"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || limiter.calls != 0 || sessions.created != 0 {
		t.Fatalf("status=%d limiter=%d created=%d", recorder.Code, limiter.calls, sessions.created)
	}
}

func TestLogoutRequiresValidCSRFBeforeDelete(t *testing.T) {
	router, sessions, _, _ := testHTTPHandler(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/browser/session/logout", nil)
	request.Header.Set("Origin", "https://antifraud.example")
	request.Header.Set("X-AFKH-CSRF", "invalid")
	request.AddCookie(&http.Cookie{Name: productionCookieName, Value: sessions.token})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || sessions.validate != 1 || sessions.deleted != 0 {
		t.Fatalf("status=%d validate=%d deleted=%d", recorder.Code, sessions.validate, sessions.deleted)
	}
}

func TestSessionBackendFailureIsFailClosed(t *testing.T) {
	router, sessions, _, _ := testHTTPHandler(t, true)
	sessions.err = errors.New("redis unavailable")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/browser/session", nil)
	request.AddCookie(&http.Cookie{Name: productionCookieName, Value: sessions.token})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
