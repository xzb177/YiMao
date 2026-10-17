package handlers

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

func carpoolSeasonTestCtx(userID int64, params map[string]string) *callback.Context {
	return &callback.Context{
		UserID:   userID,
		ChatID:   -1001,
		ChatType: "supergroup",
		Callback: &callback.Callback{Action: "carpool", Params: params},
	}
}

// TestCarpoolHandlerSeasonParam Bot 回调带季号时记入该季拼车池。
func TestCarpoolHandlerSeasonParam(t *testing.T) {
	svc := services.NewCarpoolService(t.TempDir())
	h := NewCarpoolHandler(svc)

	resp, err := h.Handle(carpoolSeasonTestCtx(111, map[string]string{"id": "100", "type": "tv", "season": "2"}))
	if err != nil || resp == nil {
		t.Fatalf("Handle err=%v resp=%v", err, resp)
	}
	if !strings.Contains(resp.CallbackMsg, "第2季") {
		t.Fatalf("toast 应带「第2季」，实际 %q", resp.CallbackMsg)
	}
	if !svc.Contains(100, "tv", 2, 111) {
		t.Fatalf("用户应加入 S2 拼车池")
	}
	if svc.Contains(100, "tv", 1, 111) {
		t.Fatalf("用户不应出现在 S1 拼车池")
	}
}

// TestCarpoolHandlerMissingSeasonDefaultsToAnySeason 无季号回调记为任意季（兼容旧按钮）。
func TestCarpoolHandlerMissingSeasonDefaultsToAnySeason(t *testing.T) {
	svc := services.NewCarpoolService(t.TempDir())
	h := NewCarpoolHandler(svc)

	if _, err := h.Handle(carpoolSeasonTestCtx(111, map[string]string{"id": "100", "type": "tv"})); err != nil {
		t.Fatalf("Handle err=%v", err)
	}
	if !svc.Contains(100, "tv", 0, 111) {
		t.Fatalf("无季号回调应记为任意季")
	}
}

// TestCarpoolHandlerMovieIgnoresSeason 电影回调忽略季号。
func TestCarpoolHandlerMovieIgnoresSeason(t *testing.T) {
	svc := services.NewCarpoolService(t.TempDir())
	h := NewCarpoolHandler(svc)

	if _, err := h.Handle(carpoolSeasonTestCtx(111, map[string]string{"id": "200", "type": "movie", "season": "3"})); err != nil {
		t.Fatalf("Handle err=%v", err)
	}
	if !svc.Contains(200, "movie", 0, 111) {
		t.Fatalf("电影应忽略季号加入")
	}
}
