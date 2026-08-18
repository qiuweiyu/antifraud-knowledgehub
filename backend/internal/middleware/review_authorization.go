package middleware

import "github.com/gin-gonic/gin"

// SubmissionReviewAuthorization protects controlled maintainer review transport.
// The expected token must be the independently configured review credential.
func SubmissionReviewAuthorization(expectedToken string) gin.HandlerFunc {
	return bearerTokenAuthorization(expectedToken)
}
