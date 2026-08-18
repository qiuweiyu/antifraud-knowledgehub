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
	testSubmissionReviewToken = "review-0123456789abcdef0123456789abcdef"
	testSubmissionWriteOnlyToken = "submit-0123456789abcdef0123456789abcdef"
)

func TestSubmissionReviewAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		header     string
		expected   string
		statusCode int
		called     bool
	}{
		{name: "missing", expected: testSubmissionReviewToken, statusCode: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic abc", expected: testSubmissionReviewToken, statusCode: http.StatusUnauthorized},
		{name: "missing bearer token", header: "Bearer ", expected: testSubmissionReviewToken, statusCode: http.StatusUnauthorized},
		{name: "token with whitespace", header: "Bearer abc def", expected: testSubmissionReviewToken, statusCode: http.StatusUnauthorized},
		{name: "wrong token", header: "Bearer review-0123456789abcdef0123456789abcdeg", expected: testSubmissionReviewToken, statusCode: http.StatusUnauthorized},
		{name: "submission write token cannot review", header: "Bearer " + testSubmissionWriteOnlyToken, expected: testSubmissionReviewToken, statusCode: http.StatusUnauthorized},
		{name: "blank expected token fails closed", header: "Bearer " + testSubmissionReviewToken, expected: "", statusCode: http.StatusUnauthorized},
		{name: "correct review token", header: "Bearer " + testSubmissionReviewToken, expected: testSubmissionReviewToken, statusCode: http.StatusOK, called: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.POST("/protected-review", SubmissionReviewAuthorization(tt.expected), func(c *gin.Context) {
				called = true
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest(http.MethodPost, "/protected-review", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)

			if recorder.Code != tt.statusCode {
				t.Fatalf("expected status %d, got %d", tt.statusCode, recorder.Code)
			}
			if called != tt.called {
				t.Fatalf("expected handler called=%v, got %v", tt.called, called)
			}
			if tt.statusCode == http.StatusUnauthorized {
				var envelope response.Envelope
				if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if envelope.Success || envelope.Error == nil || envelope.Error.Code != "unauthorized" {
					t.Fatalf("unexpected unauthorized response: %+v", envelope)
				}
				if strings.Contains(recorder.Body.String(), testSubmissionReviewToken) || strings.Contains(recorder.Body.String(), testSubmissionWriteOnlyToken) {
					t.Fatal("authorization error response must not reflect credential material")
				}
			}
		})
	}
}

func TestSubmissionReviewAuthorizationUsesSharedStrictBearerParser(t *testing.T) {
	malformed := []string{
		"bearer " + testSubmissionReviewToken,
		"Bearer\t" + testSubmissionReviewToken,
		"Bearer  " + testSubmissionReviewToken,
		"Bearer " + testSubmissionReviewToken + " ",
		"Bearer " + testSubmissionReviewToken + "\n",
	}
	for _, header := range malformed {
		provided, ok := parseBearerToken(header)
		if ok || provided != "" {
			t.Fatalf("malformed review authorization header must be rejected: %q", header)
		}
	}
}
