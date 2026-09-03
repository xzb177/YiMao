package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/session"
)

func TestWashHandlerRejectsTVSeasonOnlyBeforeAnyLookup(t *testing.T) {
	h := NewWashHandler(nil, nil, nil, nil, session.NewManager(time.Hour, 64))
	resp, err := h.Handle(&callback.Context{UserID: 7, Callback: &callback.Callback{Action: "wash", Params: map[string]string{"id": "42", "type": "tv", "season": "1", "confirm": "1"}}})
	if err != nil || resp == nil || !strings.Contains(resp.CallbackMsg, "仅限单集") {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}

func TestWashHandlerRejectsMovieEpisodeScope(t *testing.T) {
	h := NewWashHandler(nil, nil, nil, nil, session.NewManager(time.Hour, 64))
	resp, err := h.Handle(&callback.Context{UserID: 7, Callback: &callback.Callback{Action: "wash", Params: map[string]string{"id": "42", "type": "movie", "episode": "1"}}})
	if err != nil || resp == nil || !strings.Contains(resp.CallbackMsg, "范围无效") {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}
