package rule

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const MaxSubmissionReviewRequestBodyBytes int64 = 16 * 1024

type submissionReviewRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type submissionReviewResponse struct {
	SubmissionID  uint      `json:"submission_id"`
	Status        string    `json:"status"`
	ReviewEventID uint      `json:"review_event_id"`
	Decision      string    `json:"decision"`
	ActorKind     string    `json:"actor_kind"`
	ActorLabel    string    `json:"actor_label"`
	CreatedAt     time.Time `json:"created_at"`
	Replay        bool      `json:"replay"`
}

func SubmissionReviewHandler(db *gorm.DB, actorLabel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" {
			response.Fail(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "rule submission reviews require application/json")
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxSubmissionReviewRequestBodyBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()

		var request *submissionReviewRequest
		if err := decoder.Decode(&request); err != nil {
			writeSubmissionReviewDecodeError(c, err)
			return
		}
		if request == nil {
			response.Fail(c, http.StatusBadRequest, "invalid_review_json", "request body must be a single valid JSON object using supported review fields")
			return
		}
		if err := requireSubmissionJSONEOF(decoder); err != nil {
			writeSubmissionReviewDecodeError(c, err)
			return
		}

		submissionID, err := parseSubmissionReviewID(c.Param("id"))
		if err != nil {
			response.Fail(c, http.StatusBadRequest, "invalid_submission_id", "rule submission id must be a positive decimal integer")
			return
		}

		outcome, err := ReviewPendingSubmission(db, submissionID, SubmissionReviewCommand{
			Decision:   request.Decision,
			Reason:     request.Reason,
			ActorLabel: actorLabel,
		})
		if err != nil {
			writeSubmissionReviewServiceError(c, err)
			return
		}

		response.OK(c, submissionReviewResponse{
			SubmissionID:  outcome.Submission.ID,
			Status:        outcome.Submission.Status,
			ReviewEventID: outcome.Event.ID,
			Decision:      outcome.Event.Decision,
			ActorKind:     outcome.Event.ActorKind,
			ActorLabel:    outcome.Event.ActorLabel,
			CreatedAt:     outcome.Event.CreatedAt,
			Replay:        outcome.Replay,
		})
	}
}

func parseSubmissionReviewID(raw string) (uint, error) {
	if raw == "" {
		return 0, errors.New("empty submission id")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, fmt.Errorf("submission id contains non-decimal byte at offset %d", i)
		}
	}
	parsed, err := strconv.ParseUint(raw, 10, strconv.IntSize)
	if err != nil || parsed == 0 {
		return 0, errors.New("submission id is not a positive uint")
	}
	return uint(parsed), nil
}

func writeSubmissionReviewDecodeError(c *gin.Context, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		response.Fail(c, http.StatusRequestEntityTooLarge, "review_body_too_large", "rule submission review body exceeds 16 KiB")
		return
	}
	response.Fail(c, http.StatusBadRequest, "invalid_review_json", "request body must be a single valid JSON object using supported review fields")
}

func writeSubmissionReviewServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Fail(c, http.StatusNotFound, "submission_not_found", "rule submission not found")
	case errors.Is(err, ErrInvalidSubmissionReview):
		response.Fail(c, http.StatusBadRequest, "invalid_submission_review", "invalid rule submission review")
	case errors.Is(err, ErrSubmissionReviewConflict):
		response.Fail(c, http.StatusConflict, "submission_review_conflict", "rule submission already has a different terminal review")
	case errors.Is(err, ErrSubmissionApprovalValidation):
		response.Fail(c, http.StatusConflict, "submission_approval_validation_failed", "rule submission is no longer valid for approval")
	case errors.Is(err, ErrSubmissionReviewIntegrity):
		response.Fail(c, http.StatusInternalServerError, "submission_review_integrity_error", "rule submission review integrity check failed")
	default:
		response.Fail(c, http.StatusInternalServerError, "submission_review_failed", "rule submission review could not be completed")
	}
}
