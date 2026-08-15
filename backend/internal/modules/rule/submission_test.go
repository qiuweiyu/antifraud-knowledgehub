package rule

import (
	"errors"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCreatePendingSubmissionPersistsValidatedSnapshot(t *testing.T) {
	db := submissionTestDB(t)
	disabled := false
	draft := DraftRequest{
		Code:           "  pending_remote_screen_share  ",
		Name:           "  Pending remote screen share  ",
		Description:    "Synthetic pending rule",
		CategoryCode:   "  fake_customer_service  ",
		RuleType:       "  keyword  ",
		Pattern:        "  remote screen share  ",
		Weight:         24,
		Severity:       "  high  ",
		Enabled:        &disabled,
		Explanation:    "Synthetic explanation",
		Recommendation: "Use an official channel to verify the request.",
	}

	submission, result, err := CreatePendingSubmission(db, draft)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid draft: %+v", result.Errors)
	}
	if submission.ID == 0 {
		t.Fatal("expected persisted submission ID")
	}
	if submission.Status != PendingSubmissionStatus {
		t.Fatalf("expected status %q, got %q", PendingSubmissionStatus, submission.Status)
	}
	if submission.Code != "pending_remote_screen_share" {
		t.Fatalf("expected normalized code, got %q", submission.Code)
	}
	if submission.CategoryCode != "fake_customer_service" {
		t.Fatalf("expected normalized category, got %q", submission.CategoryCode)
	}
	if submission.RuleType != "keyword" || submission.Pattern != "remote screen share" || submission.Severity != "high" {
		t.Fatalf("expected normalized snapshot: %+v", submission)
	}
	if submission.Enabled {
		t.Fatal("expected explicit enabled=false to be preserved")
	}

	var submissionCount int64
	if err := db.Model(&database.RuleSubmission{}).Count(&submissionCount).Error; err != nil {
		t.Fatal(err)
	}
	if submissionCount != 1 {
		t.Fatalf("expected one submission, got %d", submissionCount)
	}

	var ruleCount int64
	if err := db.Model(&database.RiskRule{}).Count(&ruleCount).Error; err != nil {
		t.Fatal(err)
	}
	if ruleCount != 0 {
		t.Fatalf("creating a submission must not create RiskRule rows, got %d", ruleCount)
	}
}

func TestCreatePendingSubmissionInvalidDraftDoesNotWrite(t *testing.T) {
	db := submissionTestDB(t)
	draft := DraftRequest{
		Code:         "invalid_submission",
		Name:         "Invalid submission",
		CategoryCode: "missing_category",
		RuleType:     "keyword",
		Pattern:      "synthetic signal",
		Weight:       20,
		Severity:     "medium",
	}

	submission, result, err := CreatePendingSubmission(db, draft)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected invalid draft")
	}
	if submission.ID != 0 {
		t.Fatalf("invalid draft must not be persisted: %+v", submission)
	}

	var count int64
	if err := db.Model(&database.RuleSubmission{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid draft must produce zero writes, got %d", count)
	}
}

func TestPendingSubmissionCodeIsNotUnique(t *testing.T) {
	db := submissionTestDB(t)
	draft := DraftRequest{
		Code:         "repeated_pending_code",
		Name:         "Repeated pending code",
		CategoryCode: "fake_customer_service",
		RuleType:     "keyword",
		Pattern:      "synthetic signal",
		Weight:       20,
		Severity:     "medium",
	}

	for i := 0; i < 2; i++ {
		_, result, err := CreatePendingSubmission(db, draft)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Valid {
			t.Fatalf("expected pending duplicate to remain structurally valid: %+v", result.Errors)
		}
	}

	var count int64
	if err := db.Model(&database.RuleSubmission{}).Where("code = ?", draft.Code).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected two pending submissions with same code, got %d", count)
	}
}

func TestListPendingSubmissionsFiltersOrdersAndDoesNotWrite(t *testing.T) {
	db := submissionTestDB(t)
	first := createPendingSubmissionForTest(t, db, "inspect_first")
	second := createPendingSubmissionForTest(t, db, "inspect_second")
	createNonPendingSubmissionForTest(t, db, "hidden_non_pending")

	beforeSubmissions := countSubmissionRows(t, db)
	beforeRules := countRiskRuleRows(t, db)

	items, err := ListPendingSubmissions(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two pending submissions, got %d: %+v", len(items), items)
	}
	if items[0].ID != first.ID || items[1].ID != second.ID {
		t.Fatalf("expected deterministic oldest-first ordering, got IDs %d, %d", items[0].ID, items[1].ID)
	}
	for _, item := range items {
		if item.Status != PendingSubmissionStatus {
			t.Fatalf("list must contain only pending submissions: %+v", item)
		}
	}

	if got := countSubmissionRows(t, db); got != beforeSubmissions {
		t.Fatalf("list must not modify submissions: before=%d after=%d", beforeSubmissions, got)
	}
	if got := countRiskRuleRows(t, db); got != beforeRules {
		t.Fatalf("list must not modify RiskRule rows: before=%d after=%d", beforeRules, got)
	}
}

func TestGetPendingSubmissionFiltersStatusAndDoesNotWrite(t *testing.T) {
	db := submissionTestDB(t)
	pending := createPendingSubmissionForTest(t, db, "inspect_one")
	nonPending := createNonPendingSubmissionForTest(t, db, "hidden_one")

	beforeSubmissions := countSubmissionRows(t, db)
	beforeRules := countRiskRuleRows(t, db)

	got, err := GetPendingSubmission(db, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != pending.ID || got.Status != PendingSubmissionStatus {
		t.Fatalf("unexpected pending submission: %+v", got)
	}

	if _, err := GetPendingSubmission(db, nonPending.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected non-pending submission to be hidden, got %v", err)
	}
	if _, err := GetPendingSubmission(db, 999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected missing submission to return record not found, got %v", err)
	}

	if got := countSubmissionRows(t, db); got != beforeSubmissions {
		t.Fatalf("get must not modify submissions: before=%d after=%d", beforeSubmissions, got)
	}
	if got := countRiskRuleRows(t, db); got != beforeRules {
		t.Fatalf("get must not modify RiskRule rows: before=%d after=%d", beforeRules, got)
	}
}

func createPendingSubmissionForTest(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	submission, result, err := CreatePendingSubmission(db, DraftRequest{
		Code:         code,
		Name:         code,
		CategoryCode: "fake_customer_service",
		RuleType:     "keyword",
		Pattern:      "synthetic inspection signal",
		Weight:       20,
		Severity:     "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid test draft: %+v", result.Errors)
	}
	return submission
}

func createNonPendingSubmissionForTest(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	item := database.RuleSubmission{
		Status:       "reviewed_test_fixture",
		Code:         code,
		Name:         code,
		CategoryCode: "fake_customer_service",
		RuleType:     "keyword",
		Pattern:      "synthetic hidden signal",
		Weight:       20,
		Severity:     "medium",
		Enabled:      true,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func countSubmissionRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&database.RuleSubmission{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func countRiskRuleRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&database.RiskRule{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func submissionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.RuleSubmission{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Category{
		Code:            "fake_customer_service",
		Name:            "Synthetic customer service fraud",
		SeverityDefault: "high",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}
