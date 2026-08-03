package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestMediaOmitsInvalidSavePath(t *testing.T) {
	for _, savePath := range []string{"", " ", "0", "1", "relative/path"} {
		t.Run(savePath, func(t *testing.T) {
			var payload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/subscribe" {
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatalf("decode payload: %v", err)
					}
					_, _ = w.Write([]byte(`{"success":true,"data":{"id":42}}`))
					return
				}
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			defer server.Close()

			client := NewMoviePilotClient(server.URL, "unused", savePath)
			if _, err := client.RequestMedia("Test", 2026, 123, MediaTypeMovie, 0); err != nil {
				t.Fatalf("RequestMedia: %v", err)
			}
			if got, exists := payload["save_path"]; exists {
				t.Fatalf("save_path = %v, want omitted", got)
			}
		})
	}
}

func TestRequestMediaIncludesAbsoluteSavePath(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/subscribe" {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":42}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := NewMoviePilotClient(server.URL, "unused", " /downloads/yimao ")
	if _, err := client.RequestMedia("Test", 2026, 123, MediaTypeMovie, 0); err != nil {
		t.Fatalf("RequestMedia: %v", err)
	}
	if got := payload["save_path"]; got != "/downloads/yimao" {
		t.Fatalf("save_path = %v, want /downloads/yimao", got)
	}
}
