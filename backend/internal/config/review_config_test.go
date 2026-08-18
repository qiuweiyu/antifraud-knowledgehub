package config

import (
	"strings"
	"testing"
)

const (
	reviewConfigTestToken      = "review-0123456789abcdef0123456789abcdef"
	submissionConfigTestToken  = "submit-0123456789abcdef0123456789abcdef"
)

func clearReviewTransportEnv(t *testing.T) {
	t.Helper()
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", "")
	t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", "")
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")
}

func TestRuleSubmissionReviewTransportDefaultsDisabled(t *testing.T) {
	clearReviewTransportEnv(t)

	cfg := Load()
	if cfg.RuleSubmissionReviewsEnabled {
		t.Fatal("rule submission reviews must be disabled by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled review transport should validate: %v", err)
	}
}

func TestRuleSubmissionReviewTransportInvalidBooleanFailsClosed(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "definitely")

	cfg := Load()
	if cfg.RuleSubmissionReviewsEnabled {
		t.Fatal("invalid review enablement value must fail closed to disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fail-closed disabled review config should validate: %v", err)
	}
}

func TestRuleSubmissionReviewTransportEnabledRequiresToken(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", "maintainer-ci")

	if err := Load().Validate(); err == nil {
		t.Fatal("enabled review transport without token must fail validation")
	}
}

func TestRuleSubmissionReviewTransportRejectsShortToken(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", "too-short")
	t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", "maintainer-ci")

	if err := Load().Validate(); err == nil {
		t.Fatal("enabled review transport with short token must fail validation")
	}
}

func TestRuleSubmissionReviewTransportRequiresActorLabel(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", reviewConfigTestToken)
	t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", "   ")

	if err := Load().Validate(); err == nil {
		t.Fatal("enabled review transport without a trusted actor label must fail validation")
	}
}

func TestRuleSubmissionReviewTransportBoundsActorLabelByUTF8Bytes(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", reviewConfigTestToken)

	t.Run("exactly 120 bytes accepted", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", strings.Repeat("界", 40))
		if err := Load().Validate(); err != nil {
			t.Fatalf("120-byte UTF-8 actor label should validate: %v", err)
		}
	})

	t.Run("more than 120 bytes rejected", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", strings.Repeat("界", 41))
		if err := Load().Validate(); err == nil {
			t.Fatal("actor label above 120 UTF-8 bytes must fail validation")
		}
	})
}

func TestRuleSubmissionReviewTransportRejectsSharedWriteToken(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", reviewConfigTestToken)
	t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", "maintainer-ci")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", reviewConfigTestToken)

	if err := Load().Validate(); err == nil {
		t.Fatal("review token must not reuse the submission write token")
	}
}

func TestRuleSubmissionReviewTransportAcceptsIndependentControlledConfig(t *testing.T) {
	clearReviewTransportEnv(t)
	t.Setenv("RULE_SUBMISSION_REVIEWS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", reviewConfigTestToken)
	t.Setenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL", "  maintainer-ci  ")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", submissionConfigTestToken)

	cfg := Load()
	if !cfg.RuleSubmissionReviewsEnabled {
		t.Fatal("expected review transport enabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected independent controlled review config to validate: %v", err)
	}
}
