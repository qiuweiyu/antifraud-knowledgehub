package browserauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const exchangeRateScript = `
local source = redis.call("INCR", KEYS[1])
if source == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[3])
end
local global = redis.call("INCR", KEYS[2])
if global == 1 then
  redis.call("PEXPIRE", KEYS[2], ARGV[3])
end
if source > tonumber(ARGV[1]) or global > tonumber(ARGV[2]) then
  return 0
end
return 1
`

type ExchangeRateConfig struct {
	SourceLimit int64
	GlobalLimit int64
	Window      time.Duration
}

type ExchangeRateLimiter interface {
	Allow(ctx context.Context, source string, config ExchangeRateConfig) (bool, error)
}

type RedisExchangeRateLimiter struct {
	Client *redis.Client
}

func ValidateExchangeRateConfig(config ExchangeRateConfig) error {
	if config.SourceLimit <= 0 || config.GlobalLimit <= 0 || config.GlobalLimit < config.SourceLimit || config.Window < time.Millisecond {
		return errors.New("browser session exchange rate configuration is invalid")
	}
	return nil
}

func (l RedisExchangeRateLimiter) Allow(ctx context.Context, source string, config ExchangeRateConfig) (bool, error) {
	if l.Client == nil {
		return false, errors.New("browser session exchange Redis client is not configured")
	}
	if source == "" {
		return false, errors.New("browser session exchange source is required")
	}
	if err := ValidateExchangeRateConfig(config); err != nil {
		return false, err
	}
	digest := sha256.Sum256([]byte("afkh-browser-session-exchange-source-v1\x00" + source))
	sourceKey := "afkh:browser-session-exchange:rate:source:" + hex.EncodeToString(digest[:])
	globalKey := "afkh:browser-session-exchange:rate:global"
	windowMs := config.Window.Milliseconds()
	result, err := l.Client.Eval(ctx, exchangeRateScript, []string{sourceKey, globalKey}, config.SourceLimit, config.GlobalLimit, windowMs).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
