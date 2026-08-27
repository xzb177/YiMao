package services

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

func TestSendPhotoURLCarriesEphemeralOptions(t *testing.T) {
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendPhoto" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		payload := decodePayload(t, r)
		params := payload["ephemeral_message_parameters"].(map[string]any)
		if params["receiver_user_id"] != float64(42) || params["callback_query_id"] != "cb" || payload["message_thread_id"] != float64(88) {
			t.Fatalf("missing ephemeral options: %#v", payload)
		}
		if _, ok := payload["receiver_user_id"]; ok {
			t.Fatal("top-level receiver_user_id must be omitted")
		}
		writeMessageOK(t, w)
	})
	_, err := client.SendPhotoByURLWithParseMode(-1001, "https://example.com/p.jpg", "poster", "HTML", nil, &types.TelegramSendOptions{ReceiverUserID: 42, CallbackQueryID: "cb", MessageThreadID: 88})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMultipartEphemeralOptions(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Fatal(err)
		}
		got = map[string]string{}
		got["message_thread_id"] = r.FormValue("message_thread_id")
		got["ephemeral_message_parameters"] = r.FormValue("ephemeral_message_parameters")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":0,"ephemeral_message_id":73,"receiver_user":{"id":42,"first_name":"Miao"},"chat":{"id":-1001,"type":"supergroup"},"date":1}}`)
	}))
	defer server.Close()
	client := NewTelegramClient("test")
	client.SetBaseURLForTest(server.URL, server.Client())
	client.imageCache = nil
	// Serve a tiny payload from the same server's dedicated path.
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("fake-image")) }))
	defer imageServer.Close()
	_, err := client.SendPhotoFromURLWithParseMode(-1001, imageServer.URL, "poster", "", nil, nil, &types.TelegramSendOptions{ReceiverUserID: 42, CallbackQueryID: "cb", MessageThreadID: 88})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{}
	if err := json.Unmarshal([]byte(got["ephemeral_message_parameters"]), &params); err != nil {
		t.Fatalf("ephemeral json: %v raw=%q", err, got["ephemeral_message_parameters"])
	}
	if got["message_thread_id"] != "88" || params["receiver_user_id"] != float64(42) || params["callback_query_id"] != "cb" {
		t.Fatalf("fields=%v params=%v", got, params)
	}
}

func TestEditEphemeralMediaAndCaptionReturnBoolean(t *testing.T) {
	var methods []string
	client := newTelegramTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, strings.TrimPrefix(r.URL.Path, "/"))
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["receiver_user_id"] != float64(42) || payload["ephemeral_message_id"] != float64(73) {
			t.Fatalf("payload=%v", payload)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	})
	if err := client.EditEphemeralMessageMedia(-1001, 42, 73, map[string]interface{}{"type": "photo", "media": "file-id"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.EditEphemeralMessageCaption(-1001, 42, 73, "caption", "HTML", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "editEphemeralMessageMedia,editEphemeralMessageCaption" {
		t.Fatalf("methods=%v", methods)
	}
}

var _ *multipart.Writer
