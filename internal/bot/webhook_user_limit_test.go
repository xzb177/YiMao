package bot

import (
	"testing"

	"github.com/xzb177/yimao/pkg/types"
)

// 审计 A2：单用户 60 秒窗口内最多 30 个 update，超出直接拒绝（不占处理位）。
func TestWebhookUserRateLimit(t *testing.T) {
	const user = int64(424242)
	defer func() {
		webhookUserLimitMu.Lock()
		delete(webhookUserWindows, user)
		webhookUserLimitMu.Unlock()
	}()
	for i := 0; i < webhookUserMaxUpdates; i++ {
		if !allowWebhookUpdate(user) {
			t.Fatalf("第 %d 次应放行", i+1)
		}
		releaseWebhookUpdate(user) // 只测速率：立即归还占用
	}
	if allowWebhookUpdate(user) {
		t.Fatalf("第 %d 次应被限流", webhookUserMaxUpdates+1)
	}
}

// 审计 A2：单用户同时占用处理位上限（默认 4），占满后新的直接拒绝——
// 一个人连点占不满全局 20 个处理位。
func TestWebhookUserOccupancyLimit(t *testing.T) {
	const user = int64(434343)
	defer func() {
		webhookUserLimitMu.Lock()
		delete(webhookUserWindows, user)
		webhookUserLimitMu.Unlock()
	}()
	for i := 0; i < webhookUserOccupancyLimit; i++ {
		if !allowWebhookUpdate(user) {
			t.Fatalf("第 %d 个并发位应放行", i+1)
		}
	}
	if allowWebhookUpdate(user) {
		t.Fatal("第 5 个并发位应被拒绝")
	}
	releaseWebhookUpdate(user)
	if !allowWebhookUpdate(user) {
		t.Fatal("归还一位后应放行")
	}
	releaseWebhookUpdate(user)
}

// 不同用户互不影响。
func TestWebhookUserLimitIsPerUser(t *testing.T) {
	const a, b = int64(444001), int64(444002)
	defer func() {
		webhookUserLimitMu.Lock()
		delete(webhookUserWindows, a)
		delete(webhookUserWindows, b)
		webhookUserLimitMu.Unlock()
	}()
	for i := 0; i < webhookUserMaxUpdates; i++ {
		if !allowWebhookUpdate(a) {
			t.Fatalf("用户 A 第 %d 次应放行", i+1)
		}
		releaseWebhookUpdate(a)
	}
	if allowWebhookUpdate(a) {
		t.Fatal("用户 A 应被限流")
	}
	if !allowWebhookUpdate(b) {
		t.Fatal("用户 B 不应受用户 A 影响")
	}
	releaseWebhookUpdate(b)
}

func TestWebhookUpdateUserIDExtraction(t *testing.T) {
	u := types.TelegramUpdate{
		CallbackQuery: &types.TelegramCallbackQuery{From: &types.TelegramUser{ID: 777}},
	}
	if got := webhookUpdateUserID(u); got != 777 {
		t.Fatalf("回调应取出 777，实际 %d", got)
	}
	u2 := types.TelegramUpdate{
		Message: &types.TelegramMessage{From: &types.TelegramUser{ID: 888}},
	}
	if got := webhookUpdateUserID(u2); got != 888 {
		t.Fatalf("消息应取出 888，实际 %d", got)
	}
}
