package errors

import (
	"fmt"
)

// ErrorCode represents a unique error code
type ErrorCode string

const (
	// General errors
	ErrCodeInternal   ErrorCode = "INTERNAL_ERROR"
	ErrCodeInvalidInput ErrorCode = "INVALID_INPUT"
	ErrCodeNotFound    ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden   ErrorCode = "FORBIDDEN"
	ErrCodeRateLimit   ErrorCode = "RATE_LIMIT_EXCEEDED"

	// Bot-specific errors
	ErrCodeCallbackInvalid ErrorCode = "CALLBACK_INVALID"
	ErrCodeSessionExpired ErrorCode = "SESSION_EXPIRED"
	ErrCodeQuotaExceeded  ErrorCode = "QUOTA_EXCEEDED"

	// Service errors
	ErrCodeMoviePilotError ErrorCode = "MOVIEPILOT_ERROR"
	ErrCodeJellyseerrError ErrorCode = "JELLYSEERR_ERROR" // Deprecated, kept for compatibility
	ErrCodeAIError         ErrorCode = "AI_ERROR"
	ErrCodeMediaNotFound   ErrorCode = "MEDIA_NOT_FOUND"
)

// AppError represents an application error with context
type AppError struct {
	Code    ErrorCode
	Message string
	Details map[string]interface{}
	Cause   error
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap implements the errors.Unwrap interface
func (e *AppError) Unwrap() error {
	return e.Cause
}

// New creates a new AppError
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Details: make(map[string]interface{}),
	}
}

// Wrap wraps an existing error with context
func Wrap(code ErrorCode, message string, cause error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Details: make(map[string]interface{}),
		Cause:   cause,
	}
}

// WithDetail adds a detail to the error
func (e *AppError) WithDetail(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// Is checks if an error is of a specific type
func Is(err error, code ErrorCode) bool {
	appErr, ok := err.(*AppError)
	if !ok {
		return false
	}
	return appErr.Code == code
}

// Common error constructors
func InternalErr(msg string) *AppError {
	return New(ErrCodeInternal, msg)
}

func InvalidInput(msg string) *AppError {
	return New(ErrCodeInvalidInput, msg)
}

func NotFound(msg string) *AppError {
	return New(ErrCodeNotFound, msg)
}

func Unauthorized(msg string) *AppError {
	return New(ErrCodeUnauthorized, msg)
}

func Forbidden(msg string) *AppError {
	return New(ErrCodeForbidden, msg)
}

func CallbackInvalid(msg string) *AppError {
	return New(ErrCodeCallbackInvalid, msg)
}

func SessionExpired(msg string) *AppError {
	return New(ErrCodeSessionExpired, msg)
}

func QuotaExceeded(msg string) *AppError {
	return New(ErrCodeQuotaExceeded, msg)
}

func JellyseerrErr(msg string, cause error) *AppError {
	return Wrap(ErrCodeJellyseerrError, msg, cause)
}

func MoviePilotErr(msg string, cause error) *AppError {
	return Wrap(ErrCodeMoviePilotError, msg, cause)
}

func AIErr(msg string, cause error) *AppError {
	return Wrap(ErrCodeAIError, msg, cause)
}

func MediaNotFound(msg string) *AppError {
	return New(ErrCodeMediaNotFound, msg)
}
