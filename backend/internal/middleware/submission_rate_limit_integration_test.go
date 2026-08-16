package middleware

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisIntegrationAddrEnv = "AFKH_REDIS_INTEGRATION_ADDR"

func integrationRedisClient(t *testing.T) *redis.Client {
	t.Helper()

	addr := os.Getenv(redisIntegrationAddrEnv)
	if addr == "" {
		t.Skipf("%s is not configured", redisIntegrationAddrEnv)
	}

	client := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("ping integration Redis: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func resetIntegrationRateKeys(t *testing.T, client *redis.Client, credentialKeys ...string) {
	t.Helper()

	keys := append([]string{submissionRateGlobalKey}, credentialKeys...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Del(ctx, keys...).Err(); err != nil {
		t.Fatalf("reset integration Redis keys: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.Del(ctx, keys...).Err()
	})
}

func TestRedisSubmissionRateBackendIntegrationCredentialLimitAndWindowExpiry(t *testing.T) {
	client := integrationRedisClient(t)
	backend := RedisSubmissionRateBackend{Client: client}
	credentialKey := submissionCredentialRateKey("integration-credential-window")
	resetIntegrationRateKeys(t, client, credentialKey)

	const credentialLimit int64 = 2
	const globalLimit int64 = 20
	window := 300 * time.Millisecond

	ctx := context.Background()
	for attempt := 1; attempt <= 2; attempt++ {
		allowed, err := backend.Allow(ctx, credentialKey, credentialLimit, globalLimit, window)
		if err != nil {
			t.Fatalf("allow attempt %d: %v", attempt, err)
		}
		if !allowed {
			t.Fatalf("attempt %d unexpectedly rejected", attempt)
		}
	}

	allowed, err := backend.Allow(ctx, credentialKey, credentialLimit, globalLimit, window)
	if err != nil {
		t.Fatalf("limited attempt: %v", err)
	}
	if allowed {
		t.Fatal("expected third credential attempt to be rate limited")
	}

	credentialTTL, err := client.PTTL(ctx, credentialKey).Result()
	if err != nil {
		t.Fatalf("credential PTTL: %v", err)
	}
	if credentialTTL <= 0 || credentialTTL > window {
		t.Fatalf("credential TTL = %v, want > 0 and <= %v", credentialTTL, window)
	}

	globalTTL, err := client.PTTL(ctx, submissionRateGlobalKey).Result()
	if err != nil {
		t.Fatalf("global PTTL: %v", err)
	}
	if globalTTL <= 0 || globalTTL > window {
		t.Fatalf("global TTL = %v, want > 0 and <= %v", globalTTL, window)
	}

	time.Sleep(window + 150*time.Millisecond)

	allowed, err = backend.Allow(ctx, credentialKey, credentialLimit, globalLimit, window)
	if err != nil {
		t.Fatalf("allow after window expiry: %v", err)
	}
	if !allowed {
		t.Fatal("expected credential to be allowed after the Redis window expired")
	}
}

func TestRedisSubmissionRateBackendIntegrationGlobalLimitAcrossCredentials(t *testing.T) {
	client := integrationRedisClient(t)
	backend := RedisSubmissionRateBackend{Client: client}
	keyA := submissionCredentialRateKey("integration-global-a")
	keyB := submissionCredentialRateKey("integration-global-b")
	keyC := submissionCredentialRateKey("integration-global-c")
	resetIntegrationRateKeys(t, client, keyA, keyB, keyC)

	const credentialLimit int64 = 10
	const globalLimit int64 = 2
	window := 2 * time.Second

	ctx := context.Background()
	for index, key := range []string{keyA, keyB} {
		allowed, err := backend.Allow(ctx, key, credentialLimit, globalLimit, window)
		if err != nil {
			t.Fatalf("global allow attempt %d: %v", index+1, err)
		}
		if !allowed {
			t.Fatalf("global attempt %d unexpectedly rejected", index+1)
		}
	}

	allowed, err := backend.Allow(ctx, keyC, credentialLimit, globalLimit, window)
	if err != nil {
		t.Fatalf("global limited attempt: %v", err)
	}
	if allowed {
		t.Fatal("expected third request across credentials to hit the global limit")
	}

	globalTTL, err := client.PTTL(ctx, submissionRateGlobalKey).Result()
	if err != nil {
		t.Fatalf("global PTTL: %v", err)
	}
	if globalTTL <= 0 || globalTTL > window {
		t.Fatalf("global TTL = %v, want > 0 and <= %v", globalTTL, window)
	}
}
