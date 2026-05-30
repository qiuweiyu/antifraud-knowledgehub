package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv           string
	AppPort          string
	DatabaseDSN      string
	RedisAddr        string
	CORSAllowOrigins []string
}

func Load() Config {
	return Config{
		AppEnv:           getEnv("APP_ENV", "development"),
		AppPort:          getEnv("APP_PORT", "8080"),
		DatabaseDSN:      getEnv("DATABASE_DSN", "host=localhost user=postgres password=postgres dbname=antifraud port=5432 sslmode=disable TimeZone=Asia/Shanghai"),
		RedisAddr:        getEnv("REDIS_ADDR", "localhost:6379"),
		CORSAllowOrigins: splitEnv("CORS_ALLOW_ORIGINS", []string{"http://localhost:5173", "http://localhost:3000"}),
	}
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
