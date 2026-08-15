package config

import "testing"

func TestRuleSubmissionTransportDefaultsDisabled(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")

	cfg := Load()
	if cfg.RuleSubmissionsEnabled {
		t.Fatal("rule submissions must be disabled by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled transport should validate: %v", err)
	}
}

func TestRuleSubmissionTransportInvalidBooleanFailsClosed(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "definitely")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")

	cfg := Load()
	if cfg.RuleSubmissionsEnabled {
		t.Fatal("invalid enablement value must fail closed to disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fail-closed disabled config should validate: %v", err)
	}
}

func TestRuleSubmissionTransportEnabledRequiresToken(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")

	cfg := Load()
	if !cfg.RuleSubmissionsEnabled {
		t.Fatal("expected transport enabled")
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled transport without token must fail validation")
	}
}

func TestRuleSubmissionTransportRejectsShortToken(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "too-short")

	cfg := Load()
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled transport with short token must fail validation")
	}
}

func TestRuleSubmissionTransportAcceptsStrongToken(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "0123456789abcdef0123456789abcdef")

	cfg := Load()
	if !cfg.RuleSubmissionsEnabled {
		t.Fatal("expected transport enabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid controlled transport config: %v", err)
	}
}
