package bot

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/pkg/types"
)

// TestRenderCallbackResponseReportsPrivateDeliveryCoordinates pins that private
// text responses report the concrete message that carried them, so handlers can
// persist an editable receipt coordinate.
func TestRenderCallbackResponseReportsPrivateDeliveryCoordinates(t *testing.T) {
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":4321,"chat":{"id":42,"type":"private"},"date":1}}`)
	})
	var delivered *types.TelegramMessage
	RenderCallbackResponse("test",
		&callback.Context{UserID: 42, ChatID: 42, ChatType: "private", MessageID: 9, CallbackID: "cb-1"},
		&callback.Response{Text: "receipt", OnDelivered: func(msg *types.TelegramMessage) { delivered = msg }},
		client)
	if delivered == nil || delivered.MessageID != 4321 || delivered.Chat == nil || delivered.Chat.ID != 42 {
		t.Fatalf("delivery coordinates not reported: %+v", delivered)
	}
}

// TestRenderCallbackResponseDoesNotReportEphemeralDelivery keeps ephemeral group
// responses out of persisted receipt coordinates; they cannot be edited later.
func TestRenderCallbackResponseDoesNotReportEphemeralDelivery(t *testing.T) {
	client := callbackTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":0,"ephemeral_message_id":7,"chat":{"id":-1001,"type":"group"},"date":1}}`)
	})
	called := false
	RenderCallbackResponse("test",
		&callback.Context{UserID: 42, ChatID: -1001, ChatType: "group", MessageID: 9, CallbackID: "cb-2"},
		&callback.Response{Text: "receipt", OnDelivered: func(*types.TelegramMessage) { called = true }},
		client)
	if called {
		t.Fatal("ephemeral delivery must not be persisted as an editable receipt")
	}
}
