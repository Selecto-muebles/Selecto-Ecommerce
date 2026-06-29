package apperrors

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Code string

const (
	CodeInvalidInput      Code = "invalid_input"
	CodeUnauthorized      Code = "unauthorized"
	CodeForbidden         Code = "forbidden"
	CodeNotFound          Code = "not_found"
	CodeConflict          Code = "conflict"
	CodeRateLimited       Code = "rate_limited"
	CodeInternal          Code = "internal_error"
	CodeBadGateway        Code = "bad_gateway"
	CodeInvalidSignature  Code = "invalid_signature"
	CodeExpiredSignature  Code = "expired_signature"
	CodeInsufficientStock Code = "insufficient_stock"
)

func JSON(c *gin.Context, status int, code Code, message string, details gin.H) {
	payload := gin.H{
		"error":      message,
		"error_code": code,
	}
	if requestID, exists := c.Get("request_id"); exists {
		payload["request_id"] = requestID
	}
	if details != nil {
		payload["details"] = details
	}
	c.JSON(status, payload)
}

func BadRequest(c *gin.Context, message string) {
	JSON(c, http.StatusBadRequest, CodeInvalidInput, message, nil)
}

func Internal(c *gin.Context) {
	JSON(c, http.StatusInternalServerError, CodeInternal, "internal server error", nil)
}
