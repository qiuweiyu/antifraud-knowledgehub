package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const minSubmissionWriteTokenLength = 32

type Config struct {
	AppEnv                   string
	AppPort                  string
	DatabaseDriver           string
	DatabaseDSN              string
	RedisAddr                string
	CORSAllowOrigins         []string
	RuleSubmissionsEnabled   bool
	RuleSubmissionWriteToken string
}

func Load() Config {
	return Config{
		AppEnv:                   getEnv("APP_ENV", "development"),
		AppPort:                  getEnv("APP_PORT", "8080"),
		DatabaseDriver:           getEnv("DATABASE_DRIVER", "postgres"),
		DatabaseDSN:              getEnv("DATABASE_DSN", "host=localhost user=postgres password=postgres dbname=antifraud port=5432 sslmode=disable TimeZone=Asia/Shanghai"),
		RedisAddr:                getEnv("REDIS_ADDR", "localhost:6379"),
		CORSAllowOrigins:         splitEnv("CORS_ALLOW_ORIGINS", []string{"http://localhost:5173", "http://localhost:3000"}),
		RuleSubmissionsEnabled:   boolEnv("RULE_SUBMISSIONS_ENABLED"),
		RuleSubmissionWriteToken: os.Getenv("RULE_SUBMISSION_WRITE_TOKEN"),
	}
}

func (c Config) Validate() error {
	if !c.RuleSubmissionsEnabled {
		return nil
	}
	if strings.TrimSpace(c.RuleSubmissionWriteToken) == "" {
		return fmt.Errorf("rule submissions are enabled but RULE_SUBMISSION_WRITE_TOKEN is not configured")
	}
	if len(c.RuleSubmissionWriteToken) < minSubmissionWriteTokenLength {
		return fmt.Errorf("RULE_SUBMISSION_WRITE_TOKEN must be at least %d characters when rule submissions are enabled", minSubmissionWriteTokenLength)
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
