package rule

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/gin-gonic/gin"
)

func TestReviewPendingRevisionSubmissionRevalidatesAndDoesNotMutateRule(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_review_approve")
	beforeDigest, err := database.RiskRuleSnapshotDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	proposal, _, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, revisionRequestFromRule(target))
	if err != nil || !created {
		t.Fatalf("create revision proposal: created=%v err=%v", created, err)
	}

	outcome, err := ReviewPendingSubmission(db, proposal.ID, SubmissionReviewCommand{
		Decision:   ApprovedSubmissionStatus,
		Reason:     "synthetic revision approval",
		ActorLabel: "maintainer-revision-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Submission.Status != ApprovedSubmissionStatus || outcome.Event.Decision != ApprovedSubmissionStatus || outcome.Replay {
		t.Fatalf("unexpected revision review outcome: %+v", outcome)
	}

	var current database.RiskRule
	if err := db.First(&current, target.ID).Error; err != nil {
		t.Fatal(err)
	}
	afterDigest, err := database.RiskRuleSnapshotDigest(current)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != target.Version || afterDigest != beforeDigest {
		t.Fatalf("approval in I2 must not mutate executable rule: version=%d digest=%s", current.Version, afterDigest)
	}
	var historyCount int64
	if err := db.Model(&database.RiskRuleVersion{}).Where("risk_rule_id = ?", target.ID).Count(&historyCount).Error; err != nil {
		t.Fatal(err)
	}
	if historyCount != 1 {
		t.Fatalf("I2 approval must not append a rule version, got %d", historyCount)
	}
}

func TestReviewPendingRevisionSubmissionApprovalRejectsStaleBase(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_review_stale")
	proposal, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, revisionRequestFromRule(target))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&database.RiskRule{}).Where("id = ?", target.ID).UpdateColumn("version", 2).Error; err != nil {
		t.Fatal(err)
	}

	outcome, err := ReviewPendingSubmission(db, proposal.ID, SubmissionReviewCommand{
		Decision:   ApprovedSubmissionStatus,
		Reason:     "must not silently rebase",
		ActorLabel: "maintainer-revision-test",
	})
	if !errors.Is(err, ErrSubmissionApprovalValidation) {
		t.Fatalf("expected approval validation conflict, got %v", err)
	}
	if outcome.Validation.Valid || len(outcome.Validation.Errors) == 0 || outcome.Validation.Errors[0].Code != "stale_base_version" {
		t.Fatalf("expected stale_base_version validation, got %+v", outcome.Validation)
	}

	var stored database.RuleSubmission
	if err := db.First(&stored, proposal.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != PendingSubmissionStatus {
		t.Fatalf("stale approval attempt must leave proposal pending, got %q", stored.Status)
	}
	var reviewCount int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", proposal.ID).Count(&reviewCount).Error; err != nil {
		t.Fatal(err)
	}
	if reviewCount != 0 {
		t.Fatalf("stale approval must append zero review events, got %d", reviewCount)
	}
}

func TestReviewPendingRevisionSubmissionRejectCanCloseStaleProposal(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_review_reject_stale")
	proposal, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, revisionRequestFromRule(target))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&database.RiskRule{}).Where("id = ?", target.ID).UpdateColumn("version", 2).Error; err != nil {
		t.Fatal(err)
	}

	outcome, err := ReviewPendingSubmission(db, proposal.ID, SubmissionReviewCommand{
		Decision:   RejectedSubmissionStatus,
		Reason:     "reject obsolete proposal",
		ActorLabel: "maintainer-revision-test",
	})
	if err != nil {
		t.Fatalf("rejection must remain possible after base becomes stale: %v", err)
	}
	if outcome.Submission.Status != RejectedSubmissionStatus || outcome.Event.Decision != RejectedSubmissionStatus {
		t.Fatalf("unexpected rejection outcome: %+v", outcome)
	}
}

func TestReviewPendingRevisionSubmissionFailsClosedOnProjectionHistoryMismatch(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_review_integrity")
	proposal, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, revisionRequestFromRule(target))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&database.RiskRule{}).Where("id = ?", target.ID).UpdateColumn("name", "out of band current drift").Error; err != nil {
		t.Fatal(err)
	}

	_, err = ReviewPendingSubmission(db, proposal.ID, SubmissionReviewCommand{
		Decision:   ApprovedSubmissionStatus,
		Reason:     "must fail integrity",
		ActorLabel: "maintainer-revision-test",
	})
	if !errors.Is(err, ErrSubmissionReviewIntegrity) {
		t.Fatalf("projection/history mismatch must fail review integrity, got %v", err)
	}
	var stored database.RuleSubmission
	if err := db.First(&stored, proposal.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != PendingSubmissionStatus {
		t.Fatalf("integrity failure must leave proposal pending, got %q", stored.Status)
	}
}

func TestApprovedRevisionPublicationIsBlockedBeforeI3WithoutRuleMutation(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_publication_guard")
	beforeDigest, err := database.RiskRuleSnapshotDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	proposal, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, revisionRequestFromRule(target))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewPendingSubmission(db, proposal.ID, SubmissionReviewCommand{
		Decision:   ApprovedSubmissionStatus,
		Reason:     "approve for guard test",
		ActorLabel: "maintainer-revision-test",
	}); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(
		"/rule-submissions/:id/publications",
		RevisionPublicationI2Guard(db),
		SubmissionPublicationHandler(db, "publisher-revision-test"),
	)
	req := httptest.NewRequest(http.MethodPost, "/rule-submissions/"+uintString(proposal.ID)+"/publications", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), `"code":"submission_not_publishable"`) {
		t.Fatalf("approved revision publication must be blocked before I3: %d %s", resp.Code, resp.Body.String())
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
		t.Fatalf("blocked publication mutated current rule: version=%d digest=%s", current.Version, afterDigest)
	}
	var publicationCount int64
	if err := db.Model(&database.RuleSubmissionPublicationEvent{}).Count(&publicationCount).Error; err != nil {
		t.Fatal(err)
	}
	if publicationCount != 0 {
		t.Fatalf("blocked revision publication must append zero publication events, got %d", publicationCount)
	}
	var versionCount int64
	if err := db.Model(&database.RiskRuleVersion{}).Where("risk_rule_id = ?", target.ID).Count(&versionCount).Error; err != nil {
		t.Fatal(err)
	}
	if versionCount != 1 {
		t.Fatalf("blocked revision publication must append zero versions, total=%d", versionCount)
	}
}
