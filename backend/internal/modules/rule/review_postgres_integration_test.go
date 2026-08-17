package rule

import (
	"errors"
	"os"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresConcurrentIdenticalReviewsConvergeOnOneTerminalEvent(t *testing.T) {
	db := reviewPostgresTestDB(t)
	submission := createReviewPendingSubmission(t, db, "postgres_review_same_command")
	command := SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "Concurrent reviewers sent the exact same trusted command.",
		ActorLabel: "maintainer-console-a",
	}

	const callers = 24
	type outcome struct {
		eventID uint
		status  string
		replay  bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan outcome, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			got, err := ReviewPendingSubmission(db, submission.ID, command)
			results <- outcome{eventID: got.Event.ID, status: got.Submission.Status, replay: got.Replay, err: err}
		}()
	}
	close(start)

	var canonicalEventID uint
	createdCount := 0
	for i := 0; i < callers; i++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent identical review %d failed: %v", i, got.err)
		}
		if got.status != ApprovedSubmissionStatus || got.eventID == 0 {
			t.Fatalf("unexpected concurrent review outcome %d: %+v", i, got)
		}
		if canonicalEventID == 0 {
			canonicalEventID = got.eventID
		} else if got.eventID != canonicalEventID {
			t.Fatalf("all exact review retries must resolve to one event: want=%d got=%d", canonicalEventID, got.eventID)
		}
		if !got.replay {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected exactly one terminal review winner, got %d non-replay callers", createdCount)
	}

	assertPostgresReviewIntegrity(t, db, submission.ID, ApprovedSubmissionStatus)
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("concurrent approval must create zero RiskRule rows, got %d", got)
	}
}

func TestPostgresConcurrentApproveRejectAllowsExactlyOneDecision(t *testing.T) {
	db := reviewPostgresTestDB(t)
	submission := createReviewPendingSubmission(t, db, "postgres_review_conflict")
	commands := []SubmissionReviewCommand{
		{Decision: ApprovedSubmissionStatus, Reason: "Approve race candidate.", ActorLabel: "maintainer-a"},
		{Decision: RejectedSubmissionStatus, Reason: "Reject race candidate.", ActorLabel: "maintainer-b"},
	}

	type outcome struct {
		decision string
		review   SubmissionReviewOutcome
		err      error
	}
	start := make(chan struct{})
	results := make(chan outcome, len(commands))
	for _, command := range commands {
		command := command
		go func() {
			<-start
			got, err := ReviewPendingSubmission(db, submission.ID, command)
			results <- outcome{decision: command.Decision, review: got, err: err}
		}()
	}
	close(start)

	successes := 0
	conflicts := 0
	winnerStatus := ""
	for range commands {
		got := <-results
		switch {
		case got.err == nil:
			successes++
			winnerStatus = got.review.Submission.Status
			if got.review.Replay {
				t.Fatalf("different concurrent commands cannot resolve as an exact replay: %+v", got)
			}
		case errors.Is(got.err, ErrSubmissionReviewConflict):
			conflicts++
		default:
			t.Fatalf("unexpected approve/reject race error for %s: %v", got.decision, got.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one winner and one conflict, got successes=%d conflicts=%d", successes, conflicts)
	}
	if winnerStatus != ApprovedSubmissionStatus && winnerStatus != RejectedSubmissionStatus {
		t.Fatalf("unexpected winning terminal status %q", winnerStatus)
	}

	assertPostgresReviewIntegrity(t, db, submission.ID, winnerStatus)
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("review conflict race must create zero RiskRule rows, got %d", got)
	}
}

func TestPostgresReviewedSubmissionDeleteIsRestrictedByAuditFK(t *testing.T) {
	db := reviewPostgresTestDB(t)
	submission := createReviewPendingSubmission(t, db, "postgres_review_fk")
	if _, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
		Decision: RejectedSubmissionStatus,
		Reason: "FK restriction fixture.",
		ActorLabel: "maintainer-a",
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.Delete(&database.RuleSubmission{}, submission.ID).Error; err == nil {
		t.Fatal("expected reviewed submission delete to be rejected by foreign key")
	}
	var count int64
	if err := db.Model(&database.RuleSubmission{}).Where("id = ?", submission.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reviewed submission must remain after restricted delete, got count=%d", count)
	}
	if got := countReviewEventRows(t, db); got != 1 {
		t.Fatalf("review audit event must remain after restricted delete, got %d", got)
	}
}

func TestPostgresReviewEventDatabaseConstraintsRejectInvalidTransition(t *testing.T) {
	db := reviewPostgresTestDB(t)
	submission := createReviewPendingSubmission(t, db, "postgres_review_checks")
	digest, err := database.RuleSubmissionDraftDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	invalid := database.RuleSubmissionReviewEvent{
		SubmissionID: submission.ID,
		Decision: "change_requested",
		FromStatus: PendingSubmissionStatus,
		ToStatus: "change_requested",
		Reason: "invalid direct fixture",
		ActorKind: ControlledMaintainerActorKind,
		ActorLabel: "maintainer-a",
		DraftDigest: digest,
	}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("expected database CHECK constraint to reject unsupported review transition")
	}
	if got := countReviewEventRows(t, db); got != 0 {
		t.Fatalf("invalid direct event must not persist, got %d", got)
	}
}

func reviewPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(postgresSubmissionIntegrationDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not configured", postgresSubmissionIntegrationDSNEnv)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL review integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(32)
	drop := func() {
		_ = db.Migrator().DropTable(&database.RuleSubmissionReviewEvent{}, &database.RuleSubmission{}, &database.RiskRule{}, &database.Category{})
	}
	drop()
	t.Cleanup(func() {
		drop()
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}, &database.RuleSubmissionReviewEvent{}); err != nil {
		t.Fatalf("migrate PostgreSQL review integration schema: %v", err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatalf("prepare PostgreSQL submission idempotency invariant: %v", err)
	}
	if err := db.Create(&database.Category{
		Code: "fake_customer_service", Name: "PostgreSQL review integration category", SeverityDefault: "high",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func assertPostgresReviewIntegrity(t *testing.T, db *gorm.DB, submissionID uint, expectedStatus string) {
	t.Helper()
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		t.Fatal(err)
	}
	if submission.Status != expectedStatus {
		t.Fatalf("expected submission status %q, got %q", expectedStatus, submission.Status)
	}
	var events []database.RuleSubmissionReviewEvent
	if err := db.Where("submission_id = ?", submissionID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one terminal review event, got %d", len(events))
	}
	if events[0].Decision != expectedStatus || events[0].FromStatus != PendingSubmissionStatus || events[0].ToStatus != expectedStatus {
		t.Fatalf("status/event mismatch after concurrent review: submission=%+v event=%+v", submission, events[0])
	}
}
