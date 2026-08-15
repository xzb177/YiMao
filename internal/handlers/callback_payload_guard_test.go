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
	if response == nil || !strings.Contains(response.Text, "确认提交问题") {
		t.Fatalf("unexpected response: %#v", response)
	}
	if step, _ := sess.GetString("feedback_step"); step != "confirm" {
		t.Fatalf("feedback_step=%q, want confirm", step)
	}
}

func TestGenericMediaIssueRequiresMediaTitleBeforeDescription(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	handler := NewFeedbackHandler(manager, nil, nil)
	response, err := handler.Handle(&callback.Context{UserID: 9, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"issue_type": "playback", "id": "0", "media_type": "other"}}})
	if err != nil || response == nil || !strings.Contains(response.Text, "先告诉我是哪部片") {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	sess := manager.GetOrCreate(9)
	step, _ := sess.GetString("feedback_step")
	if step != "media_title" || !handler.IsInFeedbackProcess(9) {
		t.Fatalf("step=%q", step)
	}
	quick, err := handler.Handle(&callback.Context{UserID: 9, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"quick_idx": "2"}}})
	if err != nil || quick == nil || !quick.ShowAlert || !strings.Contains(quick.CallbackMsg, "先发送片名") {
		t.Fatalf("quick=%#v err=%v", quick, err)
	}
}

func TestDetailMediaIssueKeepsDirectDescriptionFlow(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	handler := NewFeedbackHandler(manager, nil, nil)
	response, err := handler.Handle(&callback.Context{UserID: 10, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"issue_type": "playback", "id": "1425", "media_type": "tv"}}})
	if err != nil || response == nil || !strings.Contains(response.Text, "快捷选择") {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	step, _ := manager.GetOrCreate(10).GetString("feedback_step")
	if step != "description" {
		t.Fatalf("step=%q", step)
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
