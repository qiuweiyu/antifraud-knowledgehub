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

func TestRuleSubmissionDraftDigestExcludesSystemOwnedFields(t *testing.T) {
	base := digestFixtureSubmission()
	baseDigest, err := RuleSubmissionDraftDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	arbitraryDigest := strings.Repeat("f", 64)
	changed := base
	changed.ID = 99
	changed.Status = "reviewed_test_fixture"
	changed.DraftDigest = &arbitraryDigest
	changed.CreatedAt = time.Now().UTC()
	changed.UpdatedAt = changed.CreatedAt.Add(time.Hour)
	got, err := RuleSubmissionDraftDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if got != baseDigest {
		t.Fatalf("system-owned fields must not affect replay identity: got %s want %s", got, baseDigest)
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
	if rows[0].DraftDigest == nil {
		t.Fatal("earliest exact legacy duplicate must become the guarded representative")
	}
	if rows[1].DraftDigest != nil {
		t.Fatalf("later exact legacy duplicate must remain nullable, got %q", *rows[1].DraftDigest)
	}
	if rows[2].DraftDigest == nil || *rows[2].DraftDigest == *rows[0].DraftDigest {
		t.Fatal("distinct legacy draft must receive its own digest")
	}
	if !db.Migrator().HasIndex(&RuleSubmission{}, RuleSubmissionPendingDigestIndex) {
		t.Fatalf("expected partial unique index %s", RuleSubmissionPendingDigestIndex)
	}

	duplicate := rows[0]
	duplicate.ID = 0
	duplicate.CreatedAt = time.Time{}
	duplicate.UpdatedAt = time.Time{}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("partial unique index must reject a second non-null pending digest")
	}

	nonPending := rows[0]
	nonPending.ID = 0
	nonPending.Status = "reviewed_test_fixture"
	nonPending.CreatedAt = time.Time{}
	nonPending.UpdatedAt = time.Time{}
	if err := db.Create(&nonPending).Error; err != nil {
		t.Fatalf("pending-scoped uniqueness must not block future non-pending history: %v", err)
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
