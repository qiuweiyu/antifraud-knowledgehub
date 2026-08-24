package rule

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const MaxSubmissionPublicationRequestBodyBytes int64 = 4 * 1024

type submissionPublicationRequest struct{}

type submissionPublicationResponse struct {
	SubmissionID       uint      `json:"submission_id"`
	Status             string    `json:"status"`
	ReviewEventID      uint      `json:"review_event_id"`
	PublicationEventID uint      `json:"publication_event_id"`
	RiskRuleID         uint      `json:"risk_rule_id"`
	RiskRuleCode       string    `json:"risk_rule_code"`
	RiskRuleVersion    uint      `json:"risk_rule_version"`
	ActorKind          string    `json:"actor_kind"`
	ActorLabel         string    `json:"actor_label"`
	CreatedAt          time.Time `json:"created_at"`
	Replay             bool      `json:"replay"`
}

func SubmissionPublicationHandler(db *gorm.DB, actorLabel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" {
			response.Fail(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "rule submission publications require application/json")
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxSubmissionPublicationRequestBodyBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()

		var request *submissionPublicationRequest
		if err := decoder.Decode(&request); err != nil {
			writeSubmissionPublicationDecodeError(c, err)
			return
		}
		if request == nil {
			response.Fail(c, http.StatusBadRequest, "invalid_publication_json", "request body must be a single empty JSON object")
			return
		}
		if err := requireSubmissionJSONEOF(decoder); err != nil {
			writeSubmissionPublicationDecodeError(c, err)
			return
		}

		submissionID, err := parseSubmissionReviewID(c.Param("id"))
		if err != nil {
			response.Fail(c, http.StatusBadRequest, "invalid_submission_id", "rule submission id must be a positive decimal integer")
			return
		}

		outcome, err := PublishApprovedSubmission(db, submissionID, SubmissionPublicationCommand{ActorLabel: actorLabel})
		if err != nil {
			writeSubmissionPublicationServiceError(c, err)
			return
		}

		data := submissionPublicationResponse{
			SubmissionID:       outcome.Event.SubmissionID,
			Status:             outcome.Submission.Status,
			ReviewEventID:      outcome.Event.ReviewEventID,
			PublicationEventID: outcome.Event.ID,
			RiskRuleID:         outcome.Event.RiskRuleID,
			RiskRuleCode:       outcome.Event.RiskRuleCode,
			RiskRuleVersion:    outcome.RuleVersion.Version,
			ActorKind:          outcome.Event.ActorKind,
			ActorLabel:         outcome.Event.ActorLabel,
			CreatedAt:          outcome.Event.CreatedAt,
			Replay:             outcome.Replay,
		}
		if outcome.Replay {
			response.OK(c, data)
			return
		}
		response.Created(c, data)
	}
}

func writeSubmissionPublicationDecodeError(c *gin.Context, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		response.Fail(c, http.StatusRequestEntityTooLarge, "publication_body_too_large", "rule submission publication body exceeds 4 KiB")
		return
	}
	response.Fail(c, http.StatusBadRequest, "invalid_publication_json", "request body must be a single empty JSON object")
}

func writeSubmissionPublicationServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Fail(c, http.StatusNotFound, "submission_not_found", "rule submission not found")
	case errors.Is(err, ErrSubmissionNotPublishable):
		response.Fail(c, http.StatusConflict, "submission_not_publishable", "rule submission is not publishable")
	case errors.Is(err, ErrSubmissionPublicationValidation):
		response.Fail(c, http.StatusConflict, "submission_publication_validation_failed", "rule submission is no longer valid for publication")
	case errors.Is(err, ErrSubmissionPublicationConflict):
		response.Fail(c, http.StatusConflict, "submission_publication_conflict", "rule submission publication conflicts with current state")
	case errors.Is(err, ErrSubmissionPublicationIntegrity):
		response.Fail(c, http.StatusInternalServerError, "submission_publication_integrity_error", "rule submission publication integrity check failed")
	default:
		response.Fail(c, http.StatusInternalServerError, "submission_publication_failed", "rule submission publication could not be completed")
	}
}