package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/session"
)

func TestWashEntryUsesNaturalLanguageAndStartsIntent(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	h := NewWashHandler(nil, nil, nil, nil, manager)
	resp, err := h.Handle(&callback.Context{UserID: 42, Callback: &callback.Callback{Action: "wash", Params: map[string]string{}}})
	if err != nil || resp == nil {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	if !strings.Contains(resp.Text, "直接把片名发给我") || strings.Contains(resp.Text, "严格的 TMDB") {
		t.Fatalf("unexpected wash copy: %q", resp.Text)
	}
	intent, _ := manager.GetOrCreate(42).GetString("media_search_intent")
	if intent != "wash" {
		t.Fatalf("intent=%q", intent)
	}
}

func TestCancelClearsWorkflowState(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	sess := manager.GetOrCreate(42)
	for _, key := range []string{"media_search_intent", "feedback_step", "feedback_tmdb_id", "feedback_media_type", "feedback_media_title", "feedback_issue_type", "feedback_require_media"} {
		sess.Set(key, "stale")
	}
	resp, err := NewCancelHandler(manager).Handle(&callback.Context{UserID: 42})
	if err != nil || resp == nil || !strings.Contains(resp.Text, "还没有提交") {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	for _, key := range []string{"media_search_intent", "feedback_step", "feedback_tmdb_id", "feedback_media_type", "feedback_media_title", "feedback_issue_type", "feedback_require_media"} {
		if _, ok := sess.Get(key); ok {
			t.Fatalf("state %q not cleared", key)
		}
	}
}

func TestIssueEntrySeparatesMediaAndGeneralProblems(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	h := NewFeedbackHandler(manager, nil, nil)
	resp, err := h.Handle(&callback.Context{UserID: 42, Callback: &callback.Callback{Action: "issue", Params: map[string]string{}}})
	if err != nil || resp == nil {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	if !strings.Contains(resp.Text, "影视内容问题") || !strings.Contains(resp.Text, "使用问题或建议") {
		t.Fatalf("unexpected issue entry: %q", resp.Text)
	}
}

func TestMediaScopeRequiresTitleEvenForOtherMediaProblem(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	h := NewFeedbackHandler(manager, nil, nil)
	_, err := h.Handle(&callback.Context{UserID: 43, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"scope": "media"}}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.Handle(&callback.Context{UserID: 43, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"issue_type": "other", "id": "0", "media_type": "other"}}})
	if err != nil || resp == nil || !strings.Contains(resp.Text, "先告诉我是哪部片") {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}
