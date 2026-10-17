package miniapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubUserMapping 是审计 A1 的测试桩：boundIDs 里的用户视为已绑定。
type stubUserMapping struct {
	boundIDs map[int64]bool
}

func (m *stubUserMapping) GetMoviePilotUserID(telegramID int64) (int64, bool) {
	if m.boundIDs[telegramID] {
		return telegramID + 1000, true
	}
	return 0, false
}
func (m *stubUserMapping) GetMoviePilotUsername(telegramID int64) (string, error) { return "", nil }
func (m *stubUserMapping) GetTelegramUsername(telegramID int64) string            { return "" }
func (m *stubUserMapping) SetTelegramUsername(telegramID int64, username string)  {}
func (m *stubUserMapping) AddMapping(telegramID int64, mpUserID int64, mpUsername string) error {
	return nil
}
func (m *stubUserMapping) RemoveMapping(telegramID int64) error { return nil }
func (m *stubUserMapping) GetTelegramIDByMoviePilotUsername(username string) (int64, bool) {
	return 0, false
}
func (m *stubUserMapping) GetTelegramIDByJellyseerrID(jellyseerrID int64) (int64, bool) {
	return 0, false
}
func (m *stubUserMapping) GetAllTelegramUsers() []int64 { return nil }
func (m *stubUserMapping) ForceSave() error             { return nil }

// 审计 A1：想看列表的写操作（POST/DELETE）必须验绑定，未绑定 → 403。
func TestWatchlistWriteRequiresBinding(t *testing.T) {
	mapping := &stubUserMapping{boundIDs: map[int64]bool{}}
	handler := NewServer(Deps{BotToken: miniAppTestToken, UserMapping: mapping}).Handler()

	req := signedRequest(t, http.MethodPost, "/api/miniapp/v1/watchlist",
		`{"tmdb_id": 123, "type": "movie"}`, 777)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden && resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("未绑定用户 POST watchlist 应被拒绝，实际 status=%d body=%s", resp.Code, resp.Body.String())
	}

	// GET 读操作不受影响（仍返回列表，无绑定要求）。
	reqGet := signedRequest(t, http.MethodGet, "/api/miniapp/v1/watchlist", "", 777)
	respGet := httptest.NewRecorder()
	handler.ServeHTTP(respGet, reqGet)
	if respGet.Code == http.StatusForbidden {
		t.Fatalf("GET watchlist 不应要求绑定，实际 status=%d", respGet.Code)
	}
}

// 审计 A1：已绑定用户不受影响（走到业务校验，而不是 403）。
func TestWatchlistWriteBoundUserPassesBindingGate(t *testing.T) {
	mapping := &stubUserMapping{boundIDs: map[int64]bool{888: true}}
	handler := NewServer(Deps{BotToken: miniAppTestToken, UserMapping: mapping}).Handler()

	req := signedRequest(t, http.MethodPost, "/api/miniapp/v1/watchlist",
		`{"tmdb_id": 123, "type": "movie"}`, 888)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code == http.StatusForbidden {
		t.Fatalf("已绑定用户不应被 403，body=%s", resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "未就绪") && resp.Code != http.StatusServiceUnavailable {
		t.Logf("已绑定用户通过绑定门（后续业务状态 %d），符合预期", resp.Code)
	}
}

// 审计 A1：洗版提交（POST）必须验绑定。
func TestWashSubmitRequiresBinding(t *testing.T) {
	mapping := &stubUserMapping{boundIDs: map[int64]bool{}}
	handler := NewServer(Deps{BotToken: miniAppTestToken, UserMapping: mapping}).Handler()

	req := signedRequest(t, http.MethodPost, "/api/miniapp/v1/wash",
		`{"tmdb_id": 123, "type": "movie"}`, 777)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden && resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("未绑定用户 POST wash 应被拒绝，实际 status=%d body=%s", resp.Code, resp.Body.String())
	}
}

// 审计 A1：问题反馈提交（POST）必须验绑定。
func TestIssueSubmitRequiresBinding(t *testing.T) {
	mapping := &stubUserMapping{boundIDs: map[int64]bool{}}
	handler := NewServer(Deps{BotToken: miniAppTestToken, UserMapping: mapping}).Handler()

	req := signedRequest(t, http.MethodPost, "/api/miniapp/v1/issues",
		`{"title": "播放卡顿", "description": "详情"}`, 777)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden && resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("未绑定用户 POST issues 应被拒绝，实际 status=%d body=%s", resp.Code, resp.Body.String())
	}
}

// 审计 A2：搜索在库状态缓存——命中时不再打上游。
func TestAvailabilityCacheHit(t *testing.T) {
	s := NewServer(Deps{BotToken: miniAppTestToken})
	if _, hit := s.cachedAvailability(123, "movie"); hit {
		t.Fatal("空缓存不应命中")
	}
	s.storeAvailability(123, "movie", true)
	if exists, hit := s.cachedAvailability(123, "movie"); !hit || !exists {
		t.Fatalf("写入后应命中且 exists=true，hit=%v exists=%v", hit, exists)
	}
	// 不同类型/ID 不串。
	if _, hit := s.cachedAvailability(123, "tv"); hit {
		t.Fatal("movie/tv 不应串缓存")
	}
}
