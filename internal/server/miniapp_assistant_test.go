package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
)

func TestMiniAppAssistantFromConfigHonorsFeatureFlag(t *testing.T) {
	disabled := newMiniAppAssistant(&config.Config{EnableAI: false, OpenAIAPIKey: "configured-but-disabled"})
	result := disabled.Assist(context.Background(), services.MiniAppAssistantInput{Message: "测试"})
	if !result.Degraded || result.FallbackQuery != "测试" {
		t.Fatalf("disabled AI did not degrade: %+v", result)
	}
}

func TestMiniAppAssistantFromConfigUsesOpenAICompatibleSettings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/compatible/chat/completions" || body.Model != "configured-model" || r.Header.Get("Authorization") != "Bearer configured-key" {
			t.Errorf("production assistant ignored config: path=%q model=%q", r.URL.Path, body.Model)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"reply":"可以","query":"测试","type":"all","suggestions":[]}`}}}})
	}))
	defer upstream.Close()

	assistant := newMiniAppAssistant(&config.Config{
		EnableAI:      true,
		OpenAIAPIKey:  "configured-key",
		OpenAIBaseURL: upstream.URL + "/compatible",
		OpenAIModel:   "configured-model",
	})
	result := assistant.Assist(context.Background(), services.MiniAppAssistantInput{Message: "测试"})
	if result.Degraded || result.Query != "测试" {
		t.Fatalf("configured assistant failed: %+v", result)
	}
}
