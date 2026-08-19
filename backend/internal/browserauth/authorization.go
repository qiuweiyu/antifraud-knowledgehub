package browserauth

import (
	"context"
	"net/http"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

const browserPrincipalContextKey = "afkh.browser.principal"

// RequireSession validates the opaque browser session before downstream work.
// State-changing routes additionally require the frozen exact-Origin and CSRF
// checks before the principal is made available to later middleware/handlers.
func (h *SessionHTTPHandler) RequireSession(stateChanging bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		noStore(c)
		if h == nil || h.Registry == nil || h.Sessions == nil {
			response.Fail(c, http.StatusServiceUnavailable, "browser_session_backend_unavailable", "Browser session service unavailable")
			c.Abort()
			return
		}
		if stateChanging && !h.validStateChangingOrigin(c) {
			response.Fail(c, http.StatusForbidden, "browser_origin_rejected", "Browser origin rejected")
			c.Abort()
			return
		}
		rawToken, ok := h.sessionCookie(c)
		if !ok {
			response.Fail(c, http.StatusUnauthorized, "browser_session_invalid", "Browser session invalid or expired")
			c.Abort()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), sessionBackendTimeout)
		_, principal, err := h.Sessions.Validate(ctx, rawToken, h.Registry)
		cancel()
		if err != nil {
			h.writeSessionValidationError(c, err)
			c.Abort()
			return
		}
		if stateChanging && !ValidCSRF(rawToken, c.GetHeader("X-AFKH-CSRF")) {
			response.Fail(c, http.StatusForbidden, "browser_csrf_rejected", "CSRF validation failed")
			c.Abort()
			return
		}
		c.Set(browserPrincipalContextKey, principal)
		c.Next()
	}
}

func PrincipalFromContext(c *gin.Context) (Principal, bool) {
	if c == nil {
		return Principal{}, false
	}
	value, ok := c.Get(browserPrincipalContextKey)
	if !ok {
		return Principal{}, false
	}
	principal, ok := value.(Principal)
	if !ok || principal.ID == "" || principal.Generation == 0 {
		return Principal{}, false
	}
	return principal, true
}
