package rule

import (
	"os"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const postgresSubmissionIntegrationDSNEnv = "AFKH_POSTGRES_INTEGRATION_DSN"

func TestPostgresConcurrentExactReplayConvergesOnOnePendingSubmission(t *testing.T) {
	dsn := os.Getenv(postgresSubmissionIntegrationDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not configured", postgresSubmissionIntegrationDSNEnv)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(32)
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&database.RuleSubmission{}, &database.RiskRule{}, &database.Category{})
		_ = sqlDB.Close()
	})

	_ = db.Migrator().DropTable(&database.RuleSubmission{}, &database.RiskRule{}, &database.Category{})
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}); err != nil {
		t.Fatalf("migrate PostgreSQL integration schema: %v", err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatalf("prepare PostgreSQL idempotency invariant: %v", err)
	}
	if !db.Migrator().HasIndex(&database.RuleSubmission{}, database.RuleSubmissionPendingDigestIndex) {
		t.Fatalf("expected PostgreSQL partial unique index %s", database.RuleSubmissionPendingDigestIndex)
	}
	if err := db.Create(&database.Category{
		Code: "fake_customer_service", Name: "PostgreSQL integration category", SeverityDefault: "high",
	}).Error; err != nil {
		t.Fatal(err)
	}

	draft := DraftRequest{
		Code: "postgres_concurrent_replay", Name: "PostgreSQL concurrent replay",
		CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "synthetic concurrent signal",
		Weight: 35, Severity: "high", Explanation: "Concurrency fixture", Recommendation: "Synthetic only",
	}

	const callers = 24
	type outcome struct {
		id      uint
		created bool
		valid   bool
		err     error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			submission, result, created, err := CreateOrReplayPendingSubmission(db, draft)
			outcomes <- outcome{id: submission.ID, created: created, valid: result.Valid, err: err}
		}()
	}
	close(start)

	var canonicalID uint
	createdCount := 0
	for i := 0; i < callers; i++ {
		got := <-outcomes
		if got.err != nil {
			t.Fatalf("concurrent caller %d returned error: %v", i, got.err)
		}
		if !got.valid || got.id == 0 {
			t.Fatalf("concurrent caller %d returned invalid result: %+v", i, got)
		}
		if canonicalID == 0 {
			canonicalID = got.id
		} else if got.id != canonicalID {
			t.Fatalf("all exact replays must converge on one ID: want %d got %d", canonicalID, got.id)
		}
		if got.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("database invariant must produce exactly one winner, got %d created callers", createdCount)
	}

	var total int64
	if err := db.Model(&database.RuleSubmission{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("expected exactly one pending row after %d concurrent creates, got %d", callers, total)
	}
	var guarded int64
	if err := db.Model(&database.RuleSubmission{}).
		Where("status = ? AND draft_digest IS NOT NULL", PendingSubmissionStatus).
		Count(&guarded).Error; err != nil {
		t.Fatal(err)
	}
	if guarded != 1 {
		t.Fatalf("expected exactly one non-null pending digest, got %d", guarded)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("concurrent submission replay must not create RiskRule rows, got %d", got)
	}
}
