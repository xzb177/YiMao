package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestUserScopedSenderGroupAsyncOutputTargetsReceiver(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":0,"ephemeral_message_id":73,"receiver_user":{"id":42,"first_name":"Miao"},"chat":{"id":-1001,"type":"supergroup"},"date":1}}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test-token")
	client.SetBaseURLForTest(server.URL, server.Client())
	sender := newUserScopedSender(client, -1001, 42)

	if _, err := sender.SendMessage("async game result", "Markdown", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	payload := <-requests
	if payload["chat_id"] != float64(-1001) || payload["receiver_user_id"] != float64(42) {
		t.Fatalf("group async output missing receiver_user_id: %#v", payload)
	}
}

func TestUserScopedSenderPrivateOutputRemainsOrdinary(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		requests <- payload
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1}}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test-token")
	client.SetBaseURLForTest(server.URL, server.Client())
	sender := newUserScopedSender(client, 42, 42)

	if _, err := sender.SendMessage("private result", "", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	payload := <-requests
	if _, exists := payload["receiver_user_id"]; exists {
		t.Fatalf("private output unexpectedly ephemeral: %#v", payload)
	}
}
