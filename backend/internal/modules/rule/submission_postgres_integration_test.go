package rule

import (
	"os"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const postgresSubmissionIntegrationDSNEnv = "AFKH_POSTGRES_INTEGRATION_DSN"

type concurrentSubmissionOutcome struct {
	id      uint
	created bool
	valid   bool
	err     error
}

func postgresSubmissionIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
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
	drop := func() {
		_ = db.Migrator().DropTable(
			&database.RuleSubmissionPublicationEvent{},
			&database.RuleSubmissionReviewEvent{},
			&database.RuleSubmission{},
			&database.RiskRuleVersion{},
			&database.RiskRule{},
			&database.Category{},
		)
	}
	drop()
	t.Cleanup(func() {
		drop()
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RiskRuleVersion{}, &database.RuleSubmission{}); err != nil {
		t.Fatalf("migrate PostgreSQL integration schema: %v", err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatalf("prepare PostgreSQL idempotency invariant: %v", err)
	}
	if db.Migrator().HasIndex(&database.RuleSubmission{}, database.RuleSubmissionPendingDigestIndex) {
		t.Fatalf("legacy PostgreSQL partial unique index %s must be removed", database.RuleSubmissionPendingDigestIndex)
	}
	if !db.Migrator().HasIndex(&database.RuleSubmission{}, database.RuleSubmissionPendingRequestDigestIndex) {
		t.Fatalf("expected PostgreSQL partial unique index %s", database.RuleSubmissionPendingRequestDigestIndex)
	}
	if err := db.Create(&database.Category{
		Code: "fake_customer_service", Name: "PostgreSQL integration category", SeverityDefault: "high",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresConcurrentExactReplayConvergesOnOnePendingSubmission(t *testing.T) {
	db := postgresSubmissionIntegrationDB(t)
	draft := DraftRequest{
		Code: "postgres_concurrent_replay", Name: "PostgreSQL concurrent replay",
		CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "synthetic concurrent signal",
		Weight: 35, Severity: "high", Explanation: "Concurrency fixture", Recommendation: "Synthetic only",
	}

	const callers = 24
	start := make(chan struct{})
	outcomes := make(chan concurrentSubmissionOutcome, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			submission, result, created, err := CreateOrReplayPendingSubmission(db, draft)
			outcomes <- concurrentSubmissionOutcome{id: submission.ID, created: created, valid: result.Valid, err: err}
		}()
	}
	close(start)

	assertConcurrentSubmissionConvergence(t, outcomes, callers)

	var total int64
	if err := db.Model(&database.RuleSubmission{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("expected exactly one pending row after %d concurrent creates, got %d", callers, total)
	}
	var guarded int64
	if err := db.Model(&database.RuleSubmission{}).
		Where("status = ? AND request_digest IS NOT NULL", PendingSubmissionStatus).
		Count(&guarded).Error; err != nil {
		t.Fatal(err)
	}
	if guarded != 1 {
		t.Fatalf("expected exactly one non-null pending request digest, got %d", guarded)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("concurrent submission replay must not create RiskRule rows, got %d", got)
	}
}

func TestPostgresConcurrentExactRevisionReplayConvergesWithoutMutatingRule(t *testing.T) {
	db := postgresSubmissionIntegrationDB(t)
	target := database.RiskRule{
		Code: "postgres_revision_replay", Name: "PostgreSQL revision target",
		CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "synthetic revision base",
		Weight: 40, Severity: "high", Enabled: true, Explanation: "Synthetic", Recommendation: "Verify",
		Version: 1,
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	base, err := database.BuildRiskRuleVersion(target, 1, database.RiskRuleVersionSourceLegacyBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	beforeDigest, err := database.RiskRuleSnapshotDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	request := RevisionDraftRequest{
		BaseVersion: 1, Name: "PostgreSQL revision target revised", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "synthetic revision base", Weight: 40, Severity: "high", Enabled: &enabled,
		Explanation: "Synthetic", Recommendation: "Verify",
	}

	const callers = 24
	start := make(chan struct{})
	outcomes := make(chan concurrentSubmissionOutcome, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			submission, result, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
			outcomes <- concurrentSubmissionOutcome{id: submission.ID, created: created, valid: result.Valid, err: err}
		}()
	}
	close(start)
	assertConcurrentSubmissionConvergence(t, outcomes, callers)

	var total int64
	if err := db.Model(&database.RuleSubmission{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("expected one pending revision after concurrent exact replay, got %d", total)
	}
	var current database.RiskRule
	if err := db.First(&current, target.ID).Error; err != nil {
		t.Fatal(err)
	}
	afterDigest, err := database.RiskRuleSnapshotDigest(current)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 1 || afterDigest != beforeDigest {
		t.Fatalf("concurrent revision proposals mutated executable Rule: version=%d digest=%s", current.Version, afterDigest)
	}
	var versionCount int64
	if err := db.Model(&database.RiskRuleVersion{}).Where("risk_rule_id = ?", target.ID).Count(&versionCount).Error; err != nil {
		t.Fatal(err)
	}
	if versionCount != 1 {
		t.Fatalf("I2 revision proposal must append zero history versions, total=%d", versionCount)
	}
}

func assertConcurrentSubmissionConvergence(t *testing.T, outcomes <-chan concurrentSubmissionOutcome, callers int) {
	t.Helper()
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
}
