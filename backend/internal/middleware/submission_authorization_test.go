package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

const testSubmissionWriteToken = "0123456789abcdef0123456789abcdef"

func TestSubmissionWriteAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		header     string
		expected   string
		statusCode int
		called     bool
	}{
		{name: "missing", expected: testSubmissionWriteToken, statusCode: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic abc", expected: testSubmissionWriteToken, statusCode: http.StatusUnauthorized},
		{name: "missing bearer token", header: "Bearer ", expected: testSubmissionWriteToken, statusCode: http.StatusUnauthorized},
		{name: "token with whitespace", header: "Bearer abc def", expected: testSubmissionWriteToken, statusCode: http.StatusUnauthorized},
		{name: "wrong token", header: "Bearer 0123456789abcdef0123456789abcdeg", expected: testSubmissionWriteToken, statusCode: http.StatusUnauthorized},
		{name: "blank expected token fails closed", header: "Bearer " + testSubmissionWriteToken, expected: "", statusCode: http.StatusUnauthorized},
		{name: "correct token", header: "Bearer " + testSubmissionWriteToken, expected: testSubmissionWriteToken, statusCode: http.StatusOK, called: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.GET("/protected", SubmissionWriteAuthorization(tt.expected), func(c *gin.Context) {
				called = true
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
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
			}
		})
	}
}

func TestSecureTokenEqual(t *testing.T) {
	if !secureTokenEqual(testSubmissionWriteToken, testSubmissionWriteToken) {
		t.Fatal("expected equal tokens to match")
	}
	if secureTokenEqual(testSubmissionWriteToken, testSubmissionWriteToken+"x") {
		t.Fatal("expected different-length tokens not to match")
	}
	if secureTokenEqual("", "") {
		t.Fatal("expected blank tokens to fail closed")
	}
}
