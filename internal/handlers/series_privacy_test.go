package handlers

import (
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/session"
)

func TestSeriesSuggestionRejectsInvalidPrivateTarget(t *testing.T) {
	h := &RequestHandler{sessMgr: session.NewManager(time.Hour, 10)}
	// The helper must fail closed before any Telegram send when no valid user
	// private-chat target is available. In particular it no longer accepts a
	// group chat ID as a delivery target.
	h.sendSeriesSuggestion(0, 123)
	h.sendSeriesSuggestion(-100123, 123)
}
