package middleware

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

const llmAssistedRateBackendTimeout = 500 * time.Millisecond

const llmAssistedRateScript = `
local credential = redis.call("INCR", KEYS[1])
if credential == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[3])
end
local global = redis.call("INCR", KEYS[2])
if global == 1 then
  redis.call("PEXPIRE", KEYS[2], ARGV[3])
end
if credential > tonumber(ARGV[1]) or global > tonumber(ARGV[2]) then
  return 0
end
return 1
`

type LLMAssistedRateConfig struct {
	CredentialLimit int64
	GlobalLimit     int64
	Window          time.Duration
}

type LLMAssistedRateBackend interface {
	Allow(ctx context.Context, credentialID string, config LLMAssistedRateConfig) (bool, error)
}

type RedisLLMAssistedRateBackend struct {
	Client *redis.Client
}

func (b RedisLLMAssistedRateBackend) Allow(ctx context.Context, credentialID string, config LLMAssistedRateConfig) (bool, error) {
	if b.Client == nil {
		return false, errors.New("LLM assisted-analysis Redis client is not configured")
	}
	if credentialID == "" {
		return false, errors.New("LLM assisted-analysis credential id is required")
	}
	if config.CredentialLimit <= 0 || config.GlobalLimit <= 0 || config.GlobalLimit < config.CredentialLimit || config.Window <= 0 {
		return false, errors.New("LLM assisted-analysis rate config is invalid")
	}

	credentialKey := "afkh:llm-assisted-analysis:rate:credential:" + credentialID
	globalKey := "afkh:llm-assisted-analysis:rate:global"
	windowMs := config.Window.Milliseconds()
	if windowMs <= 0 {
		windowMs = 1
	}
	result, err := b.Client.Eval(ctx, llmAssistedRateScript, []string{credentialKey, globalKey}, config.CredentialLimit, config.GlobalLimit, windowMs).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func LLMAssistedAnalysisRateLimit(backend LLMAssistedRateBackend, credentialToken string, config LLMAssistedRateConfig) gin.HandlerFunc {
	credentialID := llmAssistedCredentialID(credentialToken)
	return func(c *gin.Context) {
		if backend == nil || credentialID == "" {
			response.Fail(c, http.StatusServiceUnavailable, "rate_limiter_unavailable", "Rate limiter unavailable")
			c.Abort()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), llmAssistedRateBackendTimeout)
		defer cancel()
		allowed, err := backend.Allow(ctx, credentialID, config)
		if err != nil {
			response.Fail(c, http.StatusServiceUnavailable, "rate_limiter_unavailable", "Rate limiter unavailable")
			c.Abort()
			return
		}
		if !allowed {
			response.Fail(c, http.StatusTooManyRequests, "rate_limited", "Rate limit exceeded")
			c.Abort()
			return
		}
		c.Next()
	}
}

func llmAssistedCredentialID(token string) string {
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
