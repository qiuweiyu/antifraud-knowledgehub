package browserauth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func browserAuthIntegrationRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("AFKH_REDIS_INTEGRATION_ADDR")
	if addr == "" {
		t.Skip("AFKH_REDIS_INTEGRATION_ADDR is not configured")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping integration Redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestSessionStoreRedisLifecycleAndGenerationRevocation(t *testing.T) {
	client := browserAuthIntegrationRedis(t)
	rawGrant := testRawGrant(7)
	registry, err := NewGrantRegistry([]GrantDefinition{testGrantDefinition("beta", 1, rawGrant, true)})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	fixedNow := time.Unix(1_800_000_000, 0).UTC()
	store := SessionStore{Client: client, Random: bytes.NewReader(bytesRepeat(9, sessionEntropy)), Now: func() time.Time { return fixedNow }}
	principal, _ := registry.Resolve("beta")
	rawSession, csrf, record, err := store.Create(context.Background(), principal)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	key := sessionRedisKey(rawSession)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })
	if rawSession == "" || csrf == "" || !ValidCSRF(rawSession, csrf) {
		t.Fatal("session or CSRF token invalid")
	}
	if record.ExpiresUnix-record.IssuedUnix != int64(SessionTTL/time.Second) {
		t.Fatalf("session lifetime=%d", record.ExpiresUnix-record.IssuedUnix)
	}
	if strings.Contains(key, rawSession) {
		t.Fatal("Redis key must not contain raw session token")
	}
	got, gotPrincipal, err := store.Validate(context.Background(), rawSession, registry)
	if err != nil || got.PrincipalID != "beta" || gotPrincipal.Generation != 1 {
		t.Fatalf("validate record=%+v principal=%+v err=%v", got, gotPrincipal, err)
	}
	revokedRegistry, err := NewGrantRegistry([]GrantDefinition{testGrantDefinition("beta", 2, rawGrant, true)})
	if err != nil {
		t.Fatalf("revoked registry: %v", err)
	}
	if _, _, err := store.Validate(context.Background(), rawSession, revokedRegistry); !errors.Is(err, ErrRevokedSession) {
		t.Fatalf("expected generation revocation, got %v", err)
	}
	if err := store.Delete(context.Background(), rawSession); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, _, err := store.Validate(context.Background(), rawSession, registry); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("deleted session should be invalid, got %v", err)
	}
}

func TestRedisExchangeRateLimiterFailsClosedAtLimits(t *testing.T) {
	client := browserAuthIntegrationRedis(t)
	limiter := RedisExchangeRateLimiter{Client: client}
	cfg := ExchangeRateConfig{SourceLimit: 1, GlobalLimit: 2, Window: 2 * time.Second}
	ctx := context.Background()
	allowed, err := limiter.Allow(ctx, "198.51.100.20", cfg)
	if err != nil || !allowed {
		t.Fatalf("first allow=%v err=%v", allowed, err)
	}
	allowed, err = limiter.Allow(ctx, "198.51.100.20", cfg)
	if err != nil || allowed {
		t.Fatalf("second same-source allow=%v err=%v", allowed, err)
	}
}
