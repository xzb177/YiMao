package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/types"
)

// 审计第 6 条：轮询模式把两个词的消息当成账号密码。
// 未绑定用户搜「iron man」会被自动注册/登录、计入锁定次数、消息被删。
// 要求：普通搜索消息不许走凭据处理路径，凭据流程只能由 /link 命令触发。

type stubUserMapping struct{}

func (stubUserMapping) GetMoviePilotUserID(telegramID int64) (int64, bool) {
	return 0, false // 未绑定
}
func (stubUserMapping) GetMoviePilotUsername(telegramID int64) (string, error) {
	return "", nil
}
func (stubUserMapping) GetTelegramUsername(telegramID int64) string           { return "" }
func (stubUserMapping) SetTelegramUsername(telegramID int64, username string) {}
func (stubUserMapping) AddMapping(telegramID int64, mpUserID int64, mpUsername string) error {
	return nil
}
func (stubUserMapping) RemoveMapping(telegramID int64) error { return nil }
func (stubUserMapping) GetTelegramIDByMoviePilotUsername(username string) (int64, bool) {
	return 0, false
}
func (stubUserMapping) GetTelegramIDByJellyseerrID(jellyseerrID int64) (int64, bool) {
	return 0, false
}
func (stubUserMapping) GetAllTelegramUsers() []int64 { return nil }
func (stubUserMapping) ForceSave() error             { return nil }

type tgCall struct {
	path string
	body []byte
}

func TestPollTwoWordMessageIsNotTreatedAsCredentials(t *testing.T) {
	var mu sync.Mutex
	var calls []tgCall
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, tgCall{path: r.URL.Path, body: chunk})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"}}}`))
	}))
	defer tgServer.Close()

	// MoviePilot 桩：所有请求都 404（用户不存在、注册失败），模拟最保守的上游。
	mpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"message":"not found"}`))
	}))
	defer mpServer.Close()

	telegram := services.NewTelegramClient("test")
	telegram.SetBaseURLForTest(tgServer.URL, tgServer.Client())

	deps := &PollDeps{
		Telegram:    telegram,
		MoviePilot:  services.NewMoviePilotClient(mpServer.URL, "key", ""),
		SessionMgr:  session.NewManager(time.Hour, 1000),
		UserMapping: stubUserMapping{},
	}
	cfg := &config.Config{MoviePilotURL: mpServer.URL, MoviePilotAPIKey: "key"}

	msg := &types.TelegramMessage{
		MessageID: 99,
		From:      &types.TelegramUser{ID: 777001},
		Chat:      &types.TelegramChat{ID: 777001, Type: "private"},
		Text:      "iron man",
	}

	HandlePollMessage(msg, deps, cfg)

	// HandleLinkCommand 里删消息是异步 goroutine：给它一点时间冒出来（修前必须能抓到）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		found := false
		for _, c := range calls {
			if strings.Contains(c.path, "deleteMessage") {
				found = true
				break
			}
		}
		mu.Unlock()
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, c := range calls {
		if strings.Contains(c.path, "deleteMessage") {
			t.Fatalf("two-word search message was deleted as if it contained credentials (audit item 6)")
		}
		if strings.Contains(c.path, "sendMessage") {
			var payload map[string]any
			if err := json.Unmarshal(c.body, &payload); err == nil {
				if text, ok := payload["text"].(string); ok && strings.Contains(text, "绑定") {
					t.Fatalf("two-word search message entered the link/credential flow: %q", text)
				}
			}
		}
	}

	// 锁定计数器也不能被污染（修前走 HandleLinkCommand 失败路径会 +1）。
	linkAttemptsMu.Lock()
	_, polluted := linkAttempts[777001]
	linkAttemptsMu.Unlock()
	if polluted {
		t.Fatalf("link failure counter was polluted by a plain two-word search message")
	}
}
