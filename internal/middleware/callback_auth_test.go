package middleware

import (
	"testing"

	"github.com/xzb177/yimao/internal/callback"
)

func TestAdminOnlyUnauthorizedReturnsOneHandledResponse(t *testing.T) {
	called := false
	h := (&AdminOnly{isAdmin: func(int64) bool { return false }}).Apply(callback.HandlerFunc(func(*callback.Context) (*callback.Response, error) {
		called = true
		return nil, nil
	}))
	resp, err := h.Handle(&callback.Context{UserID: 42})
	if err != nil || resp == nil || resp.CallbackMsg != "权限不足" || !resp.ShowAlert || resp.Text != "" || called {
		t.Fatalf("resp=%+v err=%v called=%v", resp, err, called)
	}
}

func TestAdminOnlyAuthorizedCallsNextWithoutError(t *testing.T) {
	want := &callback.Response{Text: "ok"}
	h := (&AdminOnly{isAdmin: func(int64) bool { return true }}).Apply(callback.HandlerFunc(func(*callback.Context) (*callback.Response, error) {
		return want, nil
	}))
	resp, err := h.Handle(&callback.Context{UserID: 42})
	if err != nil || resp != want {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
}
