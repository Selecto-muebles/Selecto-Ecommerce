package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizedInternalAuthHeadersAcceptsCutoverAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, timestampHeader, signatureHeader string
	}{
		{name: "standard", timestampHeader: serviceTimestampHeader, signatureHeader: serviceSignatureHeader},
		{name: "selecto legacy", timestampHeader: legacyTimestampHeader, signatureHeader: legacySignatureHeader},
		{name: "destry legacy", timestampHeader: destryTimestampHeader, signatureHeader: destrySignatureHeader},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("POST", "/payments/webhook", nil)
			context.Request.Header.Set(test.timestampHeader, "123")
			context.Request.Header.Set(test.signatureHeader, "signature")
			headers := normalizedInternalAuthHeaders(context)
			if headers.timestamp != "123" || headers.signature != "signature" {
				t.Fatalf("normalized headers = %+v", headers)
			}
		})
	}
}
