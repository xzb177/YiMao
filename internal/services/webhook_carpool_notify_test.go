package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeTelegramServer 模拟 Bot API：sendMessage 记下发出的文本，
// getChatMember 全部返回 member（可达）。
type fakeTelegramServer struct {
	mu       sync.Mutex
	messages []string
}

func (f *fakeTelegramServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.messages = append(f.messages, body.Text)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getChatMember"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"member"}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		}
	})
}

func newCarpoolNotifyTestService(t *testing.T, fake *fakeTelegramServer) *WebhookService {
	t.Helper()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	tg := NewTelegramClient("test-token")
	tg.SetBaseURLForTest(server.URL, server.Client())
	return &WebhookService{
		telegram: tg,
		chatID:   -1001234567890,
		carpool:  NewCarpoolService(t.TempDir()),
	}
}

// TestNotifyCarpoolMembersSeasonScoped S1 到货的端到端语义：
// 只通知 S1 + 任意季成员并结清，S2 成员保留；公告文案带「第1季」。
func TestNotifyCarpoolMembersSeasonScoped(t *testing.T) {
	fake := &fakeTelegramServer{}
	s := newCarpoolNotifyTestService(t, fake)
	s.carpool.Add(100, "tv", 1, 111)
	s.carpool.Add(100, "tv", 2, 222)
	s.carpool.Add(100, "tv", 0, 333)

	s.notifyCarpoolMembers("100", "tv", 1, "测试剧")

	fake.mu.Lock()
	announced := strings.Join(fake.messages, "\n")
	fake.mu.Unlock()
	if !strings.Contains(announced, "第1季") {
		t.Fatalf("公告应带「第1季」，实际消息: %q", announced)
	}
	if !strings.Contains(announced, "测试剧") {
		t.Fatalf("公告应带片名，实际消息: %q", announced)
	}
	// S1 与任意季已结清
	if got := s.carpool.Get(100, "tv", 1); len(got) != 0 {
		t.Fatalf("S1 到货后 S1 应结清，实际 %v", got)
	}
	if got := s.carpool.Get(100, "tv", 0); len(got) != 0 {
		t.Fatalf("S1 到货后任意季应结清，实际 %v", got)
	}
	// S2 保留
	if got := s.carpool.Get(100, "tv", 2); len(got) != 1 || got[0] != 222 {
		t.Fatalf("S1 到货后 S2 应保留，实际 %v", got)
	}
}

// TestNotifyCarpoolMembersMovieUnaffected 电影到货不受季逻辑影响。
func TestNotifyCarpoolMembersMovieUnaffected(t *testing.T) {
	fake := &fakeTelegramServer{}
	s := newCarpoolNotifyTestService(t, fake)
	s.carpool.Add(200, "movie", 0, 111)

	s.notifyCarpoolMembers("200", "movie", 0, "测试电影")

	if got := s.carpool.Get(200, "movie", 0); len(got) != 0 {
		t.Fatalf("电影到货后应结清，实际 %v", got)
	}
	fake.mu.Lock()
	announced := strings.Join(fake.messages, "\n")
	fake.mu.Unlock()
	if strings.Contains(announced, "第") && strings.Contains(announced, "季") {
		t.Fatalf("电影公告不应带季号，实际消息: %q", announced)
	}
}
