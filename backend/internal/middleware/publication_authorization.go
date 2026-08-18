package middleware

import "github.com/gin-gonic/gin"

// SubmissionPublicationAuthorization protects controlled publication transport.
// The expected token must be the independently configured publication credential.
func SubmissionPublicationAuthorization(expectedToken string) gin.HandlerFunc {
	return bearerTokenAuthorization(expectedToken)
}
