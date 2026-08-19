package browserauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const browserAssistedRateBackendTimeout = 500 * time.Millisecond

const browserAssistedRateScript = `
local principal = tonumber(redis.call("GET", KEYS[1]) or "0")
local global = tonumber(redis.call("GET", KEYS[2]) or "0")
if principal >= tonumber(ARGV[1]) or global >= tonumber(ARGV[2]) then
  return 0
end
principal = redis.call("INCR", KEYS[1])
if principal == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[3])
end
global = redis.call("INCR", KEYS[2])
if global == 1 then
  redis.call("PEXPIRE", KEYS[2], ARGV[3])
end
return 1
`

type BrowserAssistedRateConfig struct {
	PrincipalLimit int64
	GlobalLimit    int64
	Window         time.Duration
}

type BrowserAssistedRateBackend interface {
	Allow(ctx context.Context, principalID string, config BrowserAssistedRateConfig) (bool, error)
}

type RedisBrowserAssistedRateBackend struct {
	Client *redis.Client
}

func ValidateBrowserAssistedRateConfig(config BrowserAssistedRateConfig) error {
	if config.PrincipalLimit <= 0 || config.GlobalLimit <= 0 || config.GlobalLimit < config.PrincipalLimit || config.Window < time.Millisecond {
		return errors.New("browser assisted-analysis rate config is invalid")
	}
	return nil
}

func (b RedisBrowserAssistedRateBackend) Allow(ctx context.Context, principalID string, config BrowserAssistedRateConfig) (bool, error) {
	if b.Client == nil {
		return false, errors.New("browser assisted-analysis Redis client is not configured")
	}
	if principalID == "" {
		return false, errors.New("browser assisted-analysis principal is required")
	}
	if err := ValidateBrowserAssistedRateConfig(config); err != nil {
		return false, err
	}
	windowMs := config.Window.Milliseconds()
	if windowMs <= 0 {
		windowMs = 1
	}
	result, err := b.Client.Eval(
		ctx,
		browserAssistedRateScript,
		[]string{browserAssistedPrincipalRateKey(principalID), "afkh:browser-assisted:rate:global"},
		config.PrincipalLimit,
		config.GlobalLimit,
		windowMs,
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func BrowserAssistedCostLimit(backend BrowserAssistedRateBackend, config BrowserAssistedRateConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if backend == nil || !ok {
			response.Fail(c, http.StatusServiceUnavailable, "browser_cost_control_unavailable", "Browser assisted-analysis cost control unavailable")
			c.Abort()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), browserAssistedRateBackendTimeout)
		allowed, err := backend.Allow(ctx, principal.ID, config)
		cancel()
		if err != nil {
			response.Fail(c, http.StatusServiceUnavailable, "browser_cost_control_unavailable", "Browser assisted-analysis cost control unavailable")
			c.Abort()
			return
		}
		if !allowed {
			response.Fail(c, http.StatusTooManyRequests, "browser_assisted_rate_limited", "Browser assisted-analysis rate limit exceeded")
			c.Abort()
			return
		}
		c.Next()
	}
}

func browserAssistedPrincipalRateKey(principalID string) string {
	digest := sha256.Sum256([]byte("afkh-browser-assisted-principal-v1\x00" + principalID))
	return "afkh:browser-assisted:rate:principal:" + hex.EncodeToString(digest[:])
}
