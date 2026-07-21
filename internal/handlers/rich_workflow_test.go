package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/types"
)

func TestUserScopedSenderPrivateRichMessageUsesNativeAPI(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payload["_path"] = r.URL.Path
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":88,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test-token")
	client.SetBaseURLForTest(server.URL, server.Client())
	sender := newUserScopedSender(client, 42, 42)

	if _, err := sender.SendRichMessage("## 富文本标题\n\n**正文**", nil); err != nil {
		t.Fatalf("SendRichMessage: %v", err)
	}
	payload := <-requests
	if payload["_path"] != "/sendRichMessage" {
		t.Fatalf("path=%v, want /sendRichMessage", payload["_path"])
	}
	rich, ok := payload["rich_message"].(map[string]any)
	if !ok || rich["markdown"] != "## 富文本标题\n\n**正文**" {
		t.Fatalf("rich_message=%v", payload["rich_message"])
	}
}

func TestUserScopedSenderPrivateStructuredRichMessageUsesNativeAPI(t *testing.T) {
	requests := make(chan struct {
		path        string
		contentType string
		body        []byte
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		requests <- struct {
			path        string
			contentType string
			body        []byte
		}{r.URL.Path, r.Header.Get("Content-Type"), body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":89,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test-token")
	client.SetBaseURLForTest(server.URL, server.Client())
	sender := newUserScopedSender(client, 42, 42)
	rich := &types.TelegramInputRichMessage{
		Blocks: []types.TelegramInputRichBlock{{Type: "slideshow", Blocks: []types.TelegramInputRichBlock{{Type: "photo", Photo: &types.TelegramRichPhoto{Type: "photo", Media: "attach://scene"}}}}},
		Media:  []types.TelegramInputRichMessageMedia{{ID: "scene", Media: types.TelegramRichPhoto{Type: "photo", Media: "attach://scene"}, Upload: []byte("jpeg"), Filename: "scene.jpg"}},
	}
	if _, err := sender.SendStructuredRichMessage(rich, "当前关卡", nil); err != nil {
		t.Fatalf("SendStructuredRichMessage: %v", err)
	}
	got := <-requests
	if got.path != "/sendRichMessage" || !strings.HasPrefix(got.contentType, "multipart/form-data;") {
		t.Fatalf("request=%s %s", got.path, got.contentType)
	}
	if !bytes.Contains(got.body, []byte("slideshow")) || !bytes.Contains(got.body, []byte("attach://scene")) {
		t.Fatalf("multipart body misses slideshow attachment: %q", got.body)
	}
}

func TestUserScopedSenderGroupStructuredRichMessageFailsClosedToEphemeralText(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		payload["_path"] = r.URL.Path
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"ephemeral_message_id":90,"chat":{"id":-100,"type":"supergroup"}}}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test-token")
	client.SetBaseURLForTest(server.URL, server.Client())
	sender := newUserScopedSender(client, -100, 42)
	rich := &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "slideshow"}}}
	if _, err := sender.SendStructuredRichMessage(rich, "当前关卡", nil); err != nil {
		t.Fatal(err)
	}
	got := <-requests
	if got["_path"] != "/sendMessage" || got["receiver_user_id"] != float64(42) {
		t.Fatalf("group structured fallback leaked publicly: %+v", got)
	}
}

func TestNarratorEntryDuplicateTapDoesNotSendSecondCard(t *testing.T) {
	h := &GameHandler{sessionMgr: session.NewManager(time.Hour, 10)}
	ctx := &callback.Context{UserID: 42}

	first, err := h.handleNarratorEntry(ctx)
	if err != nil || first.RichMessage == "" {
		t.Fatalf("first response=%+v err=%v, want rich card", first, err)
	}
	second, err := h.handleNarratorEntry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.RichMessage != "" || second.Text != "" || second.CallbackMsg == "" {
		t.Fatalf("second response=%+v, want callback acknowledgement only", second)
	}
}
