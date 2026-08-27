package services

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func newTelegramTestClient(t *testing.T, handler http.HandlerFunc) *TelegramClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewTelegramClient("test-token")
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client
}

func decodePayload(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	defer r.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request payload: %v", err)
	}
	return payload
}

func writeMessageOK(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":0,"ephemeral_message_id":73,"receiver_user":{"id":42,"first_name":"Miao"},"chat":{"id":-1001,"type":"supergroup"},"date":1}}`))
}

func TestBotCommandIsEphemeralPayload(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/setMyCommands" {
			t.Fatalf("path = %q, want /setMyCommands", r.URL.Path)
		}
		payload := decodePayload(t, r)
		commands := payload["commands"].([]any)
		command := commands[0].(map[string]any)
		if command["is_ephemeral"] != true {
			t.Fatalf("is_ephemeral = %#v, want true", command["is_ephemeral"])
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})

	if err := client.SetMyCommands([]BotCommand{{Command: "menu", Description: "menu", IsEphemeral: true}}, ""); err != nil {
		t.Fatalf("SetMyCommands: %v", err)
	}
}

func TestSendMessageEphemeralPayloadAndResponseCompatibility(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Fatalf("path = %q, want /sendMessage", r.URL.Path)
		}
		payload := decodePayload(t, r)
		params := payload["ephemeral_message_parameters"].(map[string]any)
		if params["receiver_user_id"] != float64(42) {
			t.Fatalf("receiver_user_id = %#v", params["receiver_user_id"])
		}
		if params["callback_query_id"] != "callback-1" {
			t.Fatalf("callback_query_id = %#v", params["callback_query_id"])
		}
		if _, ok := payload["receiver_user_id"]; ok {
			t.Fatalf("top-level receiver_user_id must be omitted: %#v", payload)
		}
		if _, ok := payload["callback_query_id"]; ok {
			t.Fatalf("top-level callback_query_id must be omitted: %#v", payload)
		}
		if _, ok := payload["reply_parameters"]; ok {
			t.Fatal("reply_parameters should be omitted when unset")
		}
		writeMessageOK(t, w)
	})

	msg, err := client.SendMessage(-1001, "only you", "", nil, &types.TelegramSendOptions{
		ReceiverUserID:  42,
		CallbackQueryID: "callback-1",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msg.MessageID != 0 || msg.EphemeralMessageID != 73 || msg.ReceiverUser == nil || msg.ReceiverUser.ID != 42 {
		t.Fatalf("unexpected ephemeral response: %#v", msg)
	}
}

func TestSendMessageEphemeralReplyPayload(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Fatalf("path = %q, want /sendMessage", r.URL.Path)
		}
		payload := decodePayload(t, r)
		params := payload["ephemeral_message_parameters"].(map[string]any)
		if params["receiver_user_id"] != float64(42) || params["callback_query_id"] != "callback-2" {
			t.Fatalf("missing ephemeral targeting fields: %#v", payload)
		}
		if _, ok := payload["receiver_user_id"]; ok {
			t.Fatal("top-level receiver_user_id must be omitted")
		}
		reply := payload["reply_parameters"].(map[string]any)
		if reply["ephemeral_message_id"] != float64(19) {
			t.Fatalf("ephemeral_message_id = %#v", reply["ephemeral_message_id"])
		}
		if _, ok := reply["message_id"]; ok {
			t.Fatal("message_id should be omitted for an ephemeral reply")
		}
		writeMessageOK(t, w)
	})

	_, err := client.SendMessage(-1001, "private", "", nil, &types.TelegramSendOptions{
		ReceiverUserID:  42,
		CallbackQueryID: "callback-2",
		ReplyParameters: &types.TelegramReplyParameters{EphemeralMessageID: 19},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
}

func TestEditEphemeralMessageTextPayload(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editEphemeralMessageText" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		payload := decodePayload(t, r)
		if payload["chat_id"] != float64(-1001) || payload["receiver_user_id"] != float64(42) || payload["ephemeral_message_id"] != float64(73) || payload["text"] != "done" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	if err := client.EditEphemeralMessageText(-1001, 42, 73, "done", "HTML", nil); err != nil {
		t.Fatalf("EditEphemeralMessageText: %v", err)
	}
}

func TestSendMessageCarriesForumThread(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		payload := decodePayload(t, r)
		if payload["message_thread_id"] != float64(88) {
			t.Fatalf("message_thread_id = %#v", payload["message_thread_id"])
		}
		writeMessageOK(t, w)
	})
	if _, err := client.SendMessage(-1001, "topic", "", nil, &types.TelegramSendOptions{ReceiverUserID: 42, MessageThreadID: 88}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
}

func TestDeleteEphemeralMessagePayload(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deleteEphemeralMessage" {
			t.Fatalf("path = %q, want /deleteEphemeralMessage", r.URL.Path)
		}
		payload := decodePayload(t, r)
		if payload["chat_id"] != float64(-1001) || payload["receiver_user_id"] != float64(42) || payload["ephemeral_message_id"] != float64(73) {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})

	if err := client.DeleteEphemeralMessage(-1001, 42, 73); err != nil {
		t.Fatalf("DeleteEphemeralMessage: %v", err)
	}
}

func TestEphemeralAPIErrorsRemainTypedForFallback(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: callback query is too old"}`))
	})

	_, err := client.SendMessage(-1001, "late", "", nil, &types.TelegramSendOptions{ReceiverUserID: 42, CallbackQueryID: "expired"})
	var telegramErr *types.TelegramError
	if !errors.As(err, &telegramErr) {
		t.Fatalf("error type = %T, want *types.TelegramError: %v", err, err)
	}
	if telegramErr.Code != http.StatusBadRequest || !strings.Contains(telegramErr.Message, "too old") {
		t.Fatalf("unexpected Telegram error: %#v", telegramErr)
	}
}
