package config

import (
	"strings"
	"testing"
)

const (
	publicationConfigTestToken = "publish-0123456789abcdef0123456789abcdef"
	publicationReviewTestToken = "review-0123456789abcdef0123456789abcdef"
	publicationWriteTestToken  = "submit-0123456789abcdef0123456789abcdef"
)

func clearPublicationEnv(t *testing.T) {
	t.Helper()
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", "")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", "")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")
}

func TestRuleSubmissionPublicationDefaultsDisabled(t *testing.T) {
	clearPublicationEnv(t)
	cfg := Load()
	if cfg.RuleSubmissionPublicationsEnabled {
		t.Fatal("rule submission publications must be disabled by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled publication config should validate: %v", err)
	}
}

func TestRuleSubmissionPublicationInvalidBooleanFailsClosed(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "definitely")
	cfg := Load()
	if cfg.RuleSubmissionPublicationsEnabled {
		t.Fatal("invalid publication enablement must fail closed to disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fail-closed publication config should validate: %v", err)
	}
}

func TestRuleSubmissionPublicationEnabledRequiresToken(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "publisher-ci")
	if err := Load().Validate(); err == nil {
		t.Fatal("enabled publication without token must fail validation")
	}
}

func TestRuleSubmissionPublicationRejectsShortToken(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", "too-short")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "publisher-ci")
	if err := Load().Validate(); err == nil {
		t.Fatal("enabled publication with short token must fail validation")
	}
}

func TestRuleSubmissionPublicationRequiresActorLabel(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", publicationConfigTestToken)
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "   ")
	if err := Load().Validate(); err == nil {
		t.Fatal("enabled publication without trusted actor label must fail validation")
	}
}

func TestRuleSubmissionPublicationBoundsActorLabelByUTF8Bytes(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", publicationConfigTestToken)

	t.Run("exactly 120 bytes accepted", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", strings.Repeat("界", 40))
		if err := Load().Validate(); err != nil {
			t.Fatalf("120-byte publication actor label should validate: %v", err)
		}
	})

	t.Run("more than 120 bytes rejected", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", strings.Repeat("界", 41))
		if err := Load().Validate(); err == nil {
			t.Fatal("publication actor label above 120 UTF-8 bytes must fail validation")
		}
	})
}

func TestRuleSubmissionPublicationRejectsSharedWriteToken(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", publicationConfigTestToken)
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "publisher-ci")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", publicationConfigTestToken)
	if err := Load().Validate(); err == nil {
		t.Fatal("publication token must not reuse submission write token")
	}
}

func TestRuleSubmissionPublicationRejectsSharedReviewToken(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", publicationConfigTestToken)
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "publisher-ci")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", publicationConfigTestToken)
	if err := Load().Validate(); err == nil {
		t.Fatal("publication token must not reuse review token")
	}
}

func TestRuleSubmissionPublicationAcceptsIndependentControlledConfig(t *testing.T) {
	clearPublicationEnv(t)
	t.Setenv("RULE_SUBMISSION_PUBLICATIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_PUBLICATION_TOKEN", publicationConfigTestToken)
	t.Setenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL", "  publisher-ci  ")
	t.Setenv("RULE_SUBMISSION_REVIEW_TOKEN", publicationReviewTestToken)
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", publicationWriteTestToken)

	cfg := Load()
	if !cfg.RuleSubmissionPublicationsEnabled {
		t.Fatal("expected publication enabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected independent publication config to validate: %v", err)
	}
}
