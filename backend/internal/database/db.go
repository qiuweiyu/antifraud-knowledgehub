package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Store struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func Connect(cfg config.Config) (*Store, error) {
	dialector, err := dialector(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Category{}, &RiskRule{}, &RuleSubmission{}, &RuleSubmissionReviewEvent{}, &RuleSubmissionPublicationEvent{}, &ScamCase{}, &AnalysisRecord{}); err != nil {
		return nil, err
	}
	if err := PrepareRuleSubmissionIdempotency(db); err != nil {
		return nil, err
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	_ = rdb.Ping(ctx).Err()
	return &Store{DB: db, Redis: rdb}, nil
}

func dialector(cfg config.Config) (gorm.Dialector, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver)) {
	case "", "postgres", "postgresql":
		return postgres.Open(cfg.DatabaseDSN), nil
	case "sqlite":
		return sqlite.Open(cfg.DatabaseDSN), nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.DatabaseDriver)
	}
}
