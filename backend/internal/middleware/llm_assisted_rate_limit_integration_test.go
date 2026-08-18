package middleware

import (
	"context"
	"testing"
	"time"
)

const llmAssistedRateGlobalKey = "afkh:llm-assisted-analysis:rate:global"

func llmAssistedCredentialRateKeyForTest(token string) string {
	return "afkh:llm-assisted-analysis:rate:credential:" + llmAssistedCredentialID(token)
}

func resetLLMAssistedIntegrationKeys(t *testing.T, credentialKeys ...string) {
	t.Helper()
	client := integrationRedisClient(t)
	keys := append([]string{llmAssistedRateGlobalKey}, credentialKeys...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Del(ctx, keys...).Err(); err != nil {
		t.Fatalf("reset assisted-analysis integration Redis keys: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.Del(ctx, keys...).Err()
	})
	_ = client.Close()
}

func TestRedisLLMAssistedRateBackendIntegrationCredentialLimit(t *testing.T) {
	client := integrationRedisClient(t)
	backend := RedisLLMAssistedRateBackend{Client: client}
	credentialID := llmAssistedCredentialID("llm-assisted-integration-credential")
	credentialKey := "afkh:llm-assisted-analysis:rate:credential:" + credentialID
	ctx := context.Background()
	if err := client.Del(ctx, credentialKey, llmAssistedRateGlobalKey).Err(); err != nil {
		t.Fatalf("reset assisted-analysis keys: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(), credentialKey, llmAssistedRateGlobalKey).Err()
	})

	cfg := LLMAssistedRateConfig{CredentialLimit: 2, GlobalLimit: 20, Window: 2 * time.Second}
	for attempt := 1; attempt <= 2; attempt++ {
		allowed, err := backend.Allow(ctx, credentialID, cfg)
		if err != nil {
			t.Fatalf("allow attempt %d: %v", attempt, err)
		}
		if !allowed {
			t.Fatalf("attempt %d unexpectedly rejected", attempt)
		}
	}
	allowed, err := backend.Allow(ctx, credentialID, cfg)
	if err != nil {
		t.Fatalf("limited attempt: %v", err)
	}
	if allowed {
		t.Fatal("expected third assisted-analysis request to hit credential limit")
	}
	if exists, err := client.Exists(ctx, credentialKey).Result(); err != nil || exists != 1 {
		t.Fatalf("credential key exists=%d err=%v", exists, err)
	}
}

func TestRedisLLMAssistedRateBackendIntegrationGlobalLimitAndNamespaceIsolation(t *testing.T) {
	client := integrationRedisClient(t)
	backend := RedisLLMAssistedRateBackend{Client: client}
	ctx := context.Background()
	ids := []string{
		llmAssistedCredentialID("llm-assisted-global-a"),
		llmAssistedCredentialID("llm-assisted-global-b"),
		llmAssistedCredentialID("llm-assisted-global-c"),
	}
	llmKeys := []string{
		"afkh:llm-assisted-analysis:rate:credential:" + ids[0],
		"afkh:llm-assisted-analysis:rate:credential:" + ids[1],
		"afkh:llm-assisted-analysis:rate:credential:" + ids[2],
		llmAssistedRateGlobalKey,
	}
	submissionCredentialKey := submissionCredentialRateKey("llm-assisted-namespace-sentinel")
	allKeys := append(append([]string{}, llmKeys...), submissionRateGlobalKey, submissionCredentialKey)
	if err := client.Del(ctx, allKeys...).Err(); err != nil {
		t.Fatalf("reset integration keys: %v", err)
	}
	if err := client.Set(ctx, submissionRateGlobalKey, "41", time.Minute).Err(); err != nil {
		t.Fatalf("seed submission global sentinel: %v", err)
	}
	if err := client.Set(ctx, submissionCredentialKey, "17", time.Minute).Err(); err != nil {
		t.Fatalf("seed submission credential sentinel: %v", err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), allKeys...).Err() })

	cfg := LLMAssistedRateConfig{CredentialLimit: 10, GlobalLimit: 2, Window: 2 * time.Second}
	for index := 0; index < 2; index++ {
		allowed, err := backend.Allow(ctx, ids[index], cfg)
		if err != nil {
			t.Fatalf("global allow attempt %d: %v", index+1, err)
		}
		if !allowed {
			t.Fatalf("global attempt %d unexpectedly rejected", index+1)
		}
	}
	allowed, err := backend.Allow(ctx, ids[2], cfg)
	if err != nil {
		t.Fatalf("global limited attempt: %v", err)
	}
	if allowed {
		t.Fatal("expected third assisted-analysis request to hit global limit")
	}

	if got, err := client.Get(ctx, submissionRateGlobalKey).Result(); err != nil || got != "41" {
		t.Fatalf("submission global key changed: value=%q err=%v", got, err)
	}
	if got, err := client.Get(ctx, submissionCredentialKey).Result(); err != nil || got != "17" {
		t.Fatalf("submission credential key changed: value=%q err=%v", got, err)
	}
}
