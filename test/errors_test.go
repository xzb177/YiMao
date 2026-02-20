package test

import (
	"testing"

	apperrors "emby-telegram-bot/internal/errors"
)

// TestErrors tests error handling system
func TestErrors(t *testing.T) {
	t.Run("NewError", func(t *testing.T) {
		err := apperrors.New("TEST_ERROR", "test error message")
		AssertNotNil(t, err)
		AssertEqual(t, err.Code, "TEST_ERROR")
		AssertEqual(t, err.Message, "test error message")
	})

	t.Run("Wrap", func(t *testing.T) {
		originalErr := apperrors.New("INNER", "inner error")
		err := apperrors.Wrap(originalErr, "OUTER", "outer error")
		AssertNotNil(t, err)
		AssertEqual(t, err.Code, "OUTER")
		AssertEqual(t, err.Message, "outer error")
		AssertNotNil(t, err.Unwrap())
		AssertEqual(t, err.Unwrap().Error(), originalErr.Error())
	})

	t.Run("WithStatus", func(t *testing.T) {
		err := apperrors.BadRequest("invalid input").WithStatus(400)
		AssertNotNil(t, err)
		AssertEqual(t, err.StatusCode, 400)
	})

	t.Run("WithDetails", func(t *testing.T) {
		err := apperrors.NotFound("resource").WithDetails("additional info")
		AssertNotNil(t, err)
		AssertEqual(t, err.Details, "additional info")
	})

	t.Run("Predefined errors", func(t *testing.T) {
		tests := []struct {
			name       string
			err        *apperrors.AppError
			wantCode   string
			wantStatus int
		}{
			{
				name:       "BadRequest",
				err:        apperrors.BadRequest("bad request"),
				wantCode:   "ERR_BAD_REQUEST",
				wantStatus: 400,
			},
			{
				name:       "Unauthorized",
				err:        apperrors.Unauthorized("unauthorized"),
				wantCode:   "ERR_UNAUTHORIZED",
				wantStatus: 401,
			},
			{
				name:       "NotFound",
				err:        apperrors.NotFound("not found"),
				wantCode:   "ERR_NOT_FOUND",
				wantStatus: 404,
			},
			{
				name:       "Conflict",
				err:        apperrors.Conflict("conflict"),
				wantCode:   "ERR_CONFLICT",
				wantStatus: 409,
			},
			{
				name:       "Internal",
				err:        apperrors.Internal("internal error"),
				wantCode:   "ERR_INTERNAL",
				wantStatus: 500,
			},
			{
				name:       "ServiceUnavailable",
				err:        apperrors.ServiceUnavailable("unavailable"),
				wantCode:   "ERR_UNAVAILABLE",
				wantStatus: 503,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				AssertEqual(t, tt.err.Code, tt.wantCode)
				AssertEqual(t, tt.err.StatusCode, tt.wantStatus)
			})
		}
	})

	t.Run("UserNotFound", func(t *testing.T) {
		err := apperrors.UserNotFound(123)
		AssertEqual(t, err.Code, "ERR_NOT_FOUND")
		AssertContains(t, err.Error(), "123")
	})

	t.Run("MediaNotFound", func(t *testing.T) {
		err := apperrors.MediaNotFound("movie456")
		AssertEqual(t, err.Code, "ERR_NOT_FOUND")
		AssertContains(t, err.Error(), "movie456")
	})

	t.Run("QuotaExceeded", func(t *testing.T) {
		err := apperrors.QuotaExceeded("movie", 2)
		AssertEqual(t, err.Code, "ERR_BAD_REQUEST")
		AssertContains(t, err.Error(), "movie")
		AssertContains(t, err.Error(), "2")
	})

	t.Run("ErrorCollector", func(t *testing.T) {
		collector := apperrors.NewErrorCollector()

		collector.Add(apperrors.BadRequest("error1"))
		collector.Add(apperrors.NotFound("error2"))
		collector.Add(apperrors.Internal("error3"))

		AssertTrue(t, collector.HasErrors(), "collector should have errors")
		AssertEqual(t, len(collector.ToError().Error()), len("error1; error2; error3"))
	})

	t.Run("GetAppError", func(t *testing.T) {
		standardErr := apperrors.BadRequest("standard error")
		wrappedErr := apperrors.Wrap(standardErr, "WRAPPED", "wrapped error")

		// GetAppError with AppError should return same error
		result := apperrors.GetAppError(standardErr)
		AssertEqual(t, result.Code, standardErr.Code)

		// GetAppError with wrapped error should wrap in AppError
		result2 := apperrors.GetAppError(wrappedErr)
		AssertEqual(t, result2.Code, "ERR_INTERNAL")
	})
}
