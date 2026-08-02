package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type miniAppAssistantProviderFunc func(context.Context, []MiniAppAssistantProviderMessage) (string, error)

func (f miniAppAssistantProviderFunc) Complete(ctx context.Context, messages []MiniAppAssistantProviderMessage) (string, error) {
	return f(ctx, messages)
}

func TestMiniAppAssistantCleansAndBoundsProviderJSON(t *testing.T) {
	longReply := strings.Repeat("回", 700)
	longSuggestion := strings.Repeat("荐", 120)
	provider := miniAppAssistantProviderFunc(func(_ context.Context, messages []MiniAppAssistantProviderMessage) (string, error) {
		if len(messages) != 4 || messages[0].Role != "system" || messages[1].Role != "user" || messages[2].Role != "assistant" || messages[3].Content != "想看烧脑电影" {
			t.Fatalf("unexpected provider messages: %+v", messages)
		}
		if !strings.Contains(messages[0].Content, "不得执行") || !strings.Contains(messages[0].Content, "只返回 JSON") {
			t.Fatalf("system guard missing: %q", messages[0].Content)
		}
		return fmt.Sprintf("模型前言\n```json\n{\"reply\":%q,\"query\":\"  黑客帝国  \",\"type\":\"MOVIE\",\"suggestions\":[\"  1999 年的  \",\"\",%q,\"续集\",\"动画版\",\"多余\"],\"tmdb_id\":550,\"poster\":\"fake\"}\n```\n模型尾注", "  "+longReply+"  ", longSuggestion), nil
	})
	service := NewMiniAppAssistant(provider, time.Second)

	result := service.Assist(context.Background(), MiniAppAssistantInput{
		Message: "  想看烧脑电影  ",
		History: []MiniAppAssistantMessage{
			{Role: "user", Content: "科幻一点"},
			{Role: "assistant", Content: "你更喜欢哪种科幻？"},
		},
	})

	if result.Degraded || result.Query != "黑客帝国" || result.Type != "movie" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if utf8.RuneCountInString(result.Reply) != miniAppAssistantMaxReplyRunes {
		t.Fatalf("reply runes=%d want %d", utf8.RuneCountInString(result.Reply), miniAppAssistantMaxReplyRunes)
	}
	if len(result.Suggestions) != miniAppAssistantMaxSuggestions || result.Suggestions[0] != "1999 年的" {
		t.Fatalf("suggestions were not cleaned and bounded: %#v", result.Suggestions)
	}
	for _, suggestion := range result.Suggestions {
		if utf8.RuneCountInString(suggestion) > miniAppAssistantMaxSuggestionRunes {
			t.Fatalf("suggestion exceeded rune boundary: %d", utf8.RuneCountInString(suggestion))
		}
	}
}

func TestMiniAppAssistantRejectsInvalidStructuredOutput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "not json", raw: "我建议你看看科幻片"},
		{name: "missing required field", raw: `{"reply":"可以","query":"星际穿越","type":"movie"}`},
		{name: "invalid type", raw: `{"reply":"可以","query":"星际穿越","type":"documentary","suggestions":[]}`},
		{name: "empty query", raw: `{"reply":"可以","query":"  ","type":"movie","suggestions":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewMiniAppAssistant(miniAppAssistantProviderFunc(func(context.Context, []MiniAppAssistantProviderMessage) (string, error) {
				return test.raw, nil
			}), time.Second)
			result := service.Assist(context.Background(), MiniAppAssistantInput{Message: "  星际穿越  "})
			if !result.Degraded || result.FallbackQuery != "星际穿越" || result.Query != "" || result.Type != "all" || result.Reply == "" {
				t.Fatalf("invalid output did not degrade safely: %+v", result)
			}
		})
	}
}

func TestMiniAppAssistantProviderFailureDegradesWithoutErrorDetails(t *testing.T) {
	service := NewMiniAppAssistant(miniAppAssistantProviderFunc(func(context.Context, []MiniAppAssistantProviderMessage) (string, error) {
		return "", errors.New("secret upstream detail")
	}), time.Second)
	result := service.Assist(context.Background(), MiniAppAssistantInput{Message: "  老派侦探片  "})
	if !result.Degraded || result.FallbackQuery != "老派侦探片" || strings.Contains(result.Reply, "secret") {
		t.Fatalf("unsafe degraded result: %+v", result)
	}
}

func TestMiniAppAssistantPropagatesCallerCancellation(t *testing.T) {
	providerCancelled := make(chan struct{})
	service := NewMiniAppAssistant(miniAppAssistantProviderFunc(func(ctx context.Context, _ []MiniAppAssistantProviderMessage) (string, error) {
		<-ctx.Done()
		close(providerCancelled)
		return "", ctx.Err()
	}), time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan MiniAppAssistantResult, 1)
	go func() { done <- service.Assist(ctx, MiniAppAssistantInput{Message: "测试"}) }()
	cancel()
	select {
	case <-providerCancelled:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not reach provider")
	}
	select {
	case result := <-done:
		if !result.Degraded || result.FallbackQuery != "测试" {
			t.Fatalf("unexpected cancellation fallback: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("assistant did not return after cancellation")
	}
}

func TestMiniAppAssistantAppliesProviderBudget(t *testing.T) {
	const budget = 25 * time.Millisecond
	service := NewMiniAppAssistant(miniAppAssistantProviderFunc(func(ctx context.Context, _ []MiniAppAssistantProviderMessage) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > budget+50*time.Millisecond {
			t.Errorf("provider deadline missing or too long: ok=%v remaining=%s", ok, time.Until(deadline))
		}
		<-ctx.Done()
		return "", ctx.Err()
	}), budget)
	started := time.Now()
	result := service.Assist(context.Background(), MiniAppAssistantInput{Message: "测试超时"})
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("assistant exceeded budget: %s", elapsed)
	}
	if !result.Degraded || result.FallbackQuery != "测试超时" {
		t.Fatalf("unexpected timeout fallback: %+v", result)
	}
}

func TestOpenAICompatibleMiniAppAssistantProviderUsesContextAndConfiguredModel(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected upstream request: path=%q", r.URL.Path)
		}
		var body struct {
			Model    string                            `json:"model"`
			Messages []MiniAppAssistantProviderMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "test-model" || len(body.Messages) != 1 || body.Messages[0].Content != "测试" {
			t.Errorf("unexpected request body: %+v", body)
		}
		close(requestStarted)
		<-r.Context().Done()
		close(requestCancelled)
	}))
	defer upstream.Close()

	provider := NewOpenAICompatibleMiniAppAssistantProvider(upstream.Client(), upstream.URL+"/v1/", "test-key", "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.Complete(ctx, []MiniAppAssistantProviderMessage{{Role: "user", Content: "测试"}})
		done <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("provider did not start its HTTP request")
	}
	cancel()

	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("provider did not cancel its HTTP request")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled provider call returned nil error")
		}
	case <-time.After(time.Second):
		t.Fatal("provider did not return after cancellation")
	}
}
