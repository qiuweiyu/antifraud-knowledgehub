package llmassist

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
)

type fakeProvider struct {
	calls atomic.Int32
	fn    func(context.Context, Input) (Assistance, error)
}

func (f *fakeProvider) Assist(ctx context.Context, input Input) (Assistance, error) {
	f.calls.Add(1)
	return f.fn(ctx, input)
}

func validAssistance() Assistance {
	return Assistance{
		Summary:      "Suspicious transfer request.",
		Observations: []string{"The message creates urgency."},
		Limitations:  []string{"Verify through an independent official channel."},
	}
}

func testInput() Input {
	return Input{
		Text: "客服称账户异常，需要转账到安全账户",
		RuleResult: riskengine.Result{
			RiskScore: 80,
			RiskLevel: "high",
			MatchedRules: []riskengine.MatchedRule{{
				RuleCode:       "safe_account_transfer",
				RuleName:       "安全账户转账",
				CategoryCode:   "fake_customer_service",
				Weight:         80,
				Severity:       "high",
				Evidence:       "安全账户",
				Explanation:    "Synthetic rule evidence.",
				Recommendation: "Verify independently.",
			}},
			Summary:         "rule summary",
			Recommendations: []string{"Do not transfer before verification."},
		},
	}
}

func TestNewServiceRejectsInvalidConstruction(t *testing.T) {
	if _, err := NewService(nil, time.Second); err == nil {
		t.Fatal("nil provider must be rejected")
	}
	provider := &fakeProvider{fn: func(context.Context, Input) (Assistance, error) {
		return validAssistance(), nil
	}}
	if _, err := NewService(provider, 0); err == nil {
		t.Fatal("non-positive timeout must be rejected")
	}
}

func TestServiceReturnsAvailableForValidAssistance(t *testing.T) {
	provider := &fakeProvider{fn: func(context.Context, Input) (Assistance, error) {
		return Assistance{
			Summary:      "  Suspicious transfer request.  ",
			Observations: []string{"  Creates urgency.  "},
			Limitations:  []string{"  Verify independently.  "},
		}, nil
	}}
	service, err := NewService(provider, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	outcome := service.Assist(context.Background(), testInput())
	if outcome.Status != StatusAvailable {
		t.Fatalf("expected available, got %q", outcome.Status)
	}
	if outcome.Assistance.Summary != "Suspicious transfer request." || outcome.Assistance.Observations[0] != "Creates urgency." || outcome.Assistance.Limitations[0] != "Verify independently." {
		t.Fatalf("unexpected normalized assistance: %+v", outcome.Assistance)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("expected one provider call, got %d", provider.calls.Load())
	}
}

func TestServiceProviderErrorFallsBackToUnavailable(t *testing.T) {
	provider := &fakeProvider{fn: func(context.Context, Input) (Assistance, error) {
		return Assistance{}, errors.New("provider failed")
	}}
	service, err := NewService(provider, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if outcome := service.Assist(context.Background(), testInput()); outcome.Status != StatusUnavailable {
		t.Fatalf("expected unavailable, got %+v", outcome)
	}
}

func TestServiceTimeoutFallsBackToUnavailable(t *testing.T) {
	provider := &fakeProvider{fn: func(ctx context.Context, _ Input) (Assistance, error) {
		<-ctx.Done()
		return Assistance{}, ctx.Err()
	}}
	service, err := NewService(provider, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if outcome := service.Assist(context.Background(), testInput()); outcome.Status != StatusUnavailable {
		t.Fatalf("expected unavailable after timeout, got %+v", outcome)
	}
}

func TestServiceCancelledCallerDoesNotInvokeProvider(t *testing.T) {
	provider := &fakeProvider{fn: func(context.Context, Input) (Assistance, error) {
		return validAssistance(), nil
	}}
	service, err := NewService(provider, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if outcome := service.Assist(ctx, testInput()); outcome.Status != StatusUnavailable {
		t.Fatalf("expected unavailable for cancelled caller, got %+v", outcome)
	}
	if provider.calls.Load() != 0 {
		t.Fatalf("cancelled caller must not invoke provider, calls=%d", provider.calls.Load())
	}
}

func TestServiceDeepCopiesAuthoritativeRuleResultBeforeProvider(t *testing.T) {
	input := testInput()
	provider := &fakeProvider{fn: func(_ context.Context, providerInput Input) (Assistance, error) {
		providerInput.RuleResult.MatchedRules[0].RuleCode = "tampered"
		providerInput.RuleResult.MatchedRules[0].Evidence = "tampered"
		providerInput.RuleResult.Recommendations[0] = "tampered"
		return validAssistance(), nil
	}}
	service, err := NewService(provider, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if outcome := service.Assist(context.Background(), input); outcome.Status != StatusAvailable {
		t.Fatalf("expected available, got %+v", outcome)
	}
	if input.RuleResult.MatchedRules[0].RuleCode != "safe_account_transfer" || input.RuleResult.MatchedRules[0].Evidence != "安全账户" {
		t.Fatalf("provider mutated authoritative matched rule: %+v", input.RuleResult.MatchedRules[0])
	}
	if input.RuleResult.Recommendations[0] != "Do not transfer before verification." {
		t.Fatalf("provider mutated authoritative recommendations: %+v", input.RuleResult.Recommendations)
	}
}

func TestServiceRejectsInvalidProviderOutput(t *testing.T) {
	tests := []struct {
		name       string
		assistance Assistance
	}{
		{name: "empty summary", assistance: Assistance{Summary: "   "}},
		{name: "oversize summary", assistance: Assistance{Summary: strings.Repeat("x", maxSummaryBytes+1)}},
		{name: "too many observations", assistance: Assistance{Summary: "ok", Observations: make([]string, maxListItems+1)}},
		{name: "blank list item", assistance: Assistance{Summary: "ok", Limitations: []string{" "}}},
		{name: "oversize list item", assistance: Assistance{Summary: "ok", Observations: []string{strings.Repeat("x", maxListItemBytes+1)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{fn: func(context.Context, Input) (Assistance, error) {
				return test.assistance, nil
			}}
			service, err := NewService(provider, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if outcome := service.Assist(context.Background(), testInput()); outcome.Status != StatusUnavailable {
				t.Fatalf("invalid provider output must be unavailable, got %+v", outcome)
			}
		})
	}
}

func TestAssistanceTypeHasNoRiskAuthorityFields(t *testing.T) {
	typeInfo := reflect.TypeOf(Assistance{})
	got := make([]string, 0, typeInfo.NumField())
	for i := 0; i < typeInfo.NumField(); i++ {
		got = append(got, typeInfo.Field(i).Name)
	}
	want := []string{"Summary", "Observations", "Limitations"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assistance authority surface changed: got %v want %v", got, want)
	}
}
