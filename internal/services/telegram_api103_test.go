package services

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func TestSendMessageUsesEphemeralMessageParameters(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		payload := decodePayload(t, r)
		if _, ok := payload["receiver_user_id"]; ok {
			t.Fatalf("top-level receiver_user_id present: %#v", payload)
		}
		params := payload["ephemeral_message_parameters"].(map[string]any)
		if params["receiver_user_id"] != float64(9) || params["callback_query_id"] != "cb" {
			t.Fatalf("params=%#v", params)
		}
		if params["replace_callback_query_message"] != true {
			t.Fatalf("replace=%#v", params["replace_callback_query_message"])
		}
		writeMessageOK(t, w)
	})
	if _, err := client.SendMessage(-1001, "hi", "", nil, &types.TelegramSendOptions{ReceiverUserID: 9, CallbackQueryID: "cb", ReplaceCallbackQueryMessage: true}); err != nil {
		t.Fatal(err)
	}
}

func TestSendRichMessageCarriesEphemeralMessageParameters(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendRichMessage" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		payload := decodePayload(t, r)
		if _, ok := payload["receiver_user_id"]; ok {
			t.Fatal("top-level receiver_user_id")
		}
		params := payload["ephemeral_message_parameters"].(map[string]any)
		if params["receiver_user_id"] != float64(42) {
			t.Fatalf("params=%#v", params)
		}
		writeMessageOK(t, w)
	})
	if _, err := client.SendRichMessage(-1001, "**hi**", nil, &types.TelegramSendOptions{ReceiverUserID: 42}); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeInlineKeyboardKeepsDisabledButtons(t *testing.T) {
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: [][]types.TelegramInlineKeyboardButton{{
		{Text: "dvd imported", Disabled: types.DisabledButtonValue()},
		{Text: "ok", CallbackData: "request:id:1:type:tv:season:2"},
	}}}
	got := sanitizeInlineKeyboard(keyboard)
	if got == nil || len(got.InlineKeyboard) != 1 || len(got.InlineKeyboard[0]) != 2 {
		t.Fatalf("got=%#v", got)
	}
	disabled := got.InlineKeyboard[0][0]
	if disabled.Disabled == nil || disabled.CallbackData != "" {
		t.Fatalf("disabled=%#v", disabled)
	}
	raw, err := json.Marshal(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || !json.Valid(raw) {
		t.Fatalf("raw=%s", raw)
	}
}

func TestEditEphemeralRichMessagePayload(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editEphemeralMessageText" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		payload := decodePayload(t, r)
		rich := payload["rich_message"].(map[string]any)
		if rich["markdown"] != "**done**" {
			t.Fatalf("payload=%#v", payload)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	if err := client.EditEphemeralRichMessage(-1001, 42, 73, &types.TelegramInputRichMessage{Markdown: "**done**"}, nil); err != nil {
		t.Fatal(err)
	}
}
