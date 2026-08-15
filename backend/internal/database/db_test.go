package database

import (
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
)

func TestConnectAutoMigratesRuleSubmission(t *testing.T) {
	store, err := Connect(config.Config{
		DatabaseDriver: "sqlite",
		DatabaseDSN:    ":memory:",
		RedisAddr:      "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Redis.Close()

	if !store.DB.Migrator().HasTable(&RuleSubmission{}) {
		t.Fatal("expected RuleSubmission table to be created by database startup migration")
	}
}
