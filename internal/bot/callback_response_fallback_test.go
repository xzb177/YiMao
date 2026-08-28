package bot

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/pkg/types"
)

func TestRichEditAfterKeyboardFusionOmitsReplyMarkup(t *testing.T) {
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if r.URL.Path == "/editMessageText" && strings.Contains(string(body), `"reply_markup"`) {
			t.Fatalf("rich edit payload has malformed reply_markup: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":9,"chat":{"id":42,"type":"private"},"date":1}}`)
	})
	RenderCallbackResponse("test", &callback.Context{ChatID: 42, ChatType: "private", MessageID: 9}, &callback.Response{
		Edit: true, StructuredRichMessage: &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "text"}}},
		Keyboard: &callback.Keyboard{RemoveKeyboard: true, InlineKeyboard: [][]callback.Button{{{Text: "查看进度", CallbackData: "requests"}}}},
	}, client)
}

func TestRichEditFailureAndDeleteFailureDoesNotSendDuplicate(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"failure"}`)
	})
	RenderCallbackResponse("test", &callback.Context{ChatID: 42, ChatType: "private", MessageID: 9}, &callback.Response{
		Edit: true, StructuredRichMessage: &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "text"}}},
	}, client)
	if got := strings.Join(methods, ","); got != "/editMessageText,/deleteMessage" {
		t.Fatalf("methods=%v; must not send after delete failure", methods)
	}
}

func TestRichEditFailureDeleteSuccessSendsExactlyOnce(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/editMessageText" && len(methods) == 1 {
			_, _ = fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"rich failure"}`)
			return
		}
		if r.URL.Path == "/deleteMessage" {
			_, _ = fmt.Fprint(w, `{"ok":true,"result":true}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":10,"chat":{"id":42,"type":"private"},"date":1}}`)
	})
	RenderCallbackResponse("test", &callback.Context{ChatID: 42, ChatType: "private", MessageID: 9}, &callback.Response{
		Edit: true, StructuredRichMessage: &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "text"}}},
	}, client)
	if got := strings.Join(methods, ","); got != "/editMessageText,/deleteMessage,/sendRichMessage" {
		t.Fatalf("methods=%v; want one replacement after confirmed deletion", methods)
	}
}

func TestRichEditWithoutMessageIDNeverDeletes(t *testing.T) {
	var methods []string
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":10,"chat":{"id":42,"type":"private"},"date":1}}`)
	})
	RenderCallbackResponse("test", &callback.Context{ChatID: 42, ChatType: "private"}, &callback.Response{
		Edit: true, StructuredRichMessage: &types.TelegramInputRichMessage{Blocks: []types.TelegramInputRichBlock{{Type: "text"}}},
	}, client)
	for _, method := range methods {
		if method == "/deleteMessage" {
			t.Fatalf("message_id=0 must not call delete: %v", methods)
		}
	}
}
