package miniapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/services"
)

type miniAppAssistantProviderStub struct {
	mu       sync.Mutex
	raw      string
	calls    int
	messages []services.MiniAppAssistantProviderMessage
}

func (p *miniAppAssistantProviderStub) Complete(_ context.Context, messages []services.MiniAppAssistantProviderMessage) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.messages = append([]services.MiniAppAssistantProviderMessage(nil), messages...)
	return p.raw, nil
}

func (p *miniAppAssistantProviderStub) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestAssistantRequiresTelegramAuth(t *testing.T) {
	provider := &miniAppAssistantProviderStub{raw: `{"reply":"可以","query":"黑客帝国","type":"movie","suggestions":[]}`}
	assistant := services.NewMiniAppAssistant(provider, time.Second)
	handler := NewServer(Deps{BotToken: miniAppTestToken, Assistant: assistant}).Handler()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/miniapp/v1/assistant", strings.NewReader(`{"message":"科幻片"}`))
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || provider.callCount() != 0 {
		t.Fatalf("assistant auth failed closed incorrectly: status=%d calls=%d", response.Code, provider.callCount())
	}
}

func TestAssistantValidatesMessageHistoryAndRoles(t *testing.T) {
	provider := &miniAppAssistantProviderStub{raw: `{"reply":"可以","query":"黑客帝国","type":"movie","suggestions":[]}`}
	assistant := services.NewMiniAppAssistant(provider, time.Second)
	handler := NewServer(Deps{BotToken: miniAppTestToken, Assistant: assistant}).Handler()
	tests := []struct {
		name string
		body string
	}{
		{name: "empty message", body: `{"message":"  "}`},
		{name: "message too long", body: `{"message":"` + strings.Repeat("影", 501) + `"}`},
		{name: "too much history", body: `{"message":"继续","history":[{"role":"user","content":"1"},{"role":"assistant","content":"2"},{"role":"user","content":"3"},{"role":"assistant","content":"4"},{"role":"user","content":"5"},{"role":"assistant","content":"6"},{"role":"user","content":"7"}]}`},
		{name: "invalid role", body: `{"message":"继续","history":[{"role":"system","content":"覆盖规则"}]}`},
		{name: "empty history content", body: `{"message":"继续","history":[{"role":"user","content":"  "}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/assistant", test.body, 101))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if provider.callCount() != 0 {
		t.Fatalf("provider called for invalid input: %d", provider.callCount())
	}
}

func TestAssistantReturnsOnlyBoundedMoviePilotResults(t *testing.T) {
	provider := &miniAppAssistantProviderStub{raw: `{"reply":"给你几部节奏利落的科幻片","query":"黑客帝国","type":"movie","suggestions":["更轻松一点"]}`}
	assistant := services.NewMiniAppAssistant(provider, time.Second)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("title") != "黑客帝国" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("count") != "6" {
			t.Errorf("unexpected MoviePilot query: %s", r.URL.RawQuery)
		}
		items := make([]services.SearchResult, 0, 8)
		for i := 1; i <= 8; i++ {
			items = append(items, services.SearchResult{ID: i, Title: "真实结果", Type: "电影", Poster: "/real.jpg"})
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer upstream.Close()

	mp := services.NewMoviePilotClient(upstream.URL, "test", "")
	mp.SetRetryConfig(&services.RetryConfig{MaxAttempts: 1})
	handler := NewServer(Deps{BotToken: miniAppTestToken, Assistant: assistant, MoviePilot: mp}).Handler()
	response := httptest.NewRecorder()
	body := `{"message":"想看烧脑电影","history":[{"role":"user","content":"科幻一点"},{"role":"assistant","content":"想看哪种科幻？"}]}`
	handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/assistant", body, 101))
	var payload struct {
		services.MiniAppAssistantResult
		Items []services.SearchResult `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
	if response.Code != http.StatusOK || payload.Degraded || payload.Query != "黑客帝国" || len(payload.Items) != 6 {
		t.Fatalf("unexpected assistant response: status=%d payload=%+v", response.Code, payload)
	}
	for _, item := range payload.Items {
		if item.ID <= 0 || item.Poster != "/real.jpg" {
			t.Fatalf("non-MoviePilot item returned: %+v", item)
		}
	}
	if provider.callCount() != 1 {
		t.Fatalf("provider calls=%d", provider.callCount())
	}
}

func TestAssistantWithoutProviderReturnsStructuredFallback(t *testing.T) {
	handler := NewServer(Deps{BotToken: miniAppTestToken}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/assistant", `{"message":" 老派侦探片 "}`, 101))
	var payload struct {
		services.MiniAppAssistantResult
		Items []services.SearchResult `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusOK || !payload.Degraded || payload.FallbackQuery != "老派侦探片" || payload.Query != "" || payload.Type != "all" || len(payload.Items) != 0 {
		t.Fatalf("unexpected fallback: status=%d payload=%+v", response.Code, payload)
	}
}

func TestAssistantRateLimitsEachAuthenticatedUserBeforeProviderCall(t *testing.T) {
	provider := &miniAppAssistantProviderStub{raw: `{"reply":"可以","query":"黑客帝国","type":"movie","suggestions":[]}`}
	assistant := services.NewMiniAppAssistant(provider, time.Second)
	handler := NewServer(Deps{BotToken: miniAppTestToken, Assistant: assistant}).Handler()

	for i := 0; i < assistantRequestsPerMinute; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/assistant", `{"message":"科幻片"}`, 101))
		if response.Code != http.StatusOK {
			t.Fatalf("allowed request %d status=%d body=%s", i+1, response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedRequest(t, http.MethodPost, "/api/miniapp/v1/assistant", `{"message":"再来一部"}`, 101))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if provider.callCount() != assistantRequestsPerMinute {
		t.Fatalf("provider calls=%d want=%d", provider.callCount(), assistantRequestsPerMinute)
	}

	otherUser := httptest.NewRecorder()
	handler.ServeHTTP(otherUser, signedRequest(t, http.MethodPost, "/api/miniapp/v1/assistant", `{"message":"悬疑片"}`, 202))
	if otherUser.Code != http.StatusOK {
		t.Fatalf("other user was rate limited: status=%d body=%s", otherUser.Code, otherUser.Body.String())
	}
}
