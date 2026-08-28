package rule

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RevisionSubmissionCreateHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" {
			response.Fail(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "rule revision submissions require application/json")
			return
		}

		targetID, err := parseRevisionTargetID(c.Param("id"))
		if err != nil {
			response.Fail(c, http.StatusBadRequest, "invalid_rule_id", "rule id must be a positive decimal integer")
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxSubmissionRequestBodyBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()

		var request *RevisionDraftRequest
		if err := decoder.Decode(&request); err != nil {
			writeSubmissionDecodeError(c, err)
			return
		}
		if request == nil {
			response.Fail(c, http.StatusBadRequest, "invalid_submission_json", "request body must be a single valid JSON object using supported revision fields")
			return
		}
		if err := requireSubmissionJSONEOF(decoder); err != nil {
			writeSubmissionDecodeError(c, err)
			return
		}

		submission, validation, created, err := CreateOrReplayPendingRevisionSubmission(db, targetID, *request)
		if err != nil {
			writeRevisionSubmissionServiceError(c, err)
			return
		}
		if !validation.Valid {
			writeRevisionSubmissionValidationError(c, validation)
			return
		}
		if created {
			response.Created(c, submission)
			return
		}
		response.OK(c, submission)
	}
}

func parseRevisionTargetID(raw string) (uint, error) {
	if raw == "" {
		return 0, errors.New("empty rule id")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, fmt.Errorf("rule id contains non-decimal byte at offset %d", i)
		}
	}
	parsed, err := strconv.ParseUint(raw, 10, strconv.IntSize)
	if err != nil || parsed == 0 {
		return 0, errors.New("rule id is not a positive uint")
	}
	return uint(parsed), nil
}

func writeRevisionSubmissionServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRevisionSubmission):
		response.Fail(c, http.StatusBadRequest, "invalid_revision_submission", "invalid rule revision submission")
	case errors.Is(err, ErrRevisionRuleNotFound):
		response.Fail(c, http.StatusNotFound, "rule_not_found", "target rule not found")
	case errors.Is(err, ErrRevisionStaleBaseVersion):
		response.Fail(c, http.StatusConflict, "stale_base_version", "revision base version is no longer current")
	case errors.Is(err, ErrRevisionRuleVersionIntegrity):
		response.Fail(c, http.StatusInternalServerError, "rule_version_integrity_error", "rule version integrity check failed")
	default:
		response.Fail(c, http.StatusInternalServerError, "revision_submission_create_failed", "rule revision submission could not be created")
	}
}

func writeRevisionSubmissionValidationError(c *gin.Context, validation ValidationResult) {
	code := "invalid_revision_submission"
	message := "rule revision submission is invalid"
	status := http.StatusBadRequest
	if len(validation.Errors) > 0 {
		message = validation.Errors[0].Message
		switch validation.Errors[0].Code {
		case "no_changes", "code_immutable":
			status = http.StatusConflict
			code = validation.Errors[0].Code
		}
	}
	response.Fail(c, status, code, message)
}
