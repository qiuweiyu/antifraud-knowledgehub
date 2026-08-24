package database

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestPrepareRiskRuleVersionHistoryCreatesIdempotentLegacyBaseline(t *testing.T) {
	db := ruleVersionHistoryTestDB(t)
	rule := RiskRule{
		Code: "legacy_baseline", Name: "Legacy baseline", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "legacy signal", Weight: 20, Severity: "medium", Enabled: true,
		Explanation: "Legacy explanation", Recommendation: "Verify independently.",
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareRiskRuleVersionHistory(db); err != nil {
		t.Fatal(err)
	}
	assertCurrentVersionHistory(t, db, rule.ID, 1, RiskRuleVersionSourceLegacyBaseline)
	if err := PrepareRiskRuleVersionHistory(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&RiskRuleVersion{}).Where("risk_rule_id = ?", rule.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("repeated preparation must remain idempotent, got %d versions", count)
	}
}

func TestPrepareRiskRuleVersionHistoryBackfillsControlledPublication(t *testing.T) {
	t.Run("unchanged current becomes controlled v1", func(t *testing.T) {
		db := ruleVersionHistoryTestDB(t)
		rule, event := createLegacyControlledPublicationFixture(t, db, "controlled_unchanged")
		if err := PrepareRiskRuleVersionHistory(db); err != nil {
			t.Fatal(err)
		}
		version := assertCurrentVersionHistory(t, db, rule.ID, 1, RiskRuleVersionSourceControlledPublication)
		if version.PublicationEventID == nil || *version.PublicationEventID != event.ID || version.SourceSubmissionID == nil || *version.SourceSubmissionID != event.SubmissionID {
			t.Fatalf("controlled v1 must retain source publication provenance: %+v", version)
		}
	})

	t.Run("pre-versioning drift becomes controlled v1 plus legacy v2", func(t *testing.T) {
		db := ruleVersionHistoryTestDB(t)
		rule, event := createLegacyControlledPublicationFixture(t, db, "controlled_drift")
		if err := db.Model(&RiskRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
			"name": "Changed before version history existed", "enabled": false,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := PrepareRiskRuleVersionHistory(db); err != nil {
			t.Fatal(err)
		}
		var versions []RiskRuleVersion
		if err := db.Where("risk_rule_id = ?", rule.ID).Order("version asc").Find(&versions).Error; err != nil {
			t.Fatal(err)
		}
		if len(versions) != 2 || versions[0].Version != 1 || versions[0].SourceKind != RiskRuleVersionSourceControlledPublication || versions[1].Version != 2 || versions[1].SourceKind != RiskRuleVersionSourceLegacyBaseline {
			t.Fatalf("expected controlled v1 + legacy v2, got %+v", versions)
		}
		if versions[0].PublicationEventID == nil || *versions[0].PublicationEventID != event.ID {
			t.Fatalf("v1 must link original event: %+v", versions[0])
		}
		var current RiskRule
		if err := db.First(&current, rule.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.Version != 2 || current.Name != "Changed before version history existed" || current.Enabled {
			t.Fatalf("current projection must preserve observed pre-versioning drift as v2: %+v", current)
		}
		if err := VerifyRiskRuleMatchesVersion(current, versions[1]); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPrepareRiskRuleVersionHistoryPreservesOrphanControlledHistoryWithoutRecreatingRule(t *testing.T) {
	db := ruleVersionHistoryTestDB(t)
	rule, event := createLegacyControlledPublicationFixture(t, db, "controlled_deleted")
	if err := db.Delete(&RiskRule{}, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareRiskRuleVersionHistory(db); err != nil {
		t.Fatal(err)
	}
	var currentCount int64
	if err := db.Model(&RiskRule{}).Where("id = ?", rule.ID).Count(&currentCount).Error; err != nil {
		t.Fatal(err)
	}
	if currentCount != 0 {
		t.Fatalf("preparation must not recreate deleted executable rule, got %d rows", currentCount)
	}
	var version RiskRuleVersion
	if err := db.Where("risk_rule_id = ? AND version = 1", rule.ID).First(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version.SourceKind != RiskRuleVersionSourceControlledPublication || version.PublicationEventID == nil || *version.PublicationEventID != event.ID {
		t.Fatalf("orphan historical publication must remain provable: %+v", version)
	}
}

func TestPrepareRiskRuleVersionHistoryFailsClosedOnProjectionHistoryMismatch(t *testing.T) {
	db := ruleVersionHistoryTestDB(t)
	rule := RiskRule{
		Code: "projection_mismatch", Name: "Projection baseline", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "projection signal", Weight: 20, Severity: "medium", Enabled: true,
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareRiskRuleVersionHistory(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RiskRule{}).Where("id = ?", rule.ID).UpdateColumn("name", "out-of-band same-version mutation").Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareRiskRuleVersionHistory(db); err == nil || !strings.Contains(err.Error(), "does not match history") {
		t.Fatalf("expected projection/history integrity failure, got %v", err)
	}
}

func TestPrepareRiskRuleVersionHistoryFailsClosedOnAmbiguousUnlinkedPublicationOrder(t *testing.T) {
	db := ruleVersionHistoryTestDB(t)
	rule, _ := createLegacyControlledPublicationFixture(t, db, "ambiguous_first")

	second := RuleSubmission{
		Status: approvedRuleSubmissionStatus, Code: "ambiguous_first", Name: "Second source",
		CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "second signal", Weight: 30, Severity: "high", Enabled: true,
	}
	digest, err := RuleSubmissionDraftDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	second.DraftDigest = &digest
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	review := RuleSubmissionReviewEvent{
		SubmissionID: second.ID, Decision: approvedRuleSubmissionStatus, FromStatus: pendingRuleSubmissionStatusForVersionIntegrity,
		ToStatus: approvedRuleSubmissionStatus, Reason: "ambiguous history fixture", ActorKind: controlledMaintainerActorKind,
		ActorLabel: "maintainer-b", DraftDigest: digest,
	}
	if err := db.Create(&review).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&RuleSubmissionPublicationEvent{
		SubmissionID: second.ID, ReviewEventID: review.ID, RiskRuleID: rule.ID, RiskRuleCode: second.Code,
		ActorKind: controlledPublicationActorKind, ActorLabel: "publisher-b", DraftDigest: digest,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareRiskRuleVersionHistory(db); err == nil || !strings.Contains(err.Error(), "pre-version ordering cannot be inferred") {
		t.Fatalf("expected ambiguous publication history to fail closed, got %v", err)
	}
}

func ruleVersionHistoryTestDB(t *testing.T) *gorm.DB {
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
	if err := db.AutoMigrate(&RiskRule{}, &RuleSubmission{}, &RuleSubmissionReviewEvent{}, &RuleSubmissionPublicationEvent{}, &RiskRuleVersion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func createLegacyControlledPublicationFixture(t *testing.T, db *gorm.DB, code string) (RiskRule, RuleSubmissionPublicationEvent) {
	t.Helper()
	submission := RuleSubmission{
		Status: approvedRuleSubmissionStatus, Code: code, Name: code,
		Description: "controlled historical fixture", CategoryCode: "fake_customer_service", RuleType: "keyword",
		Pattern: "controlled signal " + code, Weight: 30, Severity: "high", Enabled: true,
		Explanation: "Controlled fixture", Recommendation: "Verify independently.",
	}
	digest, err := RuleSubmissionDraftDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	submission.DraftDigest = &digest
	if err := db.Create(&submission).Error; err != nil {
		t.Fatal(err)
	}
	review := RuleSubmissionReviewEvent{
		SubmissionID: submission.ID, Decision: approvedRuleSubmissionStatus, FromStatus: pendingRuleSubmissionStatusForVersionIntegrity,
		ToStatus: approvedRuleSubmissionStatus, Reason: "approved historical fixture", ActorKind: controlledMaintainerActorKind,
		ActorLabel: "maintainer-a", DraftDigest: digest,
	}
	if err := db.Create(&review).Error; err != nil {
		t.Fatal(err)
	}
	sourceID := submission.ID
	rule := riskRuleFromSubmissionSnapshot(submission)
	rule.SourceSubmissionID = &sourceID
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	event := RuleSubmissionPublicationEvent{
		SubmissionID: submission.ID, ReviewEventID: review.ID, RiskRuleID: rule.ID, RiskRuleCode: rule.Code,
		ActorKind: controlledPublicationActorKind, ActorLabel: "publisher-a", DraftDigest: digest,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	return rule, event
}

func assertCurrentVersionHistory(t *testing.T, db *gorm.DB, riskRuleID, wantVersion uint, wantSource string) RiskRuleVersion {
	t.Helper()
	var current RiskRule
	if err := db.First(&current, riskRuleID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Version != wantVersion {
		t.Fatalf("unexpected current version: want=%d got=%d", wantVersion, current.Version)
	}
	var version RiskRuleVersion
	if err := db.Where("risk_rule_id = ? AND version = ?", riskRuleID, wantVersion).First(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version.SourceKind != wantSource {
		t.Fatalf("unexpected source kind: want=%q got=%q", wantSource, version.SourceKind)
	}
	if err := VerifyRiskRuleMatchesVersion(current, version); err != nil {
		t.Fatal(err)
	}
	return version
}