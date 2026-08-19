package browserauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testAuthorizationHandler(t *testing.T) (*SessionHTTPHandler, *fakeSessionBackend) {
	t.Helper()
	rawGrant := testRawGrant(11)
	registry, err := NewGrantRegistry([]GrantDefinition{testGrantDefinition("beta-user", 3, rawGrant, true)})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	principal, _ := registry.Resolve("beta-user")
	sessions := &fakeSessionBackend{
		token:     testSessionToken(),
		principal: principal,
		record: SessionRecord{
			PrincipalID:         principal.ID,
			PrincipalGeneration: principal.Generation,
			IssuedUnix:          time.Now().Add(-time.Minute).Unix(),
			ExpiresUnix:         time.Now().Add(time.Hour).Unix(),
		},
	}
	return &SessionHTTPHandler{
		Registry: registry,
		Sessions: sessions,
		Config: SessionHTTPConfig{
			Origin:     "https://antifraud.example",
			Production: true,
		},
	}, sessions
}

func TestRequireSessionStopsRejectedRequestsBeforeDownstreamWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		origin      string
		cookie      bool
		csrf        string
		wantStatus  int
		wantValidate int
	}{
		{name: "unauthenticated", origin: "https://antifraud.example", cookie: false, csrf: "", wantStatus: http.StatusUnauthorized, wantValidate: 0},
		{name: "bad origin", origin: "https://evil.example", cookie: true, csrf: "", wantStatus: http.StatusForbidden, wantValidate: 0},
		{name: "bad csrf", origin: "https://antifraud.example", cookie: true, csrf: "bad", wantStatus: http.StatusForbidden, wantValidate: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, sessions := testAuthorizationHandler(t)
			router := gin.New()
			downstream := 0
			router.POST("/protected", handler.RequireSession(true), func(c *gin.Context) {
				downstream++
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, "/protected", nil)
			req.Header.Set("Origin", tc.origin)
			if tc.csrf != "" {
				req.Header.Set("X-AFKH-CSRF", tc.csrf)
			}
			if tc.cookie {
				req.AddCookie(&http.Cookie{Name: productionCookieName, Value: sessions.token})
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.wantStatus || downstream != 0 || sessions.validate != tc.wantValidate {
				t.Fatalf("status=%d downstream=%d validate=%d", recorder.Code, downstream, sessions.validate)
			}
		})
	}
}

func TestRequireSessionProvidesServerResolvedPrincipalAfterCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, sessions := testAuthorizationHandler(t)
	router := gin.New()
	router.POST("/protected", handler.RequireSession(true), func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if !ok || principal.ID != "beta-user" || principal.Generation != 3 {
			t.Fatalf("unexpected principal: %+v ok=%v", principal, ok)
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.Header.Set("Origin", "https://antifraud.example")
	req.Header.Set("X-AFKH-CSRF", CSRFToken(sessions.token))
	req.AddCookie(&http.Cookie{Name: productionCookieName, Value: sessions.token})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || sessions.validate != 1 {
		t.Fatalf("status=%d validate=%d body=%s", recorder.Code, sessions.validate, recorder.Body.String())
	}
}

type fakeBrowserAssistedRateBackend struct {
	calls       int
	principalID string
	allowed     bool
	err         error
}

func (f *fakeBrowserAssistedRateBackend) Allow(_ context.Context, principalID string, _ BrowserAssistedRateConfig) (bool, error) {
	f.calls++
	f.principalID = principalID
	return f.allowed, f.err
}

func TestBrowserAssistedCostLimitUsesServerPrincipalAndFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := BrowserAssistedRateConfig{PrincipalLimit: 2, GlobalLimit: 10, Window: time.Minute}
	tests := []struct {
		name       string
		backend    *fakeBrowserAssistedRateBackend
		principal bool
		wantStatus int
		wantNext   int
	}{
		{name: "missing principal", backend: &fakeBrowserAssistedRateBackend{allowed: true}, principal: false, wantStatus: http.StatusServiceUnavailable, wantNext: 0},
		{name: "redis error", backend: &fakeBrowserAssistedRateBackend{err: errors.New("redis unavailable")}, principal: true, wantStatus: http.StatusServiceUnavailable, wantNext: 0},
		{name: "denied", backend: &fakeBrowserAssistedRateBackend{allowed: false}, principal: true, wantStatus: http.StatusTooManyRequests, wantNext: 0},
		{name: "allowed", backend: &fakeBrowserAssistedRateBackend{allowed: true}, principal: true, wantStatus: http.StatusNoContent, wantNext: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			if tc.principal {
				router.Use(func(c *gin.Context) {
					c.Set(browserPrincipalContextKey, Principal{ID: "beta-user", Generation: 1})
					c.Next()
				})
			}
			next := 0
			router.POST("/assisted", BrowserAssistedCostLimit(tc.backend, cfg), func(c *gin.Context) {
				next++
				c.Status(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/assisted", nil))
			if recorder.Code != tc.wantStatus || next != tc.wantNext {
				t.Fatalf("status=%d next=%d body=%s", recorder.Code, next, recorder.Body.String())
			}
			if tc.principal && tc.backend.calls != 1 {
				t.Fatalf("rate backend calls=%d", tc.backend.calls)
			}
			if tc.backend.calls == 1 && tc.backend.principalID != "beta-user" {
				t.Fatalf("principal id=%q", tc.backend.principalID)
			}
		})
	}
}
