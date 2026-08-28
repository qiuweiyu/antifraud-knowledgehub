package rule

import (
	"errors"
	"net/http"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RevisionPublicationI2Guard keeps I2 proposal/review-only. It must run after
// publication authorization and before SubmissionPublicationHandler so a
// revision can never fall through to the pre-I3 create-publication service.
func RevisionPublicationI2Guard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		submissionID, err := parseSubmissionReviewID(c.Param("id"))
		if err != nil {
			// Preserve the publication handler's existing input/error contract.
			c.Next()
			return
		}
		if db == nil {
			response.Fail(c, http.StatusInternalServerError, "submission_publication_failed", "rule submission publication could not be completed")
			c.Abort()
			return
		}

		var submission database.RuleSubmission
		if err := db.First(&submission, submissionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Let the existing handler preserve its 404 contract.
				c.Next()
				return
			}
			response.Fail(c, http.StatusInternalServerError, "submission_publication_failed", "rule submission publication could not be completed")
			c.Abort()
			return
		}

		kind := strings.TrimSpace(submission.Kind)
		switch kind {
		case "", database.RuleSubmissionKindCreate:
			c.Next()
		case database.RuleSubmissionKindRevision:
			response.Fail(c, http.StatusConflict, "submission_not_publishable", "rule revision publication is not enabled before I3")
			c.Abort()
		default:
			response.Fail(c, http.StatusInternalServerError, "submission_publication_integrity_error", "rule submission publication integrity check failed")
			c.Abort()
		}
	}
}
