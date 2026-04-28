package middleware

import (
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/pkg/logger"
	"emby-telegram-bot/pkg/errors"
)

// Logger logs callback processing
func Logger(next callback.Handler) callback.Handler {
	return callback.HandlerFunc(func(ctx *callback.Context) (*callback.Response, error) {
		start := time.Now()
		logger.Info("[Callback] Started: action=%s, userID=%d", ctx.Callback.Action, ctx.UserID)

		resp, err := next.Handle(ctx)

		duration := time.Since(start)
		if err != nil {
			logger.Info("[Callback] Error: action=%s, userID=%d, duration=%v, error=%v",
				ctx.Callback.Action, ctx.UserID, duration, err)
		} else {
			logger.Info("[Callback] Completed: action=%s, userID=%d, duration=%v, edit=%v",
				ctx.Callback.Action, ctx.UserID, duration, resp.Edit)
		}

		return resp, err
	})
}

// Recovery recovers from panics
func Recovery(next callback.Handler) callback.Handler {
	return callback.HandlerFunc(func(ctx *callback.Context) (resp *callback.Response, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Info("[Callback] Panic recovered: action=%s, userID=%d, panic=%v",
					ctx.Callback.Action, ctx.UserID, r)
				err = errors.InternalErr("internal error occurred")
				resp = &callback.Response{
					Text:        "❌ 发生内部错误",
					CallbackMsg: "操作失败",
					ShowAlert:   true,
				}
			}
		}()
		return next.Handle(ctx)
	})
}

// Validator validates callback data
func Validator(next callback.Handler) callback.Handler {
	return callback.HandlerFunc(func(ctx *callback.Context) (*callback.Response, error) {
		// Validate required fields
		if ctx.Callback == nil {
			return nil, errors.CallbackInvalid("callback data is nil")
		}

		if ctx.Callback.Action == "" {
			return nil, errors.CallbackInvalid("action is required")
		}

		if ctx.UserID == 0 {
			return nil, errors.Unauthorized("user ID is required")
		}

		return next.Handle(ctx)
	})
}

// SessionValidator ensures user has a valid session
type SessionValidator struct {
	sessionMgr interface {
		Get(userID int64) interface{}
		IsValid(userID int64) bool
	}
}

func (s *SessionValidator) Apply(next callback.Handler) callback.Handler {
	return callback.HandlerFunc(func(ctx *callback.Context) (*callback.Response, error) {
		if s.sessionMgr != nil && !s.sessionMgr.IsValid(ctx.UserID) {
			return nil, errors.SessionExpired("session expired or not found")
		}

		// Store session data in context if available
		if session := s.sessionMgr.Get(ctx.UserID); session != nil {
			ctx.SessionData = session
		}

		return next.Handle(ctx)
	})
}

// AdminOnly restricts handler to admins only
type AdminOnly struct {
	isAdmin func(int64) bool
}

func (a *AdminOnly) Apply(next callback.Handler) callback.Handler {
	return callback.HandlerFunc(func(ctx *callback.Context) (*callback.Response, error) {
		if !a.isAdmin(ctx.UserID) {
			return &callback.Response{
				Text:        "❌ 此功能仅管理员可用",
				CallbackMsg: "权限不足",
				ShowAlert:   true,
			}, errors.Forbidden("user is not admin")
		}

		return next.Handle(ctx)
	})
}

// RateLimiter limits callback processing rate per user
type RateLimiter struct {
	limiter interface {
		Allow(userID int64) bool
	}
}

func (r *RateLimiter) Apply(next callback.Handler) callback.Handler {
	return callback.HandlerFunc(func(ctx *callback.Context) (*callback.Response, error) {
		if r.limiter != nil && !r.limiter.Allow(ctx.UserID) {
			return &callback.Response{
				Text:        "⏱️ 操作太频繁，请稍后再试",
				CallbackMsg: "操作太频繁",
				ShowAlert:   true,
			}, errors.New(errors.ErrCodeRateLimit, "rate limit exceeded")
		}

		return next.Handle(ctx)
	})
}
