package errors

import (
	"fmt"
	"net/http"
)

// AppError represents an application error with context
type AppError struct {
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Details    string    `json:"details,omitempty"`
	StatusCode int       `json:"statusCode,omitempty"`
	Cause      error     `json:"-"`
	Timestamp  string    `json:"timestamp"`
	RequestID  string    `json:"requestId,omitempty"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause
func (e *AppError) Unwrap() error {
	return e.Cause
}

// New creates a new AppError
func New(code, message string) *AppError {
	return &AppError{
		Code:      code,
		Message:   message,
		Timestamp: timestamp(),
	}
}

// Wrap wraps an error with context
func Wrap(err error, code, message string) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{
		Code:      code,
		Message:   message,
		Cause:     err,
		Timestamp: timestamp(),
	}
}

// WithStatus adds HTTP status code
func (e *AppError) WithStatus(status int) *AppError {
	e.StatusCode = status
	return e
}

// WithDetails adds error details
func (e *AppError) WithDetails(details string) *AppError {
	e.Details = details
	return e
}

// WithRequestID adds request ID for tracing
func (e *AppError) WithRequestID(requestID string) *AppError {
	e.RequestID = requestID
	return e
}

// Common error constructors
func BadRequest(message string) *AppError {
	return New("ERR_BAD_REQUEST", message).WithStatus(http.StatusBadRequest)
}

func Unauthorized(message string) *AppError {
	return New("ERR_UNAUTHORIZED", message).WithStatus(http.StatusUnauthorized)
}

func Forbidden(message string) *AppError {
	return New("ERR_FORBIDDEN", message).WithStatus(http.StatusForbidden)
}

func NotFound(message string) *AppError {
	return New("ERR_NOT_FOUND", message).WithStatus(http.StatusNotFound)
}

func Conflict(message string) *AppError {
	return New("ERR_CONFLICT", message).WithStatus(http.StatusConflict)
}

func Internal(message string) *AppError {
	return New("ERR_INTERNAL", message).WithStatus(http.StatusInternalServerError)
}

func ServiceUnavailable(message string) *AppError {
	return New("ERR_UNAVAILABLE", message).WithStatus(http.StatusServiceUnavailable)
}

// Domain-specific errors
func UserNotFound(userID int64) *AppError {
	return NotFound(fmt.Sprintf("user %d not found", userID))
}

func MediaNotFound(mediaID string) *AppError {
	return NotFound(fmt.Sprintf("media %s not found", mediaID))
}

func InvalidMediaType(mediaType string) *AppError {
	return BadRequest(fmt.Sprintf("invalid media type: %s", mediaType))
}

func QuotaExceeded(quotaType, remaining int) *AppError {
	return BadRequest(fmt.Sprintf("%s quota exceeded. Remaining: %d", quotaType, remaining))
}

func RateLimitExceeded(retryAfter int) *AppError {
	return New("ERR_RATE_LIMIT", "rate limit exceeded").
		WithStatus(http.StatusTooManyRequests).
		WithDetails(fmt.Sprintf("retry after %d seconds", retryAfter))
}

func ExternalServiceFailed(service string, err error) *AppError {
	return Wrap(err, "ERR_EXTERNAL", fmt.Sprintf("%s service failed", service)).
		WithStatus(http.StatusBadGateway)
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

// GetAppError converts an error to AppError, returns Internal if not
func GetAppError(err error) *AppError {
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return Internal(err.Error())
}
