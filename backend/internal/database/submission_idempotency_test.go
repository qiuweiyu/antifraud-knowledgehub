package database

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type legacyRuleSubmission struct {
	ID             uint      `gorm:"primaryKey"`
	Status         string    `gorm:"index;size:32;not null"`
	Code           string    `gorm:"index;size:120;not null"`
	Name           string    `gorm:"size:180;not null"`
	Description    string
	CategoryCode   string    `gorm:"index;size:100;not null"`
	RuleType       string    `gorm:"size:40;not null"`
	Pattern        string    `gorm:"not null"`
	Weight         int       `gorm:"not null"`
	Severity       string    `gorm:"size:40;not null"`
	Enabled        bool      `gorm:"not null"`
	Explanation    string
	Recommendation string
	CreatedAt      time.Time `gorm:"index"`
	UpdatedAt      time.Time
}

func (legacyRuleSubmission) TableName() string { return "rule_submissions" }

func digestFixtureSubmission() RuleSubmission {
	return RuleSubmission{
		Kind:           RuleSubmissionKindCreate,
		Code:           "fixture_code",
		Name:           "Fixture Name",
		Description:    "Fixture description",
		CategoryCode:   "fake_customer_service",
		RuleType:       "keyword",
		Pattern:        "fixture pattern",
		Weight:         42,
		Severity:       "high",
		Enabled:        true,
		Explanation:    "Fixture explanation",
		Recommendation: "Fixture recommendation",
	}
}

func TestRuleSubmissionDraftDigestKnownV1Fixture(t *testing.T) {
	digest, err := RuleSubmissionDraftDigest(digestFixtureSubmission())
	if err != nil {
		t.Fatal(err)
	}
	const want = "ab764f51455241d4c23740df240e3829cc1bb68d1fc0134122a3c610fc7fe714"
	if digest != want {
		t.Fatalf("digest drifted: got %s want %s", digest, want)
	}
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		t.Fatalf("digest must be 64 lowercase hex characters: %q", digest)
	}
}

func TestRuleSubmissionRequestDigestKnownV1CreateFixture(t *testing.T) {
	submission := digestFixtureSubmission()
	draftDigest, err := RuleSubmissionDraftDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	submission.DraftDigest = stringPointer(draftDigest)
	requestDigest, err := RuleSubmissionRequestDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	const want = "6e6919ae69828bd8e9b4dbfc21ccf0e5ed25aca2eb48b2bb04f1822e548594d9"
	if requestDigest != want {
		t.Fatalf("request digest drifted: got %s want %s", requestDigest, want)
	}
}

func TestRuleSubmissionDraftDigestChangesForEveryPersistedDraftField(t *testing.T) {
	base := digestFixtureSubmission()
	baseDigest, err := RuleSubmissionDraftDigest(base)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(*RuleSubmission){
		"code":           func(v *RuleSubmission) { v.Code += "_changed" },
		"name":           func(v *RuleSubmission) { v.Name += " changed" },
		"description":    func(v *RuleSubmission) { v.Description += " changed" },
		"category_code":  func(v *RuleSubmission) { v.CategoryCode += "_changed" },
		"rule_type":      func(v *RuleSubmission) { v.RuleType = "pattern" },
		"pattern":        func(v *RuleSubmission) { v.Pattern += " changed" },
		"weight":         func(v *RuleSubmission) { v.Weight++ },
		"severity":       func(v *RuleSubmission) { v.Severity = "critical" },
		"enabled":        func(v *RuleSubmission) { v.Enabled = false },
		"explanation":    func(v *RuleSubmission) { v.Explanation += " changed" },
		"recommendation": func(v *RuleSubmission) { v.Recommendation += " changed" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			got, err := RuleSubmissionDraftDigest(changed)
			if err != nil {
				t.Fatal(err)
			}
			if got == baseDigest {
				t.Fatalf("persisted draft field %s changed without changing digest", name)
			}
		})
	}
}

func TestRuleSubmissionDraftDigestExcludesIntentAndSystemOwnedFields(t *testing.T) {
	base := digestFixtureSubmission()
	baseDigest, err := RuleSubmissionDraftDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	arbitraryDigest := strings.Repeat("f", 64)
	targetID := uint(77)
	baseVersion := uint(3)
	changed := base
	changed.ID = 99
	changed.Status = "reviewed_test_fixture"
	changed.Kind = RuleSubmissionKindRevision
	changed.TargetRiskRuleID = &targetID
	changed.BaseVersion = &baseVersion
	changed.DraftDigest = &arbitraryDigest
	changed.RequestDigest = &arbitraryDigest
	changed.CreatedAt = time.Now().UTC()
	changed.UpdatedAt = changed.CreatedAt.Add(time.Hour)
	got, err := RuleSubmissionDraftDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if got != baseDigest {
		t.Fatalf("intent/system fields must not affect draft digest: got %s want %s", got, baseDigest)
	}
}

func TestRuleSubmissionRequestDigestSeparatesIntentTargetBaseAndDraft(t *testing.T) {
	base := digestFixtureSubmission()
	createDigest, err := RuleSubmissionRequestDigest(base)
	if err != nil {
		t.Fatal(err)
	}

	target1, target2 := uint(10), uint(11)
	version1, version2 := uint(1), uint(2)
	revision := base
	revision.Kind = RuleSubmissionKindRevision
	revision.TargetRiskRuleID = &target1
	revision.BaseVersion = &version1
	revisionDigest, err := RuleSubmissionRequestDigest(revision)
	if err != nil {
		t.Fatal(err)
	}
	if revisionDigest == createDigest {
		t.Fatal("create and revision intent must not share request digest")
	}

	otherTarget := revision
	otherTarget.TargetRiskRuleID = &target2
	otherTargetDigest, err := RuleSubmissionRequestDigest(otherTarget)
	if err != nil {
		t.Fatal(err)
	}
	if otherTargetDigest == revisionDigest {
		t.Fatal("different revision target must change request digest")
	}

	otherBase := revision
	otherBase.BaseVersion = &version2
	otherBaseDigest, err := RuleSubmissionRequestDigest(otherBase)
	if err != nil {
		t.Fatal(err)
	}
	if otherBaseDigest == revisionDigest {
		t.Fatal("different revision base must change request digest")
	}

	otherDraft := revision
	otherDraft.Pattern += " changed"
	otherDraftDigest, err := RuleSubmissionRequestDigest(otherDraft)
	if err != nil {
		t.Fatal(err)
	}
	if otherDraftDigest == revisionDigest {
		t.Fatal("different revision draft must change request digest")
	}
}

func TestPrepareRuleSubmissionIdempotencyBackfillsLegacyDuplicatesSafely(t *testing.T) {
	db := legacySubmissionDB(t)
	baseTime := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)

	first := legacyRuleSubmission{
		Status: "pending", Code: "legacy_same", Name: "Legacy Same", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "synthetic signal", Weight: 20, Severity: "medium", Enabled: true,
		Description: "preserve me", Explanation: "legacy explanation", Recommendation: "legacy recommendation",
		CreatedAt: baseTime,
	}
	second := first
	second.ID = 0
	second.CreatedAt = baseTime.Add(time.Second)
	third := first
	third.ID = 0
	third.Pattern = "different persisted signal"
	third.CreatedAt = baseTime.Add(2 * time.Second)

	for _, item := range []*legacyRuleSubmission{&first, &second, &third} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatal(err)
	}

	var rows []RuleSubmission
	if err := db.Order("created_at asc").Order("id asc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("legacy upgrade must not delete rows: got %d", len(rows))
	}
	if rows[0].Description != "preserve me" || rows[0].Pattern != "synthetic signal" || rows[1].Pattern != "synthetic signal" {
		t.Fatalf("legacy draft content was unexpectedly rewritten: %+v", rows)
	}
	for i, row := range rows {
		if row.Kind != RuleSubmissionKindCreate || row.TargetRiskRuleID != nil || row.BaseVersion != nil {
			t.Fatalf("legacy row %d must be honestly backfilled as create: %+v", i, row)
		}
		if row.DraftDigest == nil {
			t.Fatalf("legacy row %d must receive exact content draft digest", i)
		}
	}
	if *rows[0].DraftDigest != *rows[1].DraftDigest {
		t.Fatal("exact duplicate legacy content must retain equal draft digests")
	}
	if rows[0].RequestDigest == nil {
		t.Fatal("earliest exact legacy pending request must become guarded representative")
	}
	if rows[1].RequestDigest != nil {
		t.Fatalf("later exact legacy pending duplicate must remain request-digest nullable, got %q", *rows[1].RequestDigest)
	}
	if rows[2].RequestDigest == nil || *rows[2].RequestDigest == *rows[0].RequestDigest {
		t.Fatal("distinct legacy pending request must receive its own request digest")
	}
	if db.Migrator().HasIndex(&RuleSubmission{}, RuleSubmissionPendingDigestIndex) {
		t.Fatalf("legacy pending draft digest index %s must be removed", RuleSubmissionPendingDigestIndex)
	}
	if !db.Migrator().HasIndex(&RuleSubmission{}, RuleSubmissionPendingRequestDigestIndex) {
		t.Fatalf("expected partial unique index %s", RuleSubmissionPendingRequestDigestIndex)
	}

	duplicate := rows[0]
	duplicate.ID = 0
	duplicate.CreatedAt = time.Time{}
	duplicate.UpdatedAt = time.Time{}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("partial unique request index must reject a second non-null pending request digest")
	}

	nonPending := rows[0]
	nonPending.ID = 0
	nonPending.Status = "reviewed_test_fixture"
	nonPending.CreatedAt = time.Time{}
	nonPending.UpdatedAt = time.Time{}
	if err := db.Create(&nonPending).Error; err != nil {
		t.Fatalf("pending-scoped request uniqueness must not block non-pending history: %v", err)
	}
}

func TestRuleSubmissionDatabaseIntentShapeConstraint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&RuleSubmission{}); err != nil {
		t.Fatal(err)
	}

	targetID := uint(77)
	baseVersion := uint(3)
	invalid := map[string]func(*RuleSubmission){
		"create_with_target": func(row *RuleSubmission) {
			row.Kind = RuleSubmissionKindCreate
			row.TargetRiskRuleID = &targetID
		},
		"create_with_base": func(row *RuleSubmission) {
			row.Kind = RuleSubmissionKindCreate
			row.BaseVersion = &baseVersion
		},
		"revision_without_target": func(row *RuleSubmission) {
			row.Kind = RuleSubmissionKindRevision
			row.BaseVersion = &baseVersion
		},
		"revision_without_base": func(row *RuleSubmission) {
			row.Kind = RuleSubmissionKindRevision
			row.TargetRiskRuleID = &targetID
		},
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			row := digestFixtureSubmission()
			row.Status = "pending"
			mutate(&row)
			if err := db.Create(&row).Error; err == nil {
				t.Fatalf("database must reject invalid submission intent shape: %+v", row)
			}
		})
	}

	validCreate := digestFixtureSubmission()
	validCreate.Status = "pending"
	validCreate.Code = "valid_create_shape"
	if err := db.Create(&validCreate).Error; err != nil {
		t.Fatalf("database rejected valid create intent: %v", err)
	}

	validRevision := digestFixtureSubmission()
	validRevision.Status = "pending"
	validRevision.Kind = RuleSubmissionKindRevision
	validRevision.TargetRiskRuleID = &targetID
	validRevision.BaseVersion = &baseVersion
	validRevision.Code = "valid_revision_shape"
	if err := db.Create(&validRevision).Error; err != nil {
		t.Fatalf("database rejected valid revision intent: %v", err)
	}
}

func TestPrepareRuleSubmissionIdempotencyIsRestartSafe(t *testing.T) {
	db := legacySubmissionDB(t)
	item := legacyRuleSubmission{
		Status: "pending", Code: "restart_safe", Name: "Restart Safe", CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "restart signal", Weight: 10, Severity: "low", Enabled: true,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatal(err)
	}
	if err := PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatalf("schema preparation must be idempotent across restarts: %v", err)
	}
}

func legacySubmissionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyRuleSubmission{}); err != nil {
		t.Fatal(err)
	}
	return db
}
