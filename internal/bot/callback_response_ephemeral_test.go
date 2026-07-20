package bot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/pkg/types"
)

func callbackTestClient(t *testing.T, handler http.HandlerFunc) *services.TelegramClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := services.NewTelegramClient("test")
	// Keep the test in package bot while configuring the client through its public test hook.
	client.SetBaseURLForTest(server.URL, server.Client())
	return client
}

func TestRenderCallbackResponseGroupUsesEphemeralPlainMessage(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		payload := string(body)
		if r.URL.Path == "/sendMessage" {
			if !strings.Contains(payload, `"receiver_user_id":42`) || !strings.Contains(payload, `"callback_query_id":"cb-1"`) {
				t.Fatalf("missing ephemeral target: %s", payload)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":0,"ephemeral_message_id":7,"chat":{"id":-1001,"type":"group"},"date":1}}`)
	})

	RenderCallbackResponse("test", &callback.Context{UserID: 42, ChatID: -1001, ChatType: "group", MessageID: 9, CallbackID: "cb-1"}, &callback.Response{Text: "private", Edit: true}, client)
	if len(methods) != 1 || methods[0] != "/sendMessage" {
		t.Fatalf("methods = %#v, want only sendMessage", methods)
	}
}

func TestRenderCallbackResponseSupergroupRichUsesPrivacySafePlain(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		payload := string(body)
		if !strings.Contains(payload, `"receiver_user_id":42`) || !strings.Contains(payload, `"callback_query_id":"cb-2"`) {
			t.Fatalf("missing ephemeral target on %s: %s", r.URL.Path, payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":0,"ephemeral_message_id":8,"chat":{"id":-1001,"type":"supergroup"},"date":1}}`)
	})

	RenderCallbackResponse("test", &callback.Context{UserID: 42, ChatID: -1001, ChatType: "supergroup", MessageID: 9, CallbackID: "cb-2"}, &callback.Response{Text: "safe plain", RichMessage: "**private rich**", Edit: true}, client)
	want := []string{"/sendMessage"}
	if fmt.Sprint(methods) != fmt.Sprint(want) {
		t.Fatalf("methods = %#v, want %#v (sendRichMessage has no documented ephemeral parameters)", methods, want)
	}
}

func TestRenderCallbackResponseGroupStructuredRichNeverUsesRichOrPublicFallback(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if !strings.Contains(string(body), `"receiver_user_id":42`) {
			t.Fatalf("missing ephemeral receiver: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"ephemeral_message_id":8,"chat":{"id":-1001,"type":"group"},"date":1}}`)
	})
	rich := &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "slideshow"}}}
	RenderCallbackResponse("test", &callback.Context{UserID: 42, ChatID: -1001, ChatType: "group", CallbackID: "cb-rich"}, &callback.Response{Text: "私密搜索结果", StructuredRichMessage: rich}, client)
	if fmt.Sprint(methods) != fmt.Sprint([]string{"/sendMessage"}) {
		t.Fatalf("methods=%v; group must never call sendRichMessage or fall back publicly", methods)
	}
}

func TestRenderCallbackResponsePrivateStructuredRichFailureFallsBackOnce(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sendRichMessage" {
			_, _ = fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"unsupported"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1}}`)
	})
	rich := &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "slideshow"}}}
	RenderCallbackResponse("test", &callback.Context{UserID: 42, ChatID: 42, ChatType: "private"}, &callback.Response{Text: "文本兜底", StructuredRichMessage: rich}, client)
	if fmt.Sprint(methods) != fmt.Sprint([]string{"/sendRichMessage", "/sendMessage"}) {
		t.Fatalf("methods=%v; want one rich attempt and exactly one text fallback", methods)
	}
}

func TestCommunityChatTypes(t *testing.T) {
	for _, chatType := range []string{"group", "supergroup"} {
		if !isCommunityChat(chatType) {
			t.Errorf("%s should be community-compatible", chatType)
		}
	}
	for _, chatType := range []string{"private", "channel", ""} {
		if isCommunityChat(chatType) {
			t.Errorf("%s must not use community/ephemeral routing", chatType)
		}
	}
}
