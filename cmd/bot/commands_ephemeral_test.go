package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestSetMyCommandsScopesSeparatePrivateAndEphemeralGroupMenus(t *testing.T) {
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	client := services.NewTelegramClient("test")
	client.SetBaseURLForTest(server.URL, server.Client())
	setupBotCommands(client)
	if len(payloads) != 2 {
		t.Fatalf("setMyCommands calls=%d, want 2", len(payloads))
	}
	privateScope := payloads[0]["scope"].(map[string]any)
	groupScope := payloads[1]["scope"].(map[string]any)
	if privateScope["type"] != "all_private_chats" || groupScope["type"] != "all_group_chats" {
		t.Fatalf("unexpected scopes: private=%v group=%v", privateScope, groupScope)
	}
	for _, raw := range payloads[0]["commands"].([]any) {
		if _, present := raw.(map[string]any)["is_ephemeral"]; present {
			t.Fatalf("private command must not be ephemeral: %v", raw)
		}
	}
	seenEphemeral := false
	groupCommands := payloads[1]["commands"].([]any)
	if len(groupCommands) != 7 {
		t.Fatalf("group commands=%d, want privacy-safe whitelist of 7", len(groupCommands))
	}
	sensitive := map[string]bool{"link": true, "review": true, "narrate": true, "resetpw": true, "unlink": true}
	for _, raw := range groupCommands {
		cmd := raw.(map[string]any)
		name := cmd["command"].(string)
		if cmd["is_ephemeral"] != true {
			t.Fatalf("group command must be ephemeral: %v", cmd)
		}
		if sensitive[name] {
			t.Fatalf("sensitive/free-form command exposed in group scope: %s", name)
		}
		if name == "search" {
			seenEphemeral = true
		}
	}
	if !seenEphemeral {
		t.Fatal("group /search should be ephemeral")
	}
}
