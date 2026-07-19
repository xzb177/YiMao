package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/session"
)

func TestFeedbackQuickCallbacksStayCompact(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	handler := NewFeedbackHandler(manager, nil, nil)
	ctx := &callback.Context{
		UserID: 42,
		Callback: &callback.Callback{
			Action: callback.ActionFeedback,
			Params: map[string]string{
				"issue_type": "quality",
				"id":         "123456",
				"media_type": "movie",
			},
		},
	}

	response, err := handler.handleTypeSelect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if response.Keyboard == nil {
		t.Fatal("missing quick-option keyboard")
	}

	parser := callback.NewParser()
	var quickButtons int
	for _, row := range response.Keyboard.InlineKeyboard {
		for _, button := range row {
			if len([]byte(button.CallbackData)) > 64 {
				t.Errorf("callback %q is %d bytes", button.CallbackData, len([]byte(button.CallbackData)))
			}
			if strings.HasPrefix(button.CallbackData, "feedback:quick_idx:") {
				quickButtons++
				parsed, parseErr := parser.Parse(button.CallbackData)
				if parseErr != nil {
					t.Errorf("parse %q: %v", button.CallbackData, parseErr)
				} else if parsed.Params["quick_idx"] == "" {
					t.Errorf("missing quick_idx in %q", button.CallbackData)
				}
			}
		}
	}
	if quickButtons != len(getTypeInfo("quality").quickOptions) {
		t.Fatalf("quick buttons = %d", quickButtons)
	}
}

func TestFeedbackQuickIndexRecoversSessionContext(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	handler := NewFeedbackHandler(manager, nil, nil)
	sess := manager.GetOrCreate(7)
	sess.Set("feedback_tmdb_id", "88")
	sess.Set("feedback_media_type", "movie")
	sess.Set("feedback_media_title", "")
	sess.Set("feedback_issue_type", "quality")
	sess.Set("feedback_step", "description")

	response, err := handler.Handle(&callback.Context{
		UserID: 7,
		Callback: &callback.Callback{
			Action: callback.ActionFeedback,
			Params: map[string]string{"quick_idx": "0"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.CallbackMsg != "功能暂不可用" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestCompactNarrationCallbackParses(t *testing.T) {
	ref := callback.ShortRef("一部非常非常长的电影名称")
	data := "game_narrate:ref:" + ref + ":spoiler:1"
	if len(data) > 64 {
		t.Fatalf("narration callback is %d bytes", len(data))
	}
	parsed, err := callback.NewParser().Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Action != "game_narrate" || parsed.Params["ref"] != ref || parsed.Params["spoiler"] != "1" {
		t.Fatalf("unexpected callback: %#v", parsed)
	}
}

func TestDailyChallengeUsesSessionBackedCallback(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	handler := &GameHandler{sessionMgr: manager}
	response, err := handler.handleDailyChallenge(&callback.Context{
		UserID:   99,
		Callback: &callback.Callback{Action: "game_daily_challenge", Params: map[string]string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Keyboard == nil || len(response.Keyboard.InlineKeyboard) == 0 {
		t.Fatal("missing daily-challenge keyboard")
	}

	button := response.Keyboard.InlineKeyboard[0][0]
	if len(button.CallbackData) > 64 {
		t.Fatalf("daily challenge callback is %d bytes", len(button.CallbackData))
	}
	parsed, err := callback.NewParser().Parse(button.CallbackData)
	if err != nil {
		t.Fatal(err)
	}
	ref := parsed.Params["ref"]
	if parsed.Action != "adventure_start" || ref == "" {
		t.Fatalf("unexpected callback: %#v", parsed)
	}
	if title, ok := manager.GetOrCreate(99).GetString("adventure_movie_" + ref); !ok || title == "" {
		t.Fatalf("missing session-backed challenge title for ref %q", ref)
	}
}
