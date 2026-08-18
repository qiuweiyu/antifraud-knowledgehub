package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	minSubmissionWriteTokenLength             = 32
	minSubmissionReviewTokenLength            = 32
	minSubmissionPublicationTokenLength       = 32
	maxSubmissionReviewActorLabelBytes        = 120
	maxSubmissionPublicationActorLabelBytes   = 120
	defaultSubmissionCredentialLimit    int64 = 5
	defaultSubmissionGlobalLimit        int64 = 50
	defaultSubmissionRateWindow               = 10 * time.Minute
	defaultLLMAssistanceTimeout                = 5 * time.Second
)

type Config struct {
	AppEnv                              string
	AppPort                             string
	DatabaseDriver                      string
	DatabaseDSN                         string
	RedisAddr                           string
	CORSAllowOrigins                    []string
	LLMAssistanceEnabled                bool
	LLMAssistanceProvider               string
	LLMAssistanceTimeout                time.Duration
	RuleSubmissionsEnabled              bool
	RuleSubmissionWriteToken            string
	RuleSubmissionCredentialLimit       int64
	RuleSubmissionGlobalLimit           int64
	RuleSubmissionRateWindow            time.Duration
	RuleSubmissionReviewsEnabled        bool
	RuleSubmissionReviewToken           string
	RuleSubmissionReviewActorLabel      string
	RuleSubmissionPublicationsEnabled   bool
	RuleSubmissionPublicationToken      string
	RuleSubmissionPublicationActorLabel string
}

func Load() Config {
	return Config{
		AppEnv:                              getEnv("APP_ENV", "development"),
		AppPort:                             getEnv("APP_PORT", "8080"),
		DatabaseDriver:                      getEnv("DATABASE_DRIVER", "postgres"),
		DatabaseDSN:                         getEnv("DATABASE_DSN", "host=localhost user=postgres password=postgres dbname=antifraud port=5432 sslmode=disable TimeZone=Asia/Shanghai"),
		RedisAddr:                           getEnv("REDIS_ADDR", "localhost:6379"),
		CORSAllowOrigins:                    splitEnv("CORS_ALLOW_ORIGINS", []string{"http://localhost:5173", "http://localhost:3000"}),
		LLMAssistanceEnabled:                boolEnv("LLM_ASSISTANCE_ENABLED"),
		LLMAssistanceProvider:               strings.TrimSpace(os.Getenv("LLM_ASSISTANCE_PROVIDER")),
		LLMAssistanceTimeout:                durationEnv("LLM_ASSISTANCE_TIMEOUT", defaultLLMAssistanceTimeout),
		RuleSubmissionsEnabled:              boolEnv("RULE_SUBMISSIONS_ENABLED"),
		RuleSubmissionWriteToken:            os.Getenv("RULE_SUBMISSION_WRITE_TOKEN"),
		RuleSubmissionCredentialLimit:       int64Env("RULE_SUBMISSION_CREDENTIAL_LIMIT", defaultSubmissionCredentialLimit),
		RuleSubmissionGlobalLimit:           int64Env("RULE_SUBMISSION_GLOBAL_LIMIT", defaultSubmissionGlobalLimit),
		RuleSubmissionRateWindow:            durationEnv("RULE_SUBMISSION_RATE_WINDOW", defaultSubmissionRateWindow),
		RuleSubmissionReviewsEnabled:        boolEnv("RULE_SUBMISSION_REVIEWS_ENABLED"),
		RuleSubmissionReviewToken:           os.Getenv("RULE_SUBMISSION_REVIEW_TOKEN"),
		RuleSubmissionReviewActorLabel:      os.Getenv("RULE_SUBMISSION_REVIEW_ACTOR_LABEL"),
		RuleSubmissionPublicationsEnabled:   boolEnv("RULE_SUBMISSION_PUBLICATIONS_ENABLED"),
		RuleSubmissionPublicationToken:      os.Getenv("RULE_SUBMISSION_PUBLICATION_TOKEN"),
		RuleSubmissionPublicationActorLabel: os.Getenv("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL"),
	}
}

func (c Config) Validate() error {
	if c.LLMAssistanceEnabled {
		if c.LLMAssistanceTimeout < time.Millisecond {
			return fmt.Errorf("LLM_ASSISTANCE_TIMEOUT must be at least 1ms when LLM assistance is enabled")
		}
		return fmt.Errorf("LLM assistance is enabled but no runtime provider is available in this build")
	}

	if c.RuleSubmissionsEnabled {
		if strings.TrimSpace(c.RuleSubmissionWriteToken) == "" {
			return fmt.Errorf("rule submissions are enabled but RULE_SUBMISSION_WRITE_TOKEN is not configured")
		}
		if len(c.RuleSubmissionWriteToken) < minSubmissionWriteTokenLength {
			return fmt.Errorf("RULE_SUBMISSION_WRITE_TOKEN must be at least %d characters when rule submissions are enabled", minSubmissionWriteTokenLength)
		}
		if c.RuleSubmissionCredentialLimit <= 0 {
			return fmt.Errorf("RULE_SUBMISSION_CREDENTIAL_LIMIT must be positive when rule submissions are enabled")
		}
		if c.RuleSubmissionGlobalLimit <= 0 {
			return fmt.Errorf("RULE_SUBMISSION_GLOBAL_LIMIT must be positive when rule submissions are enabled")
		}
		if c.RuleSubmissionGlobalLimit < c.RuleSubmissionCredentialLimit {
			return fmt.Errorf("RULE_SUBMISSION_GLOBAL_LIMIT must be greater than or equal to RULE_SUBMISSION_CREDENTIAL_LIMIT")
		}
		if c.RuleSubmissionRateWindow < time.Millisecond {
			return fmt.Errorf("RULE_SUBMISSION_RATE_WINDOW must be at least 1ms when rule submissions are enabled")
		}
	}

	if c.RuleSubmissionReviewsEnabled {
		if strings.TrimSpace(c.RuleSubmissionReviewToken) == "" {
			return fmt.Errorf("rule submission reviews are enabled but RULE_SUBMISSION_REVIEW_TOKEN is not configured")
		}
		if len(c.RuleSubmissionReviewToken) < minSubmissionReviewTokenLength {
			return fmt.Errorf("RULE_SUBMISSION_REVIEW_TOKEN must be at least %d characters when rule submission reviews are enabled", minSubmissionReviewTokenLength)
		}
		actorLabel := strings.TrimSpace(c.RuleSubmissionReviewActorLabel)
		if actorLabel == "" {
			return fmt.Errorf("rule submission reviews are enabled but RULE_SUBMISSION_REVIEW_ACTOR_LABEL is not configured")
		}
		if len([]byte(actorLabel)) > maxSubmissionReviewActorLabelBytes {
			return fmt.Errorf("RULE_SUBMISSION_REVIEW_ACTOR_LABEL must be at most %d UTF-8 bytes when rule submission reviews are enabled", maxSubmissionReviewActorLabelBytes)
		}
		if c.RuleSubmissionWriteToken != "" && c.RuleSubmissionReviewToken == c.RuleSubmissionWriteToken {
			return fmt.Errorf("RULE_SUBMISSION_REVIEW_TOKEN must be different from RULE_SUBMISSION_WRITE_TOKEN")
		}
	}

	if c.RuleSubmissionPublicationsEnabled {
		if strings.TrimSpace(c.RuleSubmissionPublicationToken) == "" {
			return fmt.Errorf("rule submission publications are enabled but RULE_SUBMISSION_PUBLICATION_TOKEN is not configured")
		}
		if len(c.RuleSubmissionPublicationToken) < minSubmissionPublicationTokenLength {
			return fmt.Errorf("RULE_SUBMISSION_PUBLICATION_TOKEN must be at least %d characters when rule submission publications are enabled", minSubmissionPublicationTokenLength)
		}
		actorLabel := strings.TrimSpace(c.RuleSubmissionPublicationActorLabel)
		if actorLabel == "" {
			return fmt.Errorf("rule submission publications are enabled but RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL is not configured")
		}
		if len([]byte(actorLabel)) > maxSubmissionPublicationActorLabelBytes {
			return fmt.Errorf("RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL must be at most %d UTF-8 bytes when rule submission publications are enabled", maxSubmissionPublicationActorLabelBytes)
		}
		if c.RuleSubmissionWriteToken != "" && c.RuleSubmissionPublicationToken == c.RuleSubmissionWriteToken {
			return fmt.Errorf("RULE_SUBMISSION_PUBLICATION_TOKEN must be different from RULE_SUBMISSION_WRITE_TOKEN")
		}
		if c.RuleSubmissionReviewToken != "" && c.RuleSubmissionPublicationToken == c.RuleSubmissionReviewToken {
			return fmt.Errorf("RULE_SUBMISSION_PUBLICATION_TOKEN must be different from RULE_SUBMISSION_REVIEW_TOKEN")
		}
	}

	return nil
}

func (c Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func (c Config) PortInt() int {
	port, err := strconv.Atoi(c.AppPort)
	if err != nil {
		return 8080
	}
	return port
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return enabled
}

func int64Env(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return parsed
}

func splitEnv(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
