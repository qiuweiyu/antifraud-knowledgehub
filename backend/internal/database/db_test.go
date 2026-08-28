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
	for _, column := range []string{"DraftDigest", "Kind", "TargetRiskRuleID", "BaseVersion", "RequestDigest"} {
		if !store.DB.Migrator().HasColumn(&RuleSubmission{}, column) {
			t.Fatalf("expected RuleSubmission %s column to be prepared at startup", column)
		}
	}
	if store.DB.Migrator().HasIndex(&RuleSubmission{}, RuleSubmissionPendingDigestIndex) {
		t.Fatalf("legacy partial unique index %s must be removed at startup", RuleSubmissionPendingDigestIndex)
	}
	if !store.DB.Migrator().HasIndex(&RuleSubmission{}, RuleSubmissionPendingRequestDigestIndex) {
		t.Fatalf("expected partial unique index %s to be prepared at startup", RuleSubmissionPendingRequestDigestIndex)
	}
	if !store.DB.Migrator().HasTable(&RuleSubmissionReviewEvent{}) {
		t.Fatal("expected RuleSubmissionReviewEvent table to be created by database startup migration")
	}
}
