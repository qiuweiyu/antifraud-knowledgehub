package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

const bearerPrefix = "Bearer "

func SubmissionWriteAuthorization(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		providedToken, ok := parseBearerToken(c.GetHeader("Authorization"))
		if !ok || !secureTokenEqual(providedToken, expectedToken) {
			response.Fail(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
			c.Abort()
			return
		}
		c.Next()
	}
}

func parseBearerToken(header string) (string, bool) {
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", false
	}
	token := strings.TrimPrefix(header, bearerPrefix)
	if token == "" || strings.TrimSpace(token) != token || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

func secureTokenEqual(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	providedDigest := sha256.Sum256([]byte(provided))
	expectedDigest := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) == 1
}
