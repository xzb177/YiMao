package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

func newFeedbackTestTelegram(t *testing.T) *services.TelegramClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1, "chat": map[string]any{"id": 42, "type": "private"}, "date": 1}})
	}))
	t.Cleanup(server.Close)
	client := services.NewTelegramClient("test")
	client.SetBaseURLForTest(server.URL, server.Client())
	return client
}

func seedFeedbackDescriptionSession(manager *session.Manager, userID int64) {
	sess := manager.GetOrCreate(userID)
	sess.Set("feedback_step", "description")
	sess.Set("feedback_tmdb_id", "123")
	sess.Set("feedback_media_type", "movie")
	sess.Set("feedback_media_title", "测试片")
	sess.Set("feedback_issue_type", "playback")
}

func TestFeedbackTextAndPhotoAreDraftedUntilConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		photo string
	}{
		{name: "text"},
		{name: "photo", photo: "photo-file-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := session.NewManager(time.Hour, 10)
			service := services.NewIssueService(t.TempDir())
			h := NewFeedbackHandler(manager, newFeedbackTestTelegram(t), nil)
			h.SetIssueService(service)
			seedFeedbackDescriptionSession(manager, 42)

			if err := h.HandleFeedbackWithPhoto(42, 42, "无法播放", tc.photo); err != nil {
				t.Fatal(err)
			}
			if got := service.GetUserIssues(42); len(got) != 0 {
				t.Fatalf("issue created before confirmation: %#v", got)
			}
			if step, _ := manager.GetOrCreate(42).GetString("feedback_step"); step != "confirm" {
				t.Fatalf("feedback_step=%q, want confirm", step)
			}
		})
	}
}

func TestFeedbackQuickOptionConfirmationIsIdempotent(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	service := services.NewIssueService(t.TempDir())
	h := NewFeedbackHandler(manager, nil, nil)
	h.SetIssueService(service)
	seedFeedbackDescriptionSession(manager, 42)

	quickCtx := &callback.Context{UserID: 42, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"quick_idx": "0"}}}
	resp, err := h.Handle(quickCtx)
	if err != nil || resp == nil || !strings.Contains(resp.Text, "确认提交") {
		t.Fatalf("quick response=%#v err=%v", resp, err)
	}
	if got := service.GetUserIssues(42); len(got) != 0 {
		t.Fatalf("issue created before confirmation: %#v", got)
	}

	confirmCtx := &callback.Context{UserID: 42, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"confirm": "1"}}}
	resp, err = h.Handle(confirmCtx)
	if err != nil || resp == nil || !strings.Contains(resp.Text, "反馈已提交") {
		t.Fatalf("confirm response=%#v err=%v", resp, err)
	}
	resp, err = h.Handle(confirmCtx)
	if err != nil || resp == nil {
		t.Fatalf("duplicate response=%#v err=%v", resp, err)
	}
	if got := service.GetUserIssues(42); len(got) != 1 {
		t.Fatalf("duplicate confirmation created %d issues", len(got))
	}
}

func TestFeedbackConfirmationFailureKeepsDraftForRetry(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	service := services.NewIssueService(filepath.Join(t.TempDir(), "missing"))
	h := NewFeedbackHandler(manager, nil, nil)
	h.SetIssueService(service)
	seedFeedbackDescriptionSession(manager, 42)
	sess := manager.GetOrCreate(42)
	sess.Set("feedback_draft_description", "无法播放")
	sess.Set("feedback_draft_photo_file_id", "")
	sess.Set("feedback_step", "confirm")

	resp, err := h.Handle(&callback.Context{UserID: 42, Callback: &callback.Callback{Action: callback.ActionFeedback, Params: map[string]string{"confirm": "1"}}})
	if err != nil || resp == nil || !resp.ShowAlert {
		t.Fatalf("response=%#v err=%v", resp, err)
	}
	if step, _ := sess.GetString("feedback_step"); step != "confirm" {
		t.Fatalf("failed persistence cleared draft step: %q", step)
	}
	if description, _ := sess.GetString("feedback_draft_description"); description != "无法播放" {
		t.Fatalf("failed persistence cleared description: %q", description)
	}
}
