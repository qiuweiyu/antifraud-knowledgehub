package llmassist

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type providerConformanceHarness struct {
	name     string
	model    string
	new      func(string, string, httpDoer) (Provider, error)
	success  func(*testing.T) *http.Response
}

func providerConformanceHarnesses() []providerConformanceHarness {
	return []providerConformanceHarness{
		{
			name:  ProviderOpenAI,
			model: "openai-conformance-model",
			new:   newOpenAIProviderWithDoer,
			success: func(t *testing.T) *http.Response {
				return completedOpenAIResponse(t, validAssistanceJSON(t))
			},
		},
		{
			name:  ProviderGemini,
			model: "gemini-conformance-model",
			new:   newGeminiProviderWithDoer,
			success: func(t *testing.T) *http.Response {
				return completedGeminiResponse(t, validAssistanceJSON(t))
			},
		},
		{
			name:  ProviderDeepSeek,
			model: "deepseek-conformance-model",
			new:   newDeepSeekProviderWithDoer,
			success: func(t *testing.T) *http.Response {
				return completedDeepSeekResponse(t, validAssistanceJSON(t))
			},
		},
	}
}

func TestRealProvidersConformToConstructionBoundary(t *testing.T) {
	for _, harness := range providerConformanceHarnesses() {
		harness := harness
		t.Run(harness.name, func(t *testing.T) {
			doer := &recordingDoer{}
			if _, err := harness.new("   ", harness.model, doer); err == nil {
				t.Fatal("blank credential must be rejected")
			}
			if _, err := harness.new("provider-secret", "bad\nmodel", doer); err == nil {
				t.Fatal("unsafe generic model must be rejected")
			}
		})
	}
}

func TestRealProvidersConformToCancelledContextBoundary(t *testing.T) {
	for _, harness := range providerConformanceHarnesses() {
		harness := harness
		t.Run(harness.name, func(t *testing.T) {
			doer := &recordingDoer{do: func(_ *http.Request) (*http.Response, error) {
				return harness.success(t), nil
			}}
			provider, err := harness.new("provider-secret", harness.model, doer)
			if err != nil {
				t.Fatalf("construct provider: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := provider.Assist(ctx, Input{Text: "suspicious", RuleResult: testRuleResult()}); err == nil {
				t.Fatal("pre-cancelled context must fail")
			}
			if doer.calls != 0 {
				t.Fatalf("pre-cancelled context must perform zero HTTP calls, got %d", doer.calls)
			}
		})
	}
}

func TestRealProvidersConformToSingleRequestAndAssistanceShape(t *testing.T) {
	for _, harness := range providerConformanceHarnesses() {
		harness := harness
		t.Run(harness.name, func(t *testing.T) {
			doer := &recordingDoer{do: func(_ *http.Request) (*http.Response, error) {
				return harness.success(t), nil
			}}
			provider, err := harness.new("provider-secret", harness.model, doer)
			if err != nil {
				t.Fatalf("construct provider: %v", err)
			}
			service, err := NewService(provider, time.Second)
			if err != nil {
				t.Fatalf("construct service: %v", err)
			}
			input := Input{Text: "suspicious", RuleResult: testRuleResult()}
			before := cloneInput(input)
			outcome := service.Assist(context.Background(), input)
			if outcome.Status != StatusAvailable {
				t.Fatalf("expected available assistance, got %+v", outcome)
			}
			if outcome.Assistance.Summary != "Supplemental summary" {
				t.Fatalf("unexpected assistance: %+v", outcome.Assistance)
			}
			if doer.calls != 1 {
				t.Fatalf("one Assist call must perform exactly one provider HTTP request, got %d", doer.calls)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatalf("provider/service path mutated caller input: before=%+v after=%+v", before, input)
			}
		})
	}
}

func TestRealProvidersConformToFailureFallbackAndSecretSafety(t *testing.T) {
	const secret = "conformance-provider-super-secret"

	for _, harness := range providerConformanceHarnesses() {
		harness := harness
		t.Run(harness.name, func(t *testing.T) {
			doer := &recordingDoer{do: func(_ *http.Request) (*http.Response, error) {
				return nil, errors.New("transport failure contains " + secret)
			}}
			provider, err := harness.new(secret, harness.model, doer)
			if err != nil {
				t.Fatalf("construct provider: %v", err)
			}

			_, providerErr := provider.Assist(context.Background(), Input{Text: "suspicious", RuleResult: testRuleResult()})
			if providerErr == nil {
				t.Fatal("transport failure must return provider error")
			}
			if strings.Contains(providerErr.Error(), secret) {
				t.Fatalf("provider error leaked credential: %v", providerErr)
			}
			if doer.calls != 1 {
				t.Fatalf("provider failure must not retry; expected one HTTP call, got %d", doer.calls)
			}

			fallbackDoer := &recordingDoer{do: func(_ *http.Request) (*http.Response, error) {
				return nil, errors.New("provider unavailable")
			}}
			fallbackProvider, err := harness.new(secret, harness.model, fallbackDoer)
			if err != nil {
				t.Fatalf("construct fallback provider: %v", err)
			}
			service, err := NewService(fallbackProvider, time.Second)
			if err != nil {
				t.Fatalf("construct service: %v", err)
			}
			input := Input{Text: "suspicious", RuleResult: testRuleResult()}
			before := cloneInput(input)
			outcome := service.Assist(context.Background(), input)
			if outcome.Status != StatusUnavailable {
				t.Fatalf("provider failure must normalize to unavailable, got %+v", outcome)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatalf("failure path mutated deterministic input: before=%+v after=%+v", before, input)
			}
			if fallbackDoer.calls != 1 {
				t.Fatalf("service/provider failure must not retry, got %d HTTP calls", fallbackDoer.calls)
			}
		})
	}
}

func TestAssistancePublicShapeHasNoAuthorityFields(t *testing.T) {
	typeInfo := reflect.TypeOf(Assistance{})
	if typeInfo.NumField() != 3 {
		t.Fatalf("Assistance authority surface changed: fields=%d", typeInfo.NumField())
	}
	allowed := map[string]bool{
		"Summary":      true,
		"Observations": true,
		"Limitations":  true,
	}
	for i := 0; i < typeInfo.NumField(); i++ {
		field := typeInfo.Field(i)
		if !allowed[field.Name] {
			t.Fatalf("unexpected Assistance field %s", field.Name)
		}
		lower := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, forbidden := range []string{"risk", "score", "level", "rule", "verdict", "fraud", "decision"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("Assistance field %s contains authority-bearing concept %q", field.Name, forbidden)
			}
		}
	}
}
