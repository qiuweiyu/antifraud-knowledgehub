package rule

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const MaxSubmissionRequestBodyBytes int64 = 32 * 1024

func SubmissionCreateHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" {
			response.Fail(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "rule submissions require application/json")
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxSubmissionRequestBodyBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()

		var draft DraftRequest
		if err := decoder.Decode(&draft); err != nil {
			writeSubmissionDecodeError(c, err)
			return
		}
		if err := requireSubmissionJSONEOF(decoder); err != nil {
			writeSubmissionDecodeError(c, err)
			return
		}

		submission, result, created, err := CreateOrReplayPendingSubmission(db, draft)
		if err != nil {
			response.Fail(c, http.StatusInternalServerError, "submission_create_failed", "rule submission could not be created")
			return
		}
		if !result.Valid {
			message := "rule submission is invalid"
			if len(result.Errors) > 0 {
				message = result.Errors[0].Message
			}
			response.Fail(c, http.StatusBadRequest, "invalid_submission", message)
			return
		}

		if created {
			response.Created(c, submission)
			return
		}
		response.OK(c, submission)
	}
}

func requireSubmissionJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values are not allowed")
	}
	return err
}

func writeSubmissionDecodeError(c *gin.Context, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		response.Fail(c, http.StatusRequestEntityTooLarge, "submission_body_too_large", "rule submission body exceeds 32 KiB")
		return
	}
	response.Fail(c, http.StatusBadRequest, "invalid_submission_json", "request body must be a single valid JSON object using supported fields")
}
