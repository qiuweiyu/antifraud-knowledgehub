package llmassist

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	maxSummaryBytes      = 2000
	maxListItems         = 8
	maxListItemBytes     = 1000
	minimumAssistTimeout = time.Millisecond
)

type Service struct {
	provider Provider
	timeout  time.Duration
}

func NewService(provider Provider, timeout time.Duration) (Service, error) {
	if provider == nil {
		return Service{}, errors.New("llm assistance provider is required")
	}
	if timeout < minimumAssistTimeout {
		return Service{}, errors.New("llm assistance timeout must be at least 1ms")
	}
	return Service{provider: provider, timeout: timeout}, nil
}

func (s Service) Assist(ctx context.Context, input Input) Outcome {
	if ctx == nil || ctx.Err() != nil || s.provider == nil || s.timeout < minimumAssistTimeout {
		return unavailableOutcome()
	}

	providerInput := cloneInput(input)
	providerCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	assistance, err := s.provider.Assist(providerCtx, providerInput)
	if err != nil || providerCtx.Err() != nil {
		return unavailableOutcome()
	}

	normalized, err := validateAssistance(assistance)
	if err != nil {
		return unavailableOutcome()
	}
	return Outcome{Status: StatusAvailable, Assistance: normalized}
}

func cloneInput(input Input) Input {
	cloned := input
	if input.RuleResult.MatchedRules != nil {
		cloned.RuleResult.MatchedRules = append([]struct {
			RuleCode       string `json:"rule_code"`
			RuleName       string `json:"rule_name"`
			CategoryCode   string `json:"category_code"`
			Weight         int    `json:"weight"`
			Severity       string `json:"severity"`
			Evidence       string `json:"evidence"`
			Explanation    string `json:"explanation"`
			Recommendation string `json:"recommendation"`
		}{}, input.RuleResult.MatchedRules...)
	}
	if input.RuleResult.Recommendations != nil {
		cloned.RuleResult.Recommendations = append([]string(nil), input.RuleResult.Recommendations...)
	}
	return cloned
}

func validateAssistance(value Assistance) (Assistance, error) {
	summary := strings.TrimSpace(value.Summary)
	if summary == "" || len([]byte(summary)) > maxSummaryBytes {
		return Assistance{}, errors.New("invalid assistance summary")
	}

	observations, err := normalizeStringList(value.Observations)
	if err != nil {
		return Assistance{}, err
	}
	limitations, err := normalizeStringList(value.Limitations)
	if err != nil {
		return Assistance{}, err
	}

	return Assistance{
		Summary:      summary,
		Observations: observations,
		Limitations:  limitations,
	}, nil
}

func normalizeStringList(values []string) ([]string, error) {
	if len(values) > maxListItems {
		return nil, errors.New("assistance list has too many items")
	}
	if values == nil {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" || len([]byte(item)) > maxListItemBytes {
			return nil, errors.New("invalid assistance list item")
		}
		out = append(out, item)
	}
	return out, nil
}

func unavailableOutcome() Outcome {
	return Outcome{Status: StatusUnavailable}
}
