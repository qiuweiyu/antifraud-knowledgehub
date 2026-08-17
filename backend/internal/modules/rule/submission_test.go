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

	submission, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || !created {
		t.Fatalf("expected a newly created valid draft: created=%v errors=%+v", created, result.Errors)
	}
	if submission.ID == 0 {
		t.Fatal("expected persisted submission ID")
	}
	if submission.Status != PendingSubmissionStatus {
		t.Fatalf("expected status %q, got %q", PendingSubmissionStatus, submission.Status)
	}
	if submission.Code != "pending_remote_screen_share" || submission.Name != "Pending remote screen share" {
		t.Fatalf("expected normalized code/name, got %+v", submission)
	}
	if submission.CategoryCode != "fake_customer_service" || submission.RuleType != "keyword" || submission.Pattern != "remote screen share" || submission.Severity != "high" {
		t.Fatalf("expected normalized snapshot: %+v", submission)
	}
	if submission.Enabled {
		t.Fatal("expected explicit enabled=false to be preserved")
	}
	if submission.DraftDigest == nil || len(*submission.DraftDigest) != 64 {
		t.Fatalf("new pending submission must contain a server digest: %+v", submission.DraftDigest)
	}
	if got := countSubmissionRows(t, db); got != 1 {
		t.Fatalf("expected one submission, got %d", got)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("creating a submission must not create RiskRule rows, got %d", got)
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

	submission, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || created {
		t.Fatal("expected invalid draft with no create")
	}
	if submission.ID != 0 {
		t.Fatalf("invalid draft must not be persisted: %+v", submission)
	}
	if got := countSubmissionRows(t, db); got != 0 {
		t.Fatalf("invalid draft must produce zero writes, got %d", got)
	}
}

func TestExactReplayReturnsSamePendingSubmission(t *testing.T) {
	db := submissionTestDB(t)
	draft := DraftRequest{
		Code:         "exact_replay",
		Name:         "Exact Replay",
		CategoryCode: "fake_customer_service",
		RuleType:     "keyword",
		Pattern:      "synthetic replay signal",
		Weight:       20,
		Severity:     "medium",
	}

	first, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || !created {
		t.Fatalf("first create failed: created=%v result=%+v err=%v", created, result, err)
	}
	second, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || created {
		t.Fatalf("replay failed: created=%v result=%+v err=%v", created, result, err)
	}
	if first.ID != second.ID {
		t.Fatalf("exact replay must return the same submission ID: first=%d second=%d", first.ID, second.ID)
	}
	if got := countSubmissionRows(t, db); got != 1 {
		t.Fatalf("exact replay must not grow pending queue, got %d rows", got)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("exact replay must not affect RiskRule rows, got %d", got)
	}
}

func TestSameCodeDifferentDraftRemainsDistinctProposal(t *testing.T) {
	db := submissionTestDB(t)
	base := DraftRequest{
		Code:         "repeated_pending_code",
		Name:         "Repeated pending code",
		CategoryCode: "fake_customer_service",
		RuleType:     "keyword",
		Pattern:      "synthetic signal A",
		Weight:       20,
		Severity:     "medium",
	}

	first, result, created, err := CreateOrReplayPendingSubmission(db, base)
	if err != nil || !result.Valid || !created {
		t.Fatalf("first proposal failed: created=%v result=%+v err=%v", created, result, err)
	}
	secondDraft := base
	secondDraft.Pattern = "synthetic signal B"
	second, result, created, err := CreateOrReplayPendingSubmission(db, secondDraft)
	if err != nil || !result.Valid || !created {
		t.Fatalf("second proposal failed: created=%v result=%+v err=%v", created, result, err)
	}
	if first.ID == second.ID || first.DraftDigest == nil || second.DraftDigest == nil || *first.DraftDigest == *second.DraftDigest {
		t.Fatalf("different persisted content must remain distinct proposals: first=%+v second=%+v", first, second)
	}
	if got := countSubmissionRows(t, db); got != 2 {
		t.Fatalf("expected two same-code/different-content proposals, got %d", got)
	}
}

func TestExactReplayReturnsBeforeMutableRevalidation(t *testing.T) {
	db := submissionTestDB(t)
	draft := DraftRequest{
		Code:         "replay_after_rule_change",
		Name:         "Replay after rule change",
		CategoryCode: "fake_customer_service",
		RuleType:     "keyword",
		Pattern:      "synthetic replay dependency",
		Weight:       30,
		Severity:     "high",
	}
	first, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || !created {
		t.Fatalf("initial create failed: created=%v result=%+v err=%v", created, result, err)
	}

	if err := db.Create(&database.RiskRule{
		Code: draft.Code, Name: "Now conflicts", CategoryCode: "fake_customer_service", RuleType: "keyword",
		Pattern: "production rule", Weight: 50, Severity: "high", Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}

	replay, result, created, err := CreateOrReplayPendingSubmission(db, draft)
	if err != nil || !result.Valid || created {
		t.Fatalf("exact replay should bypass mutable revalidation: created=%v result=%+v err=%v", created, result, err)
	}
	if replay.ID != first.ID {
		t.Fatalf("expected original submission %d, got %d", first.ID, replay.ID)
	}
	if got := countSubmissionRows(t, db); got != 1 {
		t.Fatalf("replay after validator dependency change must not write, got %d rows", got)
	}
	if got := countRiskRuleRows(t, db); got != 1 {
		t.Fatalf("submission replay must not mutate existing RiskRule state, got %d rows", got)
	}
}

func TestDraftDigestUsesPersistedNormalization(t *testing.T) {
	truth := true
	falsehood := false
	base := DraftRequest{
		Code: " digest_code ", Name: " Digest Name ", Description: "free text",
		CategoryCode: " fake_customer_service ", RuleType: " keyword ", Pattern: " signal ",
		Weight: 20, Severity: " medium ", Explanation: "explain", Recommendation: "recommend",
	}

	digest := digestForDraft(t, base)
	explicitTrue := base
	explicitTrue.Enabled = &truth
	if got := digestForDraft(t, explicitTrue); got != digest {
		t.Fatalf("omitted enabled and explicit true must be exact replays: %s != %s", digest, got)
	}
	trimmed := base
	trimmed.Code = "digest_code"
	trimmed.Name = "Digest Name"
	trimmed.CategoryCode = "fake_customer_service"
	trimmed.RuleType = "keyword"
	trimmed.Pattern = "signal"
	trimmed.Severity = "medium"
	if got := digestForDraft(t, trimmed); got != digest {
		t.Fatalf("transport whitespace on trimmed fields must not change digest: %s != %s", digest, got)
	}
	disabled := base
	disabled.Enabled = &falsehood
	if got := digestForDraft(t, disabled); got == digest {
		t.Fatal("enabled=false must change persisted snapshot digest")
	}
	freeTextChanged := base
	freeTextChanged.Description += " "
	if got := digestForDraft(t, freeTextChanged); got == digest {
		t.Fatal("persisted free-text whitespace must change v1 digest")
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
	if len(items) != 2 || items[0].ID != first.ID || items[1].ID != second.ID {
		t.Fatalf("expected two deterministic oldest-first pending submissions, got %+v", items)
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

func digestForDraft(t *testing.T, draft DraftRequest) string {
	t.Helper()
	digest, err := database.RuleSubmissionDraftDigest(pendingSubmissionSnapshot(draft))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func createPendingSubmissionForTest(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	submission, result, created, err := CreateOrReplayPendingSubmission(db, DraftRequest{
		Code: code, Name: code, CategoryCode: "fake_customer_service", RuleType: "keyword",
		Pattern: "synthetic inspection signal " + code, Weight: 20, Severity: "medium",
	})
	if err != nil || !result.Valid || !created {
		t.Fatalf("expected valid newly created test draft: created=%v errors=%+v err=%v", created, result.Errors, err)
	}
	return submission
}

func createNonPendingSubmissionForTest(t *testing.T, db *gorm.DB, code string) database.RuleSubmission {
	t.Helper()
	item := database.RuleSubmission{
		Status: "reviewed_test_fixture", Code: code, Name: code, CategoryCode: "fake_customer_service",
		RuleType: "keyword", Pattern: "synthetic hidden signal", Weight: 20, Severity: "medium", Enabled: true,
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
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Category{
		Code: "fake_customer_service", Name: "Synthetic customer service fraud", SeverityDefault: "high",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}
