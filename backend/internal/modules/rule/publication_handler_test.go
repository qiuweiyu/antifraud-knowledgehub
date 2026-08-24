package rule

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const publicationHandlerActor = "publisher-console"

func performPublicationHandlerRequest(db *gorm.DB, id, contentType, body, actorLabel string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/rule-submissions/:id/publications", SubmissionPublicationHandler(db, actorLabel))
	req := httptest.NewRequest(http.MethodPost, "/rule-submissions/"+id+"/publications", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func publicationErrorCode(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope response.Envelope
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Success || envelope.Error == nil {
		t.Fatalf("expected error envelope, got %+v", envelope)
	}
	return envelope.Error.Code
}

func assertPublicationReviewUnchanged(t *testing.T, db *gorm.DB, before database.RuleSubmissionReviewEvent) {
	t.Helper()
	after := mustGetReviewEvent(t, db, before.SubmissionID)
	if after.ID != before.ID || after.Decision != before.Decision || after.FromStatus != before.FromStatus ||
		after.ToStatus != before.ToStatus || after.Reason != before.Reason || after.ActorKind != before.ActorKind ||
		after.ActorLabel != before.ActorLabel || after.DraftDigest != before.DraftDigest || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("publication transport must not mutate approved review event: before=%+v after=%+v", before, after)
	}
}

func TestSubmissionPublicationHandlerRejectsTransportInputWithoutWrites(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "missing content type", id: "1", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "wrong content type", id: "1", contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "malformed content type", id: "1", contentType: "application/json; charset==utf-8", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "oversized body", id: "1", contentType: "application/json", body: strings.Repeat(" ", int(MaxSubmissionPublicationRequestBodyBytes)+1) + `{}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "publication_body_too_large"},
		{name: "empty body", id: "1", contentType: "application/json", body: ``, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "malformed json", id: "1", contentType: "application/json", body: `{`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "unknown actor field", id: "1", contentType: "application/json", body: `{"actor_label":"client"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "unknown rule field", id: "1", contentType: "application/json", body: `{"enabled":true}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "unknown force field", id: "1", contentType: "application/json", body: `{"force":true}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "trailing value", id: "1", contentType: "application/json", body: `{} {}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "null", id: "1", contentType: "application/json", body: `null`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "array", id: "1", contentType: "application/json", body: `[]`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "scalar", id: "1", contentType: "application/json", body: `"publish"`, wantStatus: http.StatusBadRequest, wantCode: "invalid_publication_json"},
		{name: "zero id", id: "0", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_submission_id"},
		{name: "negative id", id: "-1", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_submission_id"},
		{name: "plus id", id: "+1", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_submission_id"},
		{name: "whitespace id", id: "%201", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_submission_id"},
		{name: "hex id", id: "0x1", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_submission_id"},
		{name: "overflow id", id: strings.Repeat("9", 40), contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_submission_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := publicationTestDB(t)
			submission := createApprovedPublicationSubmission(t, db, reviewDraft("publication_transport_"+strings.ReplaceAll(tt.name, " ", "_")))
			reviewBefore := mustGetReviewEvent(t, db, submission.ID)
			id := tt.id
			if id == "1" {
				id = uintString(submission.ID)
			}
			resp := performPublicationHandlerRequest(db, id, tt.contentType, tt.body, publicationHandlerActor)
			if resp.Code != tt.wantStatus {
				t.Fatalf("want status %d got %d: %s", tt.wantStatus, resp.Code, resp.Body.String())
			}
			if got := publicationErrorCode(t, resp); got != tt.wantCode {
				t.Fatalf("want error code %q got %q", tt.wantCode, got)
			}
			assertPublicationCounts(t, db, 0, 0)
			assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
			assertPublicationReviewUnchanged(t, db, reviewBefore)
		})
	}
}

func TestSubmissionPublicationHandlerMapsServiceFailuresWithoutPartialPublication(t *testing.T) {
	t.Run("missing submission", func(t *testing.T) {
		db := publicationTestDB(t)
		resp := performPublicationHandlerRequest(db, "999999", "application/json", `{}`, publicationHandlerActor)
		if resp.Code != http.StatusNotFound || publicationErrorCode(t, resp) != "submission_not_found" {
			t.Fatalf("unexpected missing response: %d %s", resp.Code, resp.Body.String())
		}
		assertPublicationCounts(t, db, 0, 0)
	})

	t.Run("pending submission", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createReviewPendingSubmission(t, db, "publication_handler_pending")
		resp := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, publicationHandlerActor)
		if resp.Code != http.StatusConflict || publicationErrorCode(t, resp) != "submission_not_publishable" {
			t.Fatalf("unexpected pending response: %d %s", resp.Code, resp.Body.String())
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, PendingSubmissionStatus)
	})

	t.Run("rejected submission", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createReviewPendingSubmission(t, db, "publication_handler_rejected")
		if _, err := ReviewPendingSubmission(db, submission.ID, SubmissionReviewCommand{Decision: RejectedSubmissionStatus, Reason: "synthetic rejection", ActorLabel: "maintainer-a"}); err != nil {
			t.Fatal(err)
		}
		reviewBefore := mustGetReviewEvent(t, db, submission.ID)
		resp := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, publicationHandlerActor)
		if resp.Code != http.StatusConflict || publicationErrorCode(t, resp) != "submission_not_publishable" {
			t.Fatalf("unexpected rejected response: %d %s", resp.Code, resp.Body.String())
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, RejectedSubmissionStatus)
		assertPublicationReviewUnchanged(t, db, reviewBefore)
	})

	t.Run("current validation failure", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publication_handler_validation"))
		reviewBefore := mustGetReviewEvent(t, db, submission.ID)
		if err := db.Where("code = ?", submission.CategoryCode).Delete(&database.Category{}).Error; err != nil {
			t.Fatal(err)
		}
		resp := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, publicationHandlerActor)
		if resp.Code != http.StatusConflict || publicationErrorCode(t, resp) != "submission_publication_validation_failed" {
			t.Fatalf("unexpected validation response: %d %s", resp.Code, resp.Body.String())
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
		assertPublicationReviewUnchanged(t, db, reviewBefore)
	})

	t.Run("integrity failure", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publication_handler_integrity"))
		if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submission.ID).UpdateColumn("draft_digest", strings.Repeat("0", 64)).Error; err != nil {
			t.Fatal(err)
		}
		resp := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, publicationHandlerActor)
		if resp.Code != http.StatusInternalServerError || publicationErrorCode(t, resp) != "submission_publication_integrity_error" {
			t.Fatalf("unexpected integrity response: %d %s", resp.Code, resp.Body.String())
		}
		assertPublicationCounts(t, db, 0, 0)
		assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
	})

	t.Run("invalid server actor is internal failure", func(t *testing.T) {
		db := publicationTestDB(t)
		submission := createApprovedPublicationSubmission(t, db, reviewDraft("publication_handler_actor"))
		reviewBefore := mustGetReviewEvent(t, db, submission.ID)
		resp := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, "   ")
		if resp.Code != http.StatusInternalServerError || publicationErrorCode(t, resp) != "submission_publication_failed" {
			t.Fatalf("unexpected invalid-actor response: %d %s", resp.Code, resp.Body.String())
		}
		assertPublicationCounts(t, db, 0, 0)
		assertPublicationReviewUnchanged(t, db, reviewBefore)
	})
}

func TestSubmissionPublicationHandlerCreatesReplaysAndNeverRecreatesHardDeletedRule(t *testing.T) {
	db := publicationTestDB(t)
	enabled := false
	draft := reviewDraft("publication_handler_success")
	draft.Enabled = &enabled
	draft.Pattern = "synthetic secret pattern for response boundary"
	draft.Explanation = "synthetic hidden explanation"
	submission := createApprovedPublicationSubmission(t, db, draft)
	reviewBefore := mustGetReviewEvent(t, db, submission.ID)

	first := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json; charset=utf-8", `{}`, "  "+publicationHandlerActor+"  ")
	if first.Code != http.StatusCreated {
		t.Fatalf("first publication must return 201, got %d: %s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Success bool                          `json:"success"`
		Data    submissionPublicationResponse `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	if !firstEnvelope.Success || firstEnvelope.Data.Replay || firstEnvelope.Data.SubmissionID != submission.ID ||
		firstEnvelope.Data.Status != ApprovedSubmissionStatus || firstEnvelope.Data.ActorKind != ControlledPublisherActorKind ||
		firstEnvelope.Data.ActorLabel != publicationHandlerActor || firstEnvelope.Data.PublicationEventID == 0 || firstEnvelope.Data.RiskRuleID == 0 ||
		firstEnvelope.Data.RiskRuleCode != submission.Code || firstEnvelope.Data.RiskRuleVersion != 1 ||
		firstEnvelope.Data.ReviewEventID != reviewBefore.ID || firstEnvelope.Data.CreatedAt.IsZero() {
		t.Fatalf("unexpected first publication response: %+v", firstEnvelope)
	}
	if strings.Contains(first.Body.String(), draft.Pattern) || strings.Contains(first.Body.String(), draft.Explanation) ||
		strings.Contains(first.Body.String(), reviewBefore.Reason) || strings.Contains(first.Body.String(), reviewBefore.DraftDigest) {
		t.Fatalf("publication success response exposed hidden snapshot/review/digest material: %s", first.Body.String())
	}
	assertPublicationCounts(t, db, 1, 1)
	assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
	assertPublicationReviewUnchanged(t, db, reviewBefore)

	var storedRule database.RiskRule
	if err := db.First(&storedRule, firstEnvelope.Data.RiskRuleID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRule.Enabled || storedRule.Version != 1 || storedRule.SourceSubmissionID == nil || *storedRule.SourceSubmissionID != submission.ID {
		t.Fatalf("published rule did not preserve approved disabled snapshot/version/provenance: %+v", storedRule)
	}

	replay := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, publicationHandlerActor)
	if replay.Code != http.StatusOK {
		t.Fatalf("exact replay must return 200, got %d: %s", replay.Code, replay.Body.String())
	}
	var replayEnvelope struct {
		Success bool                          `json:"success"`
		Data    submissionPublicationResponse `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayEnvelope); err != nil {
		t.Fatal(err)
	}
	if !replayEnvelope.Success || !replayEnvelope.Data.Replay || replayEnvelope.Data.PublicationEventID != firstEnvelope.Data.PublicationEventID ||
		replayEnvelope.Data.RiskRuleID != firstEnvelope.Data.RiskRuleID || replayEnvelope.Data.RiskRuleVersion != firstEnvelope.Data.RiskRuleVersion {
		t.Fatalf("unexpected exact replay response: %+v", replayEnvelope)
	}
	assertPublicationCounts(t, db, 1, 1)

	differentActor := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, "publisher-other")
	if differentActor.Code != http.StatusConflict || publicationErrorCode(t, differentActor) != "submission_publication_conflict" {
		t.Fatalf("different actor must conflict: %d %s", differentActor.Code, differentActor.Body.String())
	}
	assertPublicationCounts(t, db, 1, 1)

	if err := db.Delete(&database.RiskRule{}, firstEnvelope.Data.RiskRuleID).Error; err != nil {
		t.Fatal(err)
	}
	afterDelete := performPublicationHandlerRequest(db, uintString(submission.ID), "application/json", `{}`, publicationHandlerActor)
	if afterDelete.Code != http.StatusOK {
		t.Fatalf("historical replay after hard delete must return 200, got %d: %s", afterDelete.Code, afterDelete.Body.String())
	}
	var afterDeleteEnvelope struct {
		Success bool                          `json:"success"`
		Data    submissionPublicationResponse `json:"data"`
	}
	if err := json.Unmarshal(afterDelete.Body.Bytes(), &afterDeleteEnvelope); err != nil {
		t.Fatal(err)
	}
	if !afterDeleteEnvelope.Success || !afterDeleteEnvelope.Data.Replay ||
		afterDeleteEnvelope.Data.PublicationEventID != firstEnvelope.Data.PublicationEventID ||
		afterDeleteEnvelope.Data.RiskRuleID != firstEnvelope.Data.RiskRuleID ||
		afterDeleteEnvelope.Data.RiskRuleCode != firstEnvelope.Data.RiskRuleCode ||
		afterDeleteEnvelope.Data.RiskRuleVersion != firstEnvelope.Data.RiskRuleVersion {
		t.Fatalf("historical replay after hard delete must resolve the immutable published version: %+v", afterDeleteEnvelope)
	}
	assertPublicationCounts(t, db, 0, 1)
	var versionCount int64
	if err := db.Model(&database.RiskRuleVersion{}).
		Where("risk_rule_id = ? AND version = ?", firstEnvelope.Data.RiskRuleID, firstEnvelope.Data.RiskRuleVersion).
		Count(&versionCount).Error; err != nil {
		t.Fatal(err)
	}
	if versionCount != 1 {
		t.Fatalf("historical replay must preserve exactly one immutable published version, got %d", versionCount)
	}
	assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
	assertPublicationReviewUnchanged(t, db, reviewBefore)
}
