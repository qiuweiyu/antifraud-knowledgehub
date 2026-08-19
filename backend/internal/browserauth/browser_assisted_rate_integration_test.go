package browserauth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRedisBrowserAssistedRateBackendPrincipalGlobalAndNamespaceIsolation(t *testing.T) {
	client := browserAuthIntegrationRedis(t)
	backend := RedisBrowserAssistedRateBackend{Client: client}
	ctx := context.Background()
	principalA := "beta-a"
	principalB := "beta-b"
	principalKeyA := browserAssistedPrincipalRateKey(principalA)
	principalKeyB := browserAssistedPrincipalRateKey(principalB)
	globalKey := "afkh:browser-assisted:rate:global"
	operatorSentinelKey := "afkh:llm-assisted-analysis:rate:global"
	if strings.Contains(principalKeyA, principalA) {
		t.Fatal("browser assisted Redis key must not expose principal id")
	}
	if err := client.Del(ctx, principalKeyA, principalKeyB, globalKey, operatorSentinelKey).Err(); err != nil {
		t.Fatalf("reset rate keys: %v", err)
	}
	if err := client.Set(ctx, operatorSentinelKey, "41", time.Minute).Err(); err != nil {
		t.Fatalf("seed operator limiter sentinel: %v", err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), principalKeyA, principalKeyB, globalKey, operatorSentinelKey).Err() })

	cfg := BrowserAssistedRateConfig{PrincipalLimit: 1, GlobalLimit: 2, Window: 2 * time.Second}
	allowed, err := backend.Allow(ctx, principalA, cfg)
	if err != nil || !allowed {
		t.Fatalf("first principal A allow=%v err=%v", allowed, err)
	}
	allowed, err = backend.Allow(ctx, principalA, cfg)
	if err != nil || allowed {
		t.Fatalf("second principal A should hit principal limit: allow=%v err=%v", allowed, err)
	}
	allowed, err = backend.Allow(ctx, principalB, cfg)
	if err != nil || allowed {
		t.Fatalf("principal B should hit global limit after denied attempt increments counters: allow=%v err=%v", allowed, err)
	}
	if got, err := client.Get(ctx, operatorSentinelKey).Result(); err != nil || got != "41" {
		t.Fatalf("operator limiter namespace changed: value=%q err=%v", got, err)
	}
}
