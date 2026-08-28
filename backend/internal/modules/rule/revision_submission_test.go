package rule

import (
	"errors"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func revisionSubmissionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&database.Category{},
		&database.RiskRule{},
		&database.RiskRuleVersion{},
		&database.RuleSubmission{},
		&database.RuleSubmissionReviewEvent{},
		&database.RuleSubmissionPublicationEvent{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Category{Code: "fake_customer_service", Name: "Synthetic category", SeverityDefault: "high"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func createRevisionTarget(t *testing.T, db *gorm.DB, code string) database.RiskRule {
	t.Helper()
	target := database.RiskRule{
		Code:           code,
		Name:           "Original rule",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "original synthetic signal",
		Weight:         30,
		Severity:       "high",
		Enabled:        true,
		Explanation:    "Original explanation",
		Recommendation: "Original recommendation",
		Version:        1,
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	version, err := database.BuildRiskRuleVersion(target, 1, database.RiskRuleVersionSourceLegacyBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	return target
}

func revisionRequestFromRule(rule database.RiskRule) RevisionDraftRequest {
	enabled := rule.Enabled
	return RevisionDraftRequest{
		BaseVersion:    rule.Version,
		Name:           rule.Name + " revised",
		Description:    rule.Description,
		CategoryCode:   rule.CategoryCode,
		RuleType:       rule.RuleType,
		Pattern:        rule.Pattern,
		Weight:         rule.Weight,
		Severity:       rule.Severity,
		Enabled:        &enabled,
		Explanation:    rule.Explanation,
		Recommendation: rule.Recommendation,
	}
}

func TestCreateOrReplayPendingRevisionSubmissionCreatesNonExecutableProposal(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_create")
	beforeDigest, err := database.RiskRuleSnapshotDigest(target)
	if err != nil {
		t.Fatal(err)
	}

	submission, validation, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, revisionRequestFromRule(target))
	if err != nil {
		t.Fatal(err)
	}
	if !created || !validation.Valid {
		t.Fatalf("expected valid new revision proposal, created=%v validation=%+v", created, validation)
	}
	if submission.Kind != database.RuleSubmissionKindRevision || submission.TargetRiskRuleID == nil || *submission.TargetRiskRuleID != target.ID || submission.BaseVersion == nil || *submission.BaseVersion != 1 {
		t.Fatalf("stored revision intent mismatch: %+v", submission)
	}
	if submission.Code != target.Code {
		t.Fatalf("server must copy immutable target code, got %q want %q", submission.Code, target.Code)
	}
	if submission.DraftDigest == nil || submission.RequestDigest == nil {
		t.Fatal("revision proposal must persist both draft and request digests")
	}

	var after database.RiskRule
	if err := db.First(&after, target.ID).Error; err != nil {
		t.Fatal(err)
	}
	afterDigest, err := database.RiskRuleSnapshotDigest(after)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != 1 || afterDigest != beforeDigest {
		t.Fatalf("revision proposal mutated executable rule: before=%s after=%s version=%d", beforeDigest, afterDigest, after.Version)
	}
}

func TestCreateOrReplayPendingRevisionSubmissionExactReplayAndCompetingDrafts(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_replay")
	request := revisionRequestFromRule(target)

	first, _, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
	if err != nil || !created {
		t.Fatalf("first proposal failed: created=%v err=%v", created, err)
	}
	second, validation, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if created || !validation.Valid || second.ID != first.ID {
		t.Fatalf("exact replay must return existing pending proposal: first=%d second=%d created=%v validation=%+v", first.ID, second.ID, created, validation)
	}

	competing := request
	competing.Pattern = "different competing synthetic signal"
	third, validation, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, competing)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !validation.Valid || third.ID == first.ID {
		t.Fatalf("different draft on same target/base must coexist: third=%+v validation=%+v", third, validation)
	}
	var count int64
	if err := db.Model(&database.RuleSubmission{}).Where("status = ?", PendingSubmissionStatus).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected two competing pending revisions, got %d", count)
	}
}

func TestCreateOrReplayPendingRevisionSubmissionRejectsInvalidBaseAndIntegrity(t *testing.T) {
	t.Run("zero base", func(t *testing.T) {
		db := revisionSubmissionTestDB(t)
		target := createRevisionTarget(t, db, "revision_zero_base")
		request := revisionRequestFromRule(target)
		request.BaseVersion = 0
		_, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
		if !errors.Is(err, ErrInvalidRevisionSubmission) {
			t.Fatalf("expected invalid revision error, got %v", err)
		}
	})

	t.Run("missing target", func(t *testing.T) {
		db := revisionSubmissionTestDB(t)
		_, _, _, err := CreateOrReplayPendingRevisionSubmission(db, 999999, RevisionDraftRequest{BaseVersion: 1})
		if !errors.Is(err, ErrRevisionRuleNotFound) {
			t.Fatalf("expected rule not found, got %v", err)
		}
	})

	t.Run("stale base", func(t *testing.T) {
		db := revisionSubmissionTestDB(t)
		target := createRevisionTarget(t, db, "revision_stale")
		request := revisionRequestFromRule(target)
		if err := db.Model(&database.RiskRule{}).Where("id = ?", target.ID).UpdateColumn("version", 2).Error; err != nil {
			t.Fatal(err)
		}
		_, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
		if !errors.Is(err, ErrRevisionStaleBaseVersion) {
			t.Fatalf("expected stale base error, got %v", err)
		}
	})

	t.Run("missing history", func(t *testing.T) {
		db := revisionSubmissionTestDB(t)
		target := createRevisionTarget(t, db, "revision_missing_history")
		request := revisionRequestFromRule(target)
		if err := db.Where("risk_rule_id = ?", target.ID).Delete(&database.RiskRuleVersion{}).Error; err != nil {
			t.Fatal(err)
		}
		_, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
		if !errors.Is(err, ErrRevisionRuleVersionIntegrity) {
			t.Fatalf("expected version integrity error, got %v", err)
		}
	})

	t.Run("projection drift", func(t *testing.T) {
		db := revisionSubmissionTestDB(t)
		target := createRevisionTarget(t, db, "revision_projection_drift")
		request := revisionRequestFromRule(target)
		if err := db.Model(&database.RiskRule{}).Where("id = ?", target.ID).UpdateColumn("name", "out of band drift").Error; err != nil {
			t.Fatal(err)
		}
		_, _, _, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, request)
		if !errors.Is(err, ErrRevisionRuleVersionIntegrity) {
			t.Fatalf("expected version integrity error, got %v", err)
		}
	})
}

func TestCreateOrReplayPendingRevisionSubmissionRejectsNoOpAndOrdinaryValidation(t *testing.T) {
	db := revisionSubmissionTestDB(t)
	target := createRevisionTarget(t, db, "revision_validation")

	enabled := target.Enabled
	noOp := RevisionDraftRequest{
		BaseVersion:    target.Version,
		Name:           target.Name,
		Description:    target.Description,
		CategoryCode:   target.CategoryCode,
		RuleType:       target.RuleType,
		Pattern:        target.Pattern,
		Weight:         target.Weight,
		Severity:       target.Severity,
		Enabled:        &enabled,
		Explanation:    target.Explanation,
		Recommendation: target.Recommendation,
	}
	_, validation, created, err := CreateOrReplayPendingRevisionSubmission(db, target.ID, noOp)
	if err != nil {
		t.Fatal(err)
	}
	if created || validation.Valid || len(validation.Errors) == 0 || validation.Errors[0].Code != "no_changes" {
		t.Fatalf("no-op revision must be rejected: created=%v validation=%+v", created, validation)
	}

	invalid := revisionRequestFromRule(target)
	invalid.CategoryCode = "missing_category"
	_, validation, created, err = CreateOrReplayPendingRevisionSubmission(db, target.ID, invalid)
	if err != nil {
		t.Fatal(err)
	}
	if created || validation.Valid {
		t.Fatalf("ordinary invalid revision must not persist: created=%v validation=%+v", created, validation)
	}
	found := false
	for _, item := range validation.Errors {
		if item.Code == "category_not_found" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected category_not_found, got %+v", validation.Errors)
	}

	var count int64
	if err := db.Model(&database.RuleSubmission{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid revisions must create zero submissions, got %d", count)
	}
}
