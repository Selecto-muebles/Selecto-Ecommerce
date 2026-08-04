package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestClientAuthorizationHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		direct    string
		forwarded string
		want      string
	}{
		{name: "direct request", direct: "Bearer customer-token", want: "Bearer customer-token"},
		{name: "api gateway request", direct: "Bearer gateway-id-token", forwarded: "Bearer customer-token", want: "Bearer customer-token"},
		{name: "blank forwarded header", direct: "Bearer customer-token", forwarded: "  ", want: "Bearer customer-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/me", nil)
			if tt.direct != "" {
				request.Header.Set("Authorization", tt.direct)
			}
			if tt.forwarded != "" {
				request.Header.Set("X-Forwarded-Authorization", tt.forwarded)
			}
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = request

			if got := clientAuthorizationHeader(context); got != tt.want {
				t.Fatalf("clientAuthorizationHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}
