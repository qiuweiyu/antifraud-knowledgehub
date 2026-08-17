package rule

import (
	"errors"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestApprovePendingSubmissionCreatesOneAuditEventAndNoRiskRule(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_approve")

	outcome, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "  Synthetic rule is explainable and bounded.  ",
		ActorLabel: "  maintainer-console-a  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Replay {
		t.Fatal("first review must not be marked as replay")
	}
	if outcome.Submission.Status != ApprovedSubmissionStatus || outcome.Event.Decision != ApprovedSubmissionStatus {
		t.Fatalf("unexpected approval outcome: %+v", outcome)
	}
	if outcome.Event.FromStatus != PendingSubmissionStatus || outcome.Event.ToStatus != ApprovedSubmissionStatus {
		t.Fatalf("unexpected transition audit: %+v", outcome.Event)
	}
	if outcome.Event.Reason != "Synthetic rule is explainable and bounded." {
		t.Fatalf("reason must be trimmed, got %q", outcome.Event.Reason)
	}
	if outcome.Event.ActorKind != ControlledMaintainerActorKind || outcome.Event.ActorLabel != "maintainer-console-a" {
		t.Fatalf("unexpected actor attribution: %+v", outcome.Event)
	}
	if outcome.Event.DraftDigest == "" || len(outcome.Event.DraftDigest) != 64 {
		t.Fatalf("expected deterministic reviewed digest, got %q", outcome.Event.DraftDigest)
	}
	if got := countReviewEventRows(t, db); got != 1 {
		t.Fatalf("expected one review event, got %d", got)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("approval must not publish RiskRule rows, got %d", got)
	}
}

func TestRejectPendingSubmissionDoesNotRequireCurrentDraftValidation(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_reject_stale")
	if err := db.Where("code = ?", "fake_customer_service").Delete(&database.Category{}).Error; err != nil {
		t.Fatal(err)
	}

	outcome, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
		Decision: RejectedSubmissionStatus,
		Reason: "Category was removed before review.",
		ActorLabel: "maintainer-console-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Submission.Status != RejectedSubmissionStatus || outcome.Event.Decision != RejectedSubmissionStatus {
		t.Fatalf("expected stale submission to remain rejectable: %+v", outcome)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("rejection must not publish RiskRule rows, got %d", got)
	}
}

func TestApprovalRevalidatesStoredSnapshotAndWritesNothingOnFailure(t *testing.T) {
	t.Run("category removed", func(t *testing.T) {
		db := reviewTestDB(t)
		submission := createReviewPendingSubmission(t, db, "review_missing_category")
		if err := db.Where("code = ?", "fake_customer_service").Delete(&database.Category{}).Error; err != nil {
			t.Fatal(err)
		}

		outcome, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
			Decision: ApprovedSubmissionStatus,
			Reason: "Should not commit.",
			ActorLabel: "maintainer-console-a",
		})
		if !errors.Is(err, ErrSubmissionApprovalValidation) {
			t.Fatalf("expected approval validation failure, got %v", err)
		}
		if outcome.Validation.Valid || len(outcome.Validation.Errors) == 0 {
			t.Fatalf("expected concrete validation errors, got %+v", outcome.Validation)
		}
		assertReviewStillPendingWithNoEvent(t, db, submission.ID)
		if got := countRiskRuleRows(t, db); got != 0 {
			t.Fatalf("failed approval must not create RiskRule rows, got %d", got)
		}
	})

	t.Run("risk rule appeared", func(t *testing.T) {
		db := reviewTestDB(t)
		submission := createReviewPendingSubmission(t, db, "review_duplicate_rule")
		if err := db.Create(&database.RiskRule{
			Code: submission.Code, Name: "Existing active rule", CategoryCode: submission.CategoryCode,
			RuleType: "keyword", Pattern: "existing active signal", Weight: 50, Severity: "high", Enabled: true,
		}).Error; err != nil {
			t.Fatal(err)
		}

		outcome, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
			Decision: ApprovedSubmissionStatus,
			Reason: "Should be blocked by current duplicate rule.",
			ActorLabel: "maintainer-console-a",
		})
		if !errors.Is(err, ErrSubmissionApprovalValidation) {
			t.Fatalf("expected duplicate-code validation failure, got %v", err)
		}
		if outcome.Validation.Valid {
			t.Fatalf("expected invalid current draft, got %+v", outcome.Validation)
		}
		assertReviewStillPendingWithNoEvent(t, db, submission.ID)
		if got := countRiskRuleRows(t, db); got != 1 {
			t.Fatalf("review must not mutate existing RiskRule state, got %d rows", got)
		}
	})
}

func TestInvalidReviewCommandsProduceZeroWrites(t *testing.T) {
	tests := []struct {
		name    string
		command SubmissionReviewCommand
	}{
		{name: "invalid decision", command: SubmissionReviewCommand{Decision: "change_requested", Reason: "reason", ActorLabel: "maintainer"}},
		{name: "blank reason", command: SubmissionReviewCommand{Decision: ApprovedSubmissionStatus, Reason: "   ", ActorLabel: "maintainer"}},
		{name: "oversized reason", command: SubmissionReviewCommand{Decision: ApprovedSubmissionStatus, Reason: strings.Repeat("界", 667), ActorLabel: "maintainer"}},
		{name: "blank actor", command: SubmissionReviewCommand{Decision: RejectedSubmissionStatus, Reason: "reason", ActorLabel: "   "}},
		{name: "oversized actor", command: SubmissionReviewCommand{Decision: RejectedSubmissionStatus, Reason: "reason", ActorLabel: strings.Repeat("界", 41)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := reviewTestDB(t)
			submission := createReviewPendingSubmission(t, db, "invalid_"+strings.ReplaceAll(tt.name, " ", "_"))
			if _, err := ReviewPendingSubmission(db, submission.ID, tt.command); !errors.Is(err, ErrInvalidSubmissionReview) {
				t.Fatalf("expected invalid-command error, got %v", err)
			}
			assertReviewStillPendingWithNoEvent(t, db, submission.ID)
		})
	}
}

func TestExactReviewRetryReplaysAndDifferentSecondCommandConflicts(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_retry")
	firstCommand := SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "  bounded review reason  ",
		ActorLabel: "  maintainer-a  ",
	}
	first, err := ReviewPendingSubmission(db, submission.ID, firstCommand)
	if err != nil {
		t.Fatal(err)
	}

	replay, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "bounded review reason",
		ActorLabel: "maintainer-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replay || replay.Event.ID != first.Event.ID || replay.Submission.ID != first.Submission.ID {
		t.Fatalf("exact retry must resolve to the original review: first=%+v replay=%+v", first, replay)
	}
	if got := countReviewEventRows(t, db); got != 1 {
		t.Fatalf("exact retry must not append another event, got %d", got)
	}

	conflicts := []SubmissionReviewCommand{
		{Decision: RejectedSubmissionStatus, Reason: "bounded review reason", ActorLabel: "maintainer-a"},
		{Decision: ApprovedSubmissionStatus, Reason: "different reason", ActorLabel: "maintainer-a"},
		{Decision: ApprovedSubmissionStatus, Reason: "bounded review reason", ActorLabel: "maintainer-b"},
	}
	for _, command := range conflicts {
		if _, err := ReviewPendingSubmission(db, submission.ID, command); !errors.Is(err, ErrSubmissionReviewConflict) {
			t.Fatalf("different second command must conflict: command=%+v err=%v", command, err)
		}
	}
	if got := countReviewEventRows(t, db); got != 1 {
		t.Fatalf("conflicts must not append events, got %d", got)
	}
}

func TestLegacyNullDigestReviewComputesAuditDigestWithoutBackfill(t *testing.T) {
	db := reviewTestDB(t)
	legacy := database.RuleSubmission{
		Status: PendingSubmissionStatus, Code: "legacy_review_digest", Name: "Legacy review digest",
		CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "legacy synthetic signal",
		Weight: 20, Severity: "medium", Enabled: true,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.DraftDigest != nil {
		t.Fatalf("fixture must start with NULL digest, got %+v", legacy.DraftDigest)
	}
	expectedDigest, err := database.RuleSubmissionDraftDigest(legacy)
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := ReviewPendingSubmission(db, legacy.ID, SubmissionReviewCommand{
		Decision: RejectedSubmissionStatus,
		Reason: "Legacy duplicate can still be reviewed.",
		ActorLabel: "maintainer-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Event.DraftDigest != expectedDigest {
		t.Fatalf("event must bind to stored legacy snapshot: want %s got %s", expectedDigest, outcome.Event.DraftDigest)
	}
	var stored database.RuleSubmission
	if err := db.First(&stored, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DraftDigest != nil {
		t.Fatalf("review must not rewrite legacy NULL digest, got %+v", stored.DraftDigest)
	}
}

func TestReviewEventInsertFailureRollsBackStatusTransition(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_rollback")
	if err := db.Exec(`CREATE TRIGGER fail_review_event_insert BEFORE INSERT ON rule_submission_review_events BEGIN SELECT RAISE(ABORT, 'injected review event failure'); END;`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "This event insert is forced to fail.",
		ActorLabel: "maintainer-a",
	}); err == nil {
		t.Fatal("expected injected event insert failure")
	}
	assertReviewStillPendingWithNoEvent(t, db, submission.ID)
}

func TestReviewReadHelpersAreOrderedAndReadOnly(t *testing.T) {
	db := reviewTestDB(t)
	pending := createReviewPendingSubmission(t, db, "review_read_pending")
	before, err := GetSubmissionReviewState(db, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Submission.Status != PendingSubmissionStatus || before.Event != nil {
		t.Fatalf("unexpected pending review state: %+v", before)
	}

	approved := createReviewPendingSubmission(t, db, "review_read_approved")
	outcome, err := ReviewPendingSubmission(db, approved.ID, SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "Read helper fixture.",
		ActorLabel: "maintainer-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeSubmissions := countSubmissionRows(t, db)
	beforeEvents := countReviewEventRows(t, db)
	beforeRules := countRiskRuleRows(t, db)

	events, err := ListSubmissionReviewEvents(db, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != outcome.Event.ID {
		t.Fatalf("unexpected review event list: %+v", events)
	}
	gotEvent, err := GetSubmissionReviewEvent(db, approved.ID)
	if err != nil || gotEvent.ID != outcome.Event.ID {
		t.Fatalf("unexpected review event lookup: event=%+v err=%v", gotEvent, err)
	}
	state, err := GetSubmissionReviewState(db, approved.ID)
	if err != nil || state.Event == nil || state.Event.ID != outcome.Event.ID || state.Submission.Status != ApprovedSubmissionStatus {
		t.Fatalf("unexpected terminal review state: state=%+v err=%v", state, err)
	}
	if got := countSubmissionRows(t, db); got != beforeSubmissions {
		t.Fatalf("review reads must not mutate submissions: before=%d after=%d", beforeSubmissions, got)
	}
	if got := countReviewEventRows(t, db); got != beforeEvents {
		t.Fatalf("review reads must not mutate events: before=%d after=%d", beforeEvents, got)
	}
	if got := countRiskRuleRows(t, db); got != beforeRules {
		t.Fatalf("review reads must not mutate RiskRule rows: before=%d after=%d", beforeRules, got)
	}
}

func TestReviewedDigestNoLongerOccupiesPendingReplayIndex(t *testing.T) {
	db := reviewTestDB(t)
	draft := reviewDraft("review_resubmit")
	first, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || !created {
		t.Fatalf("initial submission failed: created=%v result=%+v err=%v", created, result, err)
	}
	if _, err := ReviewPendingSubmission(db, first.ID, SubmissionReviewCommand{
		Decision: RejectedSubmissionStatus,
		Reason: "Rejected first proposal.",
		ActorLabel: "maintainer-a",
	}); err != nil {
		t.Fatal(err)
	}
	second, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || !created {
		t.Fatalf("terminal digest must allow a new pending proposal: created=%v result=%+v err=%v", created, result, err)
	}
	if first.ID == second.ID || second.Status != PendingSubmissionStatus {
		t.Fatalf("expected a new pending submission after terminal review: first=%+v second=%+v", first, second)
	}
}

func reviewTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}, &database.RuleSubmissionReviewEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Category{
		Code: "fake_customer_service", Name: "Review test category", SeverityDefault: "high",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func reviewDraft(code string) DraftRequest {
	return DraftRequest{
		Code: code, Name: code, CategoryCode: "fake_customer_service", RuleType: "keyword",
		Pattern: "synthetic review signal " + code, Weight: 30, Severity: "high",
		Explanation: "Synthetic review fixture", Recommendation: "Verify via official channels.",
	}
}

func createReviewPendingSubmission(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	submission, result, created, err := CreateOrReplayPendingSubmission(db, reviewDraft(code))
	if err != nil || !result.Valid || !created {
		t.Fatalf("create review fixture failed: created=%v result=%+v err=%v", created, result, err)
	}
	return submission
}

func countReviewEventRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func assertReviewStillPendingWithNoEvent(t *testing.T, db *gorm.DB, submissionID uint) {
	t.Helper()
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		t.Fatal(err)
	}
	if submission.Status != PendingSubmissionStatus {
		t.Fatalf("expected submission to remain pending, got %q", submission.Status)
	}
	var count int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submissionID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected zero review events, got %d", count)
	}
}
