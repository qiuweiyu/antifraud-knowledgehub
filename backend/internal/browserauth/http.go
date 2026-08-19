package browserauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	sessionBackendTimeout = 500 * time.Millisecond
	exchangeBodyLimit     = 1024
	productionCookieName  = "__Host-afkh_browser_session"
	developmentCookieName = "afkh_browser_session_dev"
)

type SessionBackend interface {
	Create(ctx context.Context, principal Principal) (string, string, SessionRecord, error)
	Validate(ctx context.Context, rawToken string, registry *GrantRegistry) (SessionRecord, Principal, error)
	Delete(ctx context.Context, rawToken string) error
}

type SessionHTTPConfig struct {
	Origin         string
	Production     bool
	TrustedProxies []*net.IPNet
	ExchangeRate   ExchangeRateConfig
}

type SessionHTTPHandler struct {
	Registry *GrantRegistry
	Sessions SessionBackend
	Limiter  ExchangeRateLimiter
	Config   SessionHTTPConfig
}

type sessionResponse struct {
	PrincipalID string    `json:"principal_id"`
	DisplayName string    `json:"display_label,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	CSRFToken   string    `json:"csrf_token"`
}

func (h *SessionHTTPHandler) Register(group *gin.RouterGroup) {
	group.POST("/browser/session/exchange", h.exchange)
	group.GET("/browser/session", h.status)
	group.POST("/browser/session/logout", h.logout)
}

func (h *SessionHTTPHandler) exchange(c *gin.Context) {
	noStore(c)
	if h == nil || h.Registry == nil || h.Sessions == nil || h.Limiter == nil {
		response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
		return
	}
	if !h.validStateChangingOrigin(c) {
		response.Fail(c, http.StatusForbidden, "browser_origin_rejected", "Browser origin rejected")
		return
	}
	if !isExactJSONContentType(c.GetHeader("Content-Type")) {
		response.Fail(c, http.StatusUnsupportedMediaType, "invalid_content_type", "Content-Type must be application/json")
		return
	}
	var request struct {
		AccessGrant string `json:"access_grant"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_request", "Invalid request")
		return
	}
	source, err := ClientSource(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"), h.Config.TrustedProxies)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_request", "Invalid request")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), sessionBackendTimeout)
	allowed, err := h.Limiter.Allow(ctx, source, h.Config.ExchangeRate)
	cancel()
	if err != nil {
		response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
		return
	}
	if !allowed {
		response.Fail(c, http.StatusTooManyRequests, "rate_limited", "Rate limit exceeded")
		return
	}
	principal, ok := h.Registry.Verify(request.AccessGrant)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, "browser_authentication_failed", "Browser authentication failed")
		return
	}
	ctx, cancel = context.WithTimeout(c.Request.Context(), sessionBackendTimeout)
	rawToken, csrfToken, record, err := h.Sessions.Create(ctx, principal)
	cancel()
	if err != nil {
		response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
		return
	}
	h.setSessionCookie(c, rawToken, time.Unix(record.ExpiresUnix, 0).UTC())
	response.OK(c, sessionResponse{
		PrincipalID: principal.ID,
		DisplayName: principal.DisplayLabel,
		ExpiresAt:   time.Unix(record.ExpiresUnix, 0).UTC(),
		CSRFToken:   csrfToken,
	})
}

func (h *SessionHTTPHandler) status(c *gin.Context) {
	noStore(c)
	if h == nil || h.Registry == nil || h.Sessions == nil {
		response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
		return
	}
	rawToken, ok := h.sessionCookie(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, "browser_session_invalid", "Browser session invalid or expired")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), sessionBackendTimeout)
	record, principal, err := h.Sessions.Validate(ctx, rawToken, h.Registry)
	cancel()
	if err != nil {
		h.writeSessionValidationError(c, err)
		return
	}
	response.OK(c, sessionResponse{
		PrincipalID: principal.ID,
		DisplayName: principal.DisplayLabel,
		ExpiresAt:   time.Unix(record.ExpiresUnix, 0).UTC(),
		CSRFToken:   CSRFToken(rawToken),
	})
}

func (h *SessionHTTPHandler) logout(c *gin.Context) {
	noStore(c)
	if h == nil || h.Registry == nil || h.Sessions == nil {
		response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
		return
	}
	if !h.validStateChangingOrigin(c) {
		response.Fail(c, http.StatusForbidden, "browser_origin_rejected", "Browser origin rejected")
		return
	}
	rawToken, ok := h.sessionCookie(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, "browser_session_invalid", "Browser session invalid or expired")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), sessionBackendTimeout)
	_, _, err := h.Sessions.Validate(ctx, rawToken, h.Registry)
	cancel()
	if err != nil {
		h.writeSessionValidationError(c, err)
		return
	}
	if !ValidCSRF(rawToken, c.GetHeader("X-AFKH-CSRF")) {
		response.Fail(c, http.StatusForbidden, "browser_csrf_rejected", "CSRF validation failed")
		return
	}
	ctx, cancel = context.WithTimeout(c.Request.Context(), sessionBackendTimeout)
	err = h.Sessions.Delete(ctx, rawToken)
	cancel()
	if err != nil {
		response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
		return
	}
	h.clearSessionCookie(c)
	response.OK(c, gin.H{"logged_out": true})
}

func (h *SessionHTTPHandler) validStateChangingOrigin(c *gin.Context) bool {
	return h != nil && h.Config.Origin != "" && c.GetHeader("Origin") == h.Config.Origin
}

func (h *SessionHTTPHandler) sessionCookie(c *gin.Context) (string, bool) {
	cookie, err := c.Request.Cookie(h.cookieName())
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

func (h *SessionHTTPHandler) cookieName() string {
	origin, err := url.Parse(h.Config.Origin)
	if h.Config.Production || (err == nil && origin.Scheme == "https") {
		return productionCookieName
	}
	return developmentCookieName
}

func (h *SessionHTTPHandler) cookieSecure() bool {
	origin, err := url.Parse(h.Config.Origin)
	return h.Config.Production || (err == nil && origin.Scheme == "https")
}

func (h *SessionHTTPHandler) setSessionCookie(c *gin.Context, value string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.cookieName(),
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(SessionTTL.Seconds()),
		Secure:   h.cookieSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *SessionHTTPHandler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.cookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		Secure:   h.cookieSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *SessionHTTPHandler) writeSessionValidationError(c *gin.Context, err error) {
	if errors.Is(err, ErrInvalidSession) || errors.Is(err, ErrRevokedSession) {
		response.Fail(c, http.StatusUnauthorized, "browser_session_invalid", "Browser session invalid or expired")
		return
	}
	response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
}

func decodeStrictJSON(c *gin.Context, destination any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, exchangeBodyLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("request body has trailing JSON")
	}
	return nil
}

func isExactJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	return err == nil && mediaType == "application/json"
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}
