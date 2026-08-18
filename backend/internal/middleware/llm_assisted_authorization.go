package middleware

import "github.com/gin-gonic/gin"

// LLMAssistedAnalysisAuthorization protects the explicit cost-bearing assisted
// analysis transport with the existing strict constant-time Bearer primitive.
func LLMAssistedAnalysisAuthorization(expectedToken string) gin.HandlerFunc {
	return bearerTokenAuthorization(expectedToken)
}
