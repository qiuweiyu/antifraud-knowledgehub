package config

import (
	"testing"
	"time"
)

func clearSubmissionRateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("RULE_SUBMISSION_CREDENTIAL_LIMIT", "")
	t.Setenv("RULE_SUBMISSION_GLOBAL_LIMIT", "")
	t.Setenv("RULE_SUBMISSION_RATE_WINDOW", "")
}

func TestRuleSubmissionTransportDefaultsDisabled(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")
	clearSubmissionRateEnv(t)

	cfg := Load()
	if cfg.RuleSubmissionsEnabled {
		t.Fatal("rule submissions must be disabled by default")
	}
	if cfg.RuleSubmissionCredentialLimit != 5 || cfg.RuleSubmissionGlobalLimit != 50 || cfg.RuleSubmissionRateWindow != 10*time.Minute {
		t.Fatalf("unexpected submission rate defaults: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled transport should validate: %v", err)
	}
}

func TestRuleSubmissionTransportInvalidBooleanFailsClosed(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "definitely")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "")
	clearSubmissionRateEnv(t)

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
	clearSubmissionRateEnv(t)

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
	clearSubmissionRateEnv(t)

	cfg := Load()
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled transport with short token must fail validation")
	}
}

func TestRuleSubmissionTransportAcceptsStrongTokenAndRateDefaults(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "0123456789abcdef0123456789abcdef")
	clearSubmissionRateEnv(t)

	cfg := Load()
	if !cfg.RuleSubmissionsEnabled {
		t.Fatal("expected transport enabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid controlled transport config: %v", err)
	}
}

func TestRuleSubmissionTransportAcceptsCustomRateSettings(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("RULE_SUBMISSION_CREDENTIAL_LIMIT", "7")
	t.Setenv("RULE_SUBMISSION_GLOBAL_LIMIT", "70")
	t.Setenv("RULE_SUBMISSION_RATE_WINDOW", "3m")

	cfg := Load()
	if cfg.RuleSubmissionCredentialLimit != 7 || cfg.RuleSubmissionGlobalLimit != 70 || cfg.RuleSubmissionRateWindow != 3*time.Minute {
		t.Fatalf("unexpected custom submission rate settings: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected custom rate settings to validate: %v", err)
	}
}

func TestRuleSubmissionTransportRejectsInvalidRateSettings(t *testing.T) {
	t.Setenv("RULE_SUBMISSIONS_ENABLED", "true")
	t.Setenv("RULE_SUBMISSION_WRITE_TOKEN", "0123456789abcdef0123456789abcdef")

	t.Run("invalid credential limit", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_CREDENTIAL_LIMIT", "not-a-number")
		t.Setenv("RULE_SUBMISSION_GLOBAL_LIMIT", "50")
		t.Setenv("RULE_SUBMISSION_RATE_WINDOW", "10m")
		if err := Load().Validate(); err == nil {
			t.Fatal("invalid credential limit must fail validation")
		}
	})

	t.Run("global below credential", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_CREDENTIAL_LIMIT", "10")
		t.Setenv("RULE_SUBMISSION_GLOBAL_LIMIT", "5")
		t.Setenv("RULE_SUBMISSION_RATE_WINDOW", "10m")
		if err := Load().Validate(); err == nil {
			t.Fatal("global limit below credential limit must fail validation")
		}
	})

	t.Run("invalid window", func(t *testing.T) {
		t.Setenv("RULE_SUBMISSION_CREDENTIAL_LIMIT", "5")
		t.Setenv("RULE_SUBMISSION_GLOBAL_LIMIT", "50")
		t.Setenv("RULE_SUBMISSION_RATE_WINDOW", "not-a-duration")
		if err := Load().Validate(); err == nil {
			t.Fatal("invalid rate window must fail validation")
		}
	})
}
