package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	testSubmissionPublicationToken = "publish-0123456789abcdef0123456789abcdef"
	testPublicationReviewToken      = "review-0123456789abcdef0123456789abcdef"
	testPublicationWriteToken       = "submit-0123456789abcdef0123456789abcdef"
)

func TestSubmissionPublicationAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		header     string
		expected   string
		statusCode int
		called     bool
	}{
		{name: "missing", expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic abc", expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "missing bearer token", header: "Bearer ", expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "token with whitespace", header: "Bearer abc def", expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "wrong token", header: "Bearer publish-0123456789abcdef0123456789abcdeg", expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "submission write token cannot publish", header: "Bearer " + testPublicationWriteToken, expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "review token cannot publish", header: "Bearer " + testPublicationReviewToken, expected: testSubmissionPublicationToken, statusCode: http.StatusUnauthorized},
		{name: "blank expected token fails closed", header: "Bearer " + testSubmissionPublicationToken, expected: "", statusCode: http.StatusUnauthorized},
		{name: "correct publication token", header: "Bearer " + testSubmissionPublicationToken, expected: testSubmissionPublicationToken, statusCode: http.StatusOK, called: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.POST("/protected-publication", SubmissionPublicationAuthorization(tt.expected), func(c *gin.Context) {
				called = true
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest(http.MethodPost, "/protected-publication", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)

			if recorder.Code != tt.statusCode {
				t.Fatalf("expected status %d, got %d", tt.statusCode, recorder.Code)
			}
			if called != tt.called {
				t.Fatalf("expected protected handler called=%v, got %v", tt.called, called)
			}
			if tt.statusCode == http.StatusUnauthorized {
				var envelope response.Envelope
				if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if envelope.Success || envelope.Error == nil || envelope.Error.Code != "unauthorized" {
					t.Fatalf("unexpected unauthorized response: %+v", envelope)
				}
				for _, secret := range []string{testSubmissionPublicationToken, testPublicationReviewToken, testPublicationWriteToken} {
					if strings.Contains(recorder.Body.String(), secret) {
						t.Fatal("publication authorization response must not reflect credential material")
					}
				}
			}
		})
	}
}

func TestSubmissionPublicationAuthorizationUsesSharedStrictBearerParser(t *testing.T) {
	malformed := []string{
		"bearer " + testSubmissionPublicationToken,
		"Bearer\t" + testSubmissionPublicationToken,
		"Bearer  " + testSubmissionPublicationToken,
		"Bearer " + testSubmissionPublicationToken + " ",
		"Bearer " + testSubmissionPublicationToken + "\n",
	}
	for _, header := range malformed {
		provided, ok := parseBearerToken(header)
		if ok || provided != "" {
			t.Fatalf("malformed publication authorization header must be rejected: %q", header)
		}
	}
}
