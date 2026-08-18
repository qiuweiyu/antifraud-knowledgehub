package rule

import (
	"errors"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

func TestPublishApprovedSubmissionCopiesStoredSnapshotAndCreatesAudit(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			db := publicationTestDB(t)
			draft := reviewDraft("publish_snapshot_" + map[bool]string{true: "enabled", false: "disabled"}[enabled])
			draft.Name = "  stored name  "
			draft.Description = "Synthetic publication description"
			draft.Pattern = "  synthetic publication signal  "
			draft.Weight = 47
			draft.Severity = "critical"
			draft.Enabled = &enabled
			draft.Explanation = "Publication fixture explanation"
			draft.Recommendation = "Verify independently."

			submission := createApprovedPublicationSubmission(t, db, draft)
			reviewEvent := mustGetReviewEvent(t, db, submission.ID)
			outcome, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "  publisher-console-a  "})
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Replay {
				t.Fatal("first publication must not be replay")
			}
			if outcome.Submission.Status != ApprovedSubmissionStatus {
				t.Fatalf("publication must preserve approved review status, got %q", outcome.Submission.Status)
			}
			rule := outcome.RiskRule
			if rule.Code != submission.Code || rule.Name != submission.Name || rule.Description != submission.Description ||
				rule.CategoryCode != submission.CategoryCode || rule.RuleType != submission.RuleType || rule.Pattern != submission.Pattern ||
				rule.Weight != submission.Weight || rule.Severity != submission.Severity || rule.Enabled != submission.Enabled ||
				rule.Explanation != submission.Explanation || rule.Recommendation != submission.Recommendation {
				t.Fatalf("published rule must copy stored snapshot exactly: submission=%+v rule=%+v", submission, rule)
			}
			if rule.SourceSubmissionID == nil || *rule.SourceSubmissionID != submission.ID {
				t.Fatalf("published rule must carry server-owned source provenance: %+v", rule.SourceSubmissionID)
			}
			event := outcome.Event
			if event.SubmissionID != submission.ID || event.ReviewEventID != reviewEvent.ID || event.RiskRuleID != rule.ID || event.RiskRuleCode != rule.Code {
				t.Fatalf("unexpected publication audit identifiers: %+v", event)
			}
			if event.ActorKind != ControlledPublisherActorKind || event.ActorLabel != "publisher-console-a" || event.DraftDigest != reviewEvent.DraftDigest || event.CreatedAt.IsZero() {
				t.Fatalf("unexpected publication audit metadata: %+v", event)
			}
			assertPublicationCounts(t, db, 1, 1)
		})
	}
}

func TestManualRiskRuleCreationKeepsPublicationProvenanceNull(t *testing.T) {
	db := publicationTestDB(t)
	manual := reviewDraft("manual_rule_provenance").riskRule()
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}
	if manual.SourceSubmissionID != nil {
		t.Fatalf("manual/direct rule creation must not forge publication provenance: %+v", manual.SourceSubmissionID)
	}
	if got := countPublicationEventRows(t, db); got != 0 {
		t.Fatalf("manual rule creation must create zero publication events, got %d", got)
	}
}

func TestPublicationRejectsInvalidCommandAndUnpublishableSourcesWithoutWrites(t *testing.T) {
	t.Run("invalid actor commands", func(t *testing.T) {
		for _, actor := range []string{"   ", strings.Repeat("界", 41)} {
			db := publicationTestDB(t)
			submission := createApprovedPublicationSubmission(t, db, reviewDraft("invalid_publisher_"+strings.TrimSpace(actor)))
			if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: actor}); !errors.Is(err, ErrInvalidSubmissionPublication) {
				t.Fatalf("expected invalid publication command, got %v", err)
			}
			assertPublicationCounts(t, db, 0, 0)
			assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
		}
	})

	t.Run("pending", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createReviewPendingSubmission(t, db, "publish_pending")
		if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionNotPublishable) {
			t.Fatalf("expected pending source to be not publishable, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, PendingSubmissionStatus)
	})

	t.Run("rejected", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createReviewPendingSubmission(t, db, "publish_rejected")
		if _, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{Decision: RejectedSubmissionStatus, Reason: "reject fixture", ActorLabel: "maintainer-a"}); err != nil {
			t.Fatal(err)
		}
		if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionNotPublishable) {
			t.Fatalf("expected rejected source to be not publishable, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, RejectedSubmissionStatus)
	})

	t.Run("missing", func(t *testing.T) {
		db := publicationTestDB(t)
		if _, err := PublishApprovedSubmission(db, 999999, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("expected missing source, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
	})
}

func TestPublicationFailsClosedOnApprovalIntegrityMismatch(t *testing.T) {
	t.Run("approved status without review event", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createReviewPendingSubmission(t, db, "publish_missing_review")
		if err := db.Model(&database.RuleSubmission{}).Where("id = ?", submission.ID).UpdateColumn("status", ApprovedSubmissionStatus).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionPublicationIntegrity) {
			t.Fatalf("expected missing-review integrity failure, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
	})

	t.Run("rejected event paired with approved status", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createReviewPendingSubmission(t, db, "publish_mismatched_review")
		if _, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{Decision: RejectedSubmissionStatus, Reason: "reject then tamper status fixture", ActorLabel: "maintainer-a"}); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&database.RuleSubmission{}).Where("id = ?", submission.ID).UpdateColumn("status", ApprovedSubmissionStatus).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionPublicationIntegrity) {
			t.Fatalf("expected mismatched-review integrity failure, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
	})

	t.Run("review digest mismatch", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_review_digest_mismatch"))
		if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submission.ID).UpdateColumn("draft_digest", strings.Repeat("0", 64)).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionPublicationIntegrity) {
			t.Fatalf("expected review-digest integrity failure, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
	})

	t.Run("submission digest mismatch", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_submission_digest_mismatch"))
		bad := strings.Repeat("f", 64)
		if err := db.Model(&database.RuleSubmission{}).Where("id = ?", submission.ID).UpdateColumn("draft_digest", bad).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionPublicationIntegrity) {
			t.Fatalf("expected submission-digest integrity failure, got %v", err)
		}
		assertPublicationCounts(t, db, 0, 0)
	})
}

func TestLegacyApprovedNullDigestCanPublishWithoutBackfill(t *testing.T) {
	db := publicationTestDB(t)
	legacy := database.RuleSubmission{
		Status: PendingSubmissionStatus, Code: "legacy_publish_null_digest", Name: "Legacy publish source",
		CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "legacy publication signal",
		Weight: 22, Severity: "medium", Enabled: true,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewPendingSubmission(db, legacy.ID, SubmissionReviewCommand{Decision: ApprovedSubmissionStatus, Reason: "legacy approval", ActorLabel: "maintainer-a"}); err != nil {
		t.Fatal(err)
	}
	var before database.RuleSubmission
	if err := db.First(&before, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if before.DraftDigest != nil {
		t.Fatalf("legacy fixture must keep NULL digest after review, got %+v", before.DraftDigest)
	}

	outcome, err := PublishApprovedSubmission(db, legacy.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Event.DraftDigest == "" {
		t.Fatal("publication event must persist recomputed legacy digest")
	}
	var after database.RuleSubmission
	if err := db.First(&after, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.DraftDigest != nil {
		t.Fatalf("publication must not backfill legacy NULL digest, got %+v", after.DraftDigest)
	}
}

func TestPublicationRevalidatesStoredApprovedSnapshot(t *testing.T) {
	t.Run("category removed", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_missing_category"))
		if err := db.Where("code = ?", submission.CategoryCode).Delete(&database.Category{}).Error; err != nil {
			t.Fatal(err)
		}
		outcome, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
		if !errors.Is(err, ErrSubmissionPublicationValidation) || outcome.Validation.Valid {
			t.Fatalf("expected publication validation failure, outcome=%+v err=%v", outcome, err)
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
	})

	t.Run("duplicate rule appeared", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_duplicate_code"))
		existing := draftRequestFromSubmission(submission).riskRule()
		if err := db.Create(&existing).Error; err != nil {
			t.Fatal(err)
		}
		outcome, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
		if !errors.Is(err, ErrSubmissionPublicationValidation) || outcome.Validation.Valid || len(outcome.Validation.Errors) == 0 || outcome.Validation.Errors[0].Code != "duplicate_code" {
			t.Fatalf("expected duplicate-code publication validation failure, outcome=%+v err=%v", outcome, err)
		}
		if got := countPublicationEventRows(t, db); got != 0 {
			t.Fatalf("failed publication must create no event, got %d", got)
		}
		if got := countRiskRuleRows(t, db); got != 1 {
			t.Fatalf("failed publication must preserve only pre-existing rule, got %d", got)
		}
	})
}

func TestPublicationEventInsertFailureRollsBackRiskRule(t *testing.T) {
	db := publicationTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_event_rollback"))
	if err := db.Exec(`CREATE TRIGGER fail_publication_event_insert BEFORE INSERT ON rule_submission_publication_events BEGIN SELECT RAISE(ABORT, 'injected publication event failure'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); err == nil {
		t.Fatal("expected injected publication event insert failure")
	}
	assertPublicationCounts(t, db, 0, 0)
	assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
	if got := mustGetReviewEvent(t, db, submission.ID); got.Decision != ApprovedSubmissionStatus {
		t.Fatalf("failed publication must not mutate review event: %+v", got)
	}
}

func TestExactPublicationReplayAndDifferentPublisherConflict(t *testing.T) {
	db := publicationTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_replay"))
	first, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "  publisher-a  "})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replay || replay.Event.ID != first.Event.ID || replay.RiskRule.ID != first.RiskRule.ID {
		t.Fatalf("exact retry must resolve to original publication: first=%+v replay=%+v", first, replay)
	}
	if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-b"}); !errors.Is(err, ErrSubmissionPublicationConflict) {
		t.Fatalf("different publisher must conflict, got %v", err)
	}
	assertPublicationCounts(t, db, 1, 1)
}

func TestPublicationReadHelpersAreReadOnly(t *testing.T) {
	db := publicationTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_reads"))
	published, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	beforeRules := countRiskRuleRows(t, db)
	beforeEvents := countPublicationEventRows(t, db)
	beforeReviews := countReviewEventRows(t, db)

	bySubmission, err := GetSubmissionPublicationEvent(db, submission.ID)
	if err != nil || bySubmission.ID != published.Event.ID {
		t.Fatalf("unexpected publication lookup by submission: %+v err=%v", bySubmission, err)
	}
	byReview, err := GetSubmissionPublicationEventByReviewEvent(db, published.ReviewEvent.ID)
	if err != nil || byReview.ID != published.Event.ID {
		t.Fatalf("unexpected publication lookup by review event: %+v err=%v", byReview, err)
	}
	byRuleID, err := ListSubmissionPublicationEventsByRiskRuleID(db, published.RiskRule.ID)
	if err != nil || len(byRuleID) != 1 || byRuleID[0].ID != published.Event.ID {
		t.Fatalf("unexpected publication history by rule ID: %+v err=%v", byRuleID, err)
	}
	byCode, err := ListSubmissionPublicationEventsByRiskRuleCode(db, published.RiskRule.Code)
	if err != nil || len(byCode) != 1 || byCode[0].ID != published.Event.ID {
		t.Fatalf("unexpected publication history by code: %+v err=%v", byCode, err)
	}
	source, err := GetRiskRulePublicationSource(db, published.RiskRule.ID)
	if err != nil || source.ID != submission.ID {
		t.Fatalf("unexpected current rule provenance source: %+v err=%v", source, err)
	}
	if countRiskRuleRows(t, db) != beforeRules || countPublicationEventRows(t, db) != beforeEvents || countReviewEventRows(t, db) != beforeReviews {
		t.Fatal("publication read helpers must not mutate rules, publication events, or review events")
	}
}

func TestPublishedRuleLifecycleDoesNotRewriteAuditOrAutoRepublish(t *testing.T) {
	db := publicationTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_lifecycle"))
	published, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	originalEvent := published.Event

	if err := db.Model(&database.RiskRule{}).Where("id = ?", published.RiskRule.ID).Updates(map[string]any{"name": "mutated after publication", "enabled": false}).Error; err != nil {
		t.Fatal(err)
	}
	replay, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replay || replay.RiskRule.Name != "mutated after publication" || replay.RiskRule.Enabled {
		t.Fatalf("replay must return current rule without overwriting later changes: %+v", replay.RiskRule)
	}
	storedEvent, err := GetSubmissionPublicationEvent(db, submission.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedEvent.DraftDigest != originalEvent.DraftDigest || storedEvent.RiskRuleCode != originalEvent.RiskRuleCode || storedEvent.ActorLabel != originalEvent.ActorLabel {
		t.Fatalf("later rule mutation must not rewrite publication audit: before=%+v after=%+v", originalEvent, storedEvent)
	}

	if err := db.Delete(&database.RiskRule{}, published.RiskRule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got := countPublicationEventRows(t, db); got != 1 {
		t.Fatalf("hard delete must not remove publication history, got %d events", got)
	}
	if _, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"}); !errors.Is(err, ErrSubmissionPublicationConflict) {
		t.Fatalf("retry after hard delete must not recreate rule, got %v", err)
	}
	assertPublicationCounts(t, db, 0, 1)
	assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
}

func TestPublicationSourceRelationshipsRestrictDeletion(t *testing.T) {
	db := publicationTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("publish_fk_restrict"))
	published, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&database.RuleSubmissionReviewEvent{}, published.ReviewEvent.ID).Error; err == nil {
		t.Fatal("expected publication FK to restrict source review-event deletion")
	}
	if err := db.Delete(&database.RuleSubmission{}, submission.ID).Error; err == nil {
		t.Fatal("expected audit relationships to restrict source submission deletion")
	}
	assertPublicationCounts(t, db, 1, 1)
}

func publicationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := reviewTestDB(t)
	if err := db.AutoMigrate(&database.RuleSubmissionPublicationEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func createApprovedPublicationSubmission(t *testing.T, db *gorm.DB, draft DraftRequest) database.RuleSubmission {
	t.Helper()
	submission, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || !created {
		t.Fatalf("create publication source failed: created=%v result=%+v err=%v", created, result, err)
	}
	outcome, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{
		Decision: ApprovedSubmissionStatus,
		Reason: "Synthetic publication source approved for persistence tests.",
		ActorLabel: "maintainer-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	return outcome.Submission
}

func mustGetReviewEvent(t *testing.T, db *gorm.DB, submissionID uint) database.RuleSubmissionReviewEvent {
	t.Helper()
	event, err := GetSubmissionReviewEvent(db, submissionID)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func countPublicationEventRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&database.RuleSubmissionPublicationEvent{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func assertPublicationCounts(t *testing.T, db *gorm.DB, wantRules, wantEvents int64) {
	t.Helper()
	if got := countRiskRuleRows(t, db); got != wantRules {
		t.Fatalf("unexpected RiskRule count: want=%d got=%d", wantRules, got)
	}
	if got := countPublicationEventRows(t, db); got != wantEvents {
		t.Fatalf("unexpected publication event count: want=%d got=%d", wantEvents, got)
	}
}

func assertSubmissionStatus(t *testing.T, db *gorm.DB, submissionID uint, want string) {
	t.Helper()
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		t.Fatal(err)
	}
	if submission.Status != want {
		t.Fatalf("submission status changed unexpectedly: want=%q got=%q", want, submission.Status)
	}
}
