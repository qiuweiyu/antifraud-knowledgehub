package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	submissionRateGlobalKey = "afkh:rule-submission:rate:global"
	submissionRateTimeout   = 500 * time.Millisecond
)

const submissionRateScript = `
local credential_count = redis.call('INCR', KEYS[1])
if credential_count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[3])
end
local global_count = redis.call('INCR', KEYS[2])
if global_count == 1 then
  redis.call('PEXPIRE', KEYS[2], ARGV[3])
end
if credential_count > tonumber(ARGV[1]) or global_count > tonumber(ARGV[2]) then
  return 0
end
return 1
`

type SubmissionRateConfig struct {
	CredentialLimit int64
	GlobalLimit     int64
	Window          time.Duration
}

func (c SubmissionRateConfig) validate() error {
	if c.CredentialLimit <= 0 {
		return fmt.Errorf("credential rate limit must be positive")
	}
	if c.GlobalLimit <= 0 {
		return fmt.Errorf("global rate limit must be positive")
	}
	if c.Window < time.Millisecond {
		return fmt.Errorf("rate-limit window must be at least one millisecond")
	}
	return nil
}

type SubmissionRateBackend interface {
	Allow(ctx context.Context, credentialKey string, credentialLimit, globalLimit int64, window time.Duration) (bool, error)
}

type RedisSubmissionRateBackend struct {
	Client *redis.Client
}

func (b RedisSubmissionRateBackend) Allow(ctx context.Context, credentialKey string, credentialLimit, globalLimit int64, window time.Duration) (bool, error) {
	if b.Client == nil {
		return false, fmt.Errorf("redis client is not configured")
	}
	result, err := b.Client.Eval(
		ctx,
		submissionRateScript,
		[]string{credentialKey, submissionRateGlobalKey},
		credentialLimit,
		globalLimit,
		window.Milliseconds(),
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func SubmissionWriteRateLimit(backend SubmissionRateBackend, verifiedCredential string, cfg SubmissionRateConfig) gin.HandlerFunc {
	configErr := cfg.validate()
	credentialKey := submissionCredentialRateKey(verifiedCredential)
	credentialMissing := strings.TrimSpace(verifiedCredential) == ""

	return func(c *gin.Context) {
		if backend == nil || configErr != nil || credentialMissing {
			response.Fail(c, http.StatusServiceUnavailable, "rate_limiter_unavailable", "submission rate limiter is unavailable")
			c.Abort()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), submissionRateTimeout)
		defer cancel()
		allowed, err := backend.Allow(ctx, credentialKey, cfg.CredentialLimit, cfg.GlobalLimit, cfg.Window)
		if err != nil {
			response.Fail(c, http.StatusServiceUnavailable, "rate_limiter_unavailable", "submission rate limiter is unavailable")
			c.Abort()
			return
		}
		if !allowed {
			response.Fail(c, http.StatusTooManyRequests, "rate_limited", "submission rate limit exceeded")
			c.Abort()
			return
		}
		c.Next()
	}
}

func submissionCredentialRateKey(credential string) string {
	digest := sha256.Sum256([]byte(credential))
	return "afkh:rule-submission:rate:credential:" + hex.EncodeToString(digest[:])
}
