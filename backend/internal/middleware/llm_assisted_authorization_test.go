package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

const testLLMAssistedHTTPToken = "abcdef0123456789abcdef0123456789"

func TestLLMAssistedAnalysisAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		header     string
		expected   string
		statusCode int
		called     bool
	}{
		{name: "missing", expected: testLLMAssistedHTTPToken, statusCode: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic abc", expected: testLLMAssistedHTTPToken, statusCode: http.StatusUnauthorized},
		{name: "missing token", header: "Bearer ", expected: testLLMAssistedHTTPToken, statusCode: http.StatusUnauthorized},
		{name: "whitespace", header: "Bearer abc def", expected: testLLMAssistedHTTPToken, statusCode: http.StatusUnauthorized},
		{name: "wrong token", header: "Bearer abcdef0123456789abcdef0123456788", expected: testLLMAssistedHTTPToken, statusCode: http.StatusUnauthorized},
		{name: "blank expected fails closed", header: "Bearer " + testLLMAssistedHTTPToken, expected: "", statusCode: http.StatusUnauthorized},
		{name: "valid", header: "Bearer " + testLLMAssistedHTTPToken, expected: testLLMAssistedHTTPToken, statusCode: http.StatusOK, called: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.POST("/assisted", LLMAssistedAnalysisAuthorization(tc.expected), func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/assisted", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			if recorder.Code != tc.statusCode {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.statusCode)
			}
			if called != tc.called {
				t.Fatalf("handler called=%v, want %v", called, tc.called)
			}
			if stringsContains(recorder.Body.String(), testLLMAssistedHTTPToken) {
				t.Fatal("transport token must never be reflected")
			}
		})
	}
}

func stringsContains(value, needle string) bool {
	return needle != "" && len(value) >= len(needle) && stringContains(value, needle)
}

func stringContains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
