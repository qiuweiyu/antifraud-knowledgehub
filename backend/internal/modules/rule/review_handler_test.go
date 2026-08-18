package rule

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func reviewHandlerTestRouter(db *gorm.DB, actorLabel string) http.Handler {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/rule-submissions/:id/reviews", SubmissionReviewHandler(db, actorLabel))
	return router
}

func performReviewHandlerRequest(router http.Handler, id, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule-submissions/"+url.PathEscape(id)+"/reviews", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func assertReviewHandlerError(t *testing.T, resp *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if resp.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("expected error code %q, got %s", code, resp.Body.String())
	}
}

func TestSubmissionReviewHandlerRejectsInvalidTransportBeforeMutation(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_http_transport_failures")
	router := reviewHandlerTestRouter(db, "maintainer-http")
	id := uintString(submission.ID)

	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
	}{
		{name: "missing content type", body: `{"decision":"approved","reason":"reason"}`, status: http.StatusUnsupportedMediaType, code: "unsupported_media_type"},
		{name: "wrong content type", contentType: "text/plain", body: `{"decision":"approved","reason":"reason"}`, status: http.StatusUnsupportedMediaType, code: "unsupported_media_type"},
		{name: "oversized body", contentType: "application/json", body: `{"decision":"approved","reason":"` + strings.Repeat("x", 17*1024) + `"}`, status: http.StatusRequestEntityTooLarge, code: "review_body_too_large"},
		{name: "empty body", contentType: "application/json", body: "", status: http.StatusBadRequest, code: "invalid_review_json"},
		{name: "malformed json", contentType: "application/json", body: `{"decision":`, status: http.StatusBadRequest, code: "invalid_review_json"},
		{name: "unknown actor field", contentType: "application/json", body: `{"decision":"approved","reason":"reason","actor_label":"client"}`, status: http.StatusBadRequest, code: "invalid_review_json"},
		{name: "trailing json", contentType: "application/json", body: `{"decision":"approved","reason":"reason"}{}`, status: http.StatusBadRequest, code: "invalid_review_json"},
		{name: "null", contentType: "application/json", body: `null`, status: http.StatusBadRequest, code: "invalid_review_json"},
		{name: "array", contentType: "application/json", body: `[]`, status: http.StatusBadRequest, code: "invalid_review_json"},
		{name: "scalar", contentType: "application/json", body: `"approved"`, status: http.StatusBadRequest, code: "invalid_review_json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := performReviewHandlerRequest(router, id, tt.contentType, tt.body)
			assertReviewHandlerError(t, resp, tt.status, tt.code)
			assertReviewStillPendingWithNoEvent(t, db, submission.ID)
			if got := countRiskRuleRows(t, db); got != 0 {
				t.Fatalf("transport failure must not create RiskRule rows, got %d", got)
			}
		})
	}
}

func TestSubmissionReviewHandlerStrictSubmissionID(t *testing.T) {
	db := reviewTestDB(t)
	router := reviewHandlerTestRouter(db, "maintainer-http")
	body := `{"decision":"approved","reason":"reason"}`
	for _, id := range []string{"0", "-1", "+1", "1.0", "0x10", " 1", "1 ", "abc", "184467440737095516161"} {
		t.Run(id, func(t *testing.T) {
			resp := performReviewHandlerRequest(router, id, "application/json", body)
			assertReviewHandlerError(t, resp, http.StatusBadRequest, "invalid_submission_id")
			if got := countReviewEventRows(t, db); got != 0 {
				t.Fatalf("invalid id must not append review events, got %d", got)
			}
			if got := countRiskRuleRows(t, db); got != 0 {
				t.Fatalf("invalid id must not create RiskRule rows, got %d", got)
			}
		})
	}
}

func TestSubmissionReviewHandlerMapsNotFoundAndInvalidCommand(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_http_invalid_command")
	router := reviewHandlerTestRouter(db, "maintainer-http")

	notFound := performReviewHandlerRequest(router, "999999", "application/json", `{"decision":"approved","reason":"reason"}`)
	assertReviewHandlerError(t, notFound, http.StatusNotFound, "submission_not_found")

	invalidDecision := performReviewHandlerRequest(router, uintString(submission.ID), "application/json", `{"decision":"change_requested","reason":"reason"}`)
	assertReviewHandlerError(t, invalidDecision, http.StatusBadRequest, "invalid_submission_review")
	assertReviewStillPendingWithNoEvent(t, db, submission.ID)

	blankReason := performReviewHandlerRequest(router, uintString(submission.ID), "application/json", `{"decision":"approved","reason":"   "}`)
	assertReviewHandlerError(t, blankReason, http.StatusBadRequest, "invalid_submission_review")
	assertReviewStillPendingWithNoEvent(t, db, submission.ID)

	overReasonBytes, err := json.Marshal(submissionReviewRequest{Decision: ApprovedSubmissionStatus, Reason: strings.Repeat("界", 667)})
	if err != nil {
		t.Fatal(err)
	}
	overReason := performReviewHandlerRequest(router, uintString(submission.ID), "application/json", string(overReasonBytes))
	assertReviewHandlerError(t, overReason, http.StatusBadRequest, "invalid_submission_review")
	assertReviewStillPendingWithNoEvent(t, db, submission.ID)

	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("invalid review commands must not create RiskRule rows, got %d", got)
	}
}

func TestSubmissionReviewHandlerApprovalValidationConflictWritesNothing(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_http_revalidation")
	if err := db.Where("code = ?", "fake_customer_service").Delete(&database.Category{}).Error; err != nil {
		t.Fatal(err)
	}

	resp := performReviewHandlerRequest(
		reviewHandlerTestRouter(db, "maintainer-http"),
		uintString(submission.ID),
		"application/json",
		`{"decision":"approved","reason":"should fail current validation"}`,
	)
	assertReviewHandlerError(t, resp, http.StatusConflict, "submission_approval_validation_failed")
	assertReviewStillPendingWithNoEvent(t, db, submission.ID)
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("failed approval must not create RiskRule rows, got %d", got)
	}
}

func TestSubmissionReviewHandlerApproveReplayAndConflicts(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_http_approve")
	router := reviewHandlerTestRouter(db, "maintainer-http")
	id := uintString(submission.ID)
	body := `{"decision":"approved","reason":"  bounded review reason  "}`

	first := performReviewHandlerRequest(router, id, "application/json", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first approval: expected 200, got %d: %s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), "bounded review reason") || strings.Contains(first.Body.String(), "draft_digest") {
		t.Fatalf("success response must not expose review reason or draft digest: %s", first.Body.String())
	}
	var firstEnvelope struct {
		Data submissionReviewResponse `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	if firstEnvelope.Data.SubmissionID != submission.ID || firstEnvelope.Data.Status != ApprovedSubmissionStatus || firstEnvelope.Data.Decision != ApprovedSubmissionStatus {
		t.Fatalf("unexpected approval response: %+v", firstEnvelope.Data)
	}
	if firstEnvelope.Data.Replay || firstEnvelope.Data.ReviewEventID == 0 || firstEnvelope.Data.ActorKind != ControlledMaintainerActorKind || firstEnvelope.Data.ActorLabel != "maintainer-http" {
		t.Fatalf("unexpected first-review metadata: %+v", firstEnvelope.Data)
	}
	if got := countReviewEventRows(t, db); got != 1 {
		t.Fatalf("first approval must create exactly one event, got %d", got)
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("approval transport must not publish RiskRule rows, got %d", got)
	}

	replay := performReviewHandlerRequest(router, id, "application/json", `{"decision":"approved","reason":"bounded review reason"}`)
	if replay.Code != http.StatusOK {
		t.Fatalf("exact replay: expected 200, got %d: %s", replay.Code, replay.Body.String())
	}
	var replayEnvelope struct {
		Data submissionReviewResponse `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayEnvelope); err != nil {
		t.Fatal(err)
	}
	if !replayEnvelope.Data.Replay || replayEnvelope.Data.ReviewEventID != firstEnvelope.Data.ReviewEventID {
		t.Fatalf("exact replay must return the same event with replay=true: first=%+v replay=%+v", firstEnvelope.Data, replayEnvelope.Data)
	}
	if got := countReviewEventRows(t, db); got != 1 {
		t.Fatalf("exact replay must not append events, got %d", got)
	}

	for _, tt := range []struct {
		name   string
		router http.Handler
		body   string
	}{
		{name: "different decision", router: router, body: `{"decision":"rejected","reason":"bounded review reason"}`},
		{name: "different reason", router: router, body: `{"decision":"approved","reason":"different reason"}`},
		{name: "different actor", router: reviewHandlerTestRouter(db, "maintainer-other"), body: `{"decision":"approved","reason":"bounded review reason"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := performReviewHandlerRequest(tt.router, id, "application/json", tt.body)
			assertReviewHandlerError(t, resp, http.StatusConflict, "submission_review_conflict")
			if got := countReviewEventRows(t, db); got != 1 {
				t.Fatalf("conflict must not append events, got %d", got)
			}
			if got := countRiskRuleRows(t, db); got != 0 {
				t.Fatalf("conflict must not create RiskRule rows, got %d", got)
			}
		})
	}
}

func TestSubmissionReviewHandlerRejectsPendingSubmission(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_http_reject")
	resp := performReviewHandlerRequest(
		reviewHandlerTestRouter(db, "maintainer-http"),
		uintString(submission.ID),
		"application/json",
		`{"decision":"rejected","reason":"bounded rejection reason"}`,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("rejection: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var stored database.RuleSubmission
	if err := db.First(&stored, submission.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != RejectedSubmissionStatus || countReviewEventRows(t, db) != 1 {
		t.Fatalf("expected one terminal rejection, got status=%s events=%d", stored.Status, countReviewEventRows(t, db))
	}
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("rejection transport must not create RiskRule rows, got %d", got)
	}
}

func TestSubmissionReviewHandlerMapsIntegrityFailureWithoutMutation(t *testing.T) {
	db := reviewTestDB(t)
	submission := createReviewPendingSubmission(t, db, "review_http_integrity")
	badDigest := strings.Repeat("0", 64)
	if err := db.Model(&database.RuleSubmission{}).Where("id = ?", submission.ID).Update("draft_digest", badDigest).Error; err != nil {
		t.Fatal(err)
	}

	resp := performReviewHandlerRequest(
		reviewHandlerTestRouter(db, "maintainer-http"),
		uintString(submission.ID),
		"application/json",
		`{"decision":"rejected","reason":"integrity must fail closed"}`,
	)
	assertReviewHandlerError(t, resp, http.StatusInternalServerError, "submission_review_integrity_error")
	assertReviewStillPendingWithNoEvent(t, db, submission.ID)
	if got := countRiskRuleRows(t, db); got != 0 {
		t.Fatalf("integrity failure must not create RiskRule rows, got %d", got)
	}
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
