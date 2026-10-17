package bot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/pkg/dedupfile"
	"github.com/xzb177/yimao/pkg/types"
)

// 审计第 9 条：Telegram webhook 以前没有按 update_id 去重。Telegram 在
// 收不到 200 响应时会重投同一条 update（回调处理慢导致超时最常见），
// 没有去重就会重复执行：重复回复、重复求片、重复扣配额。
// 复现：同一条 update 连投两次，第二次必须直接回 200 OK 且不执行业务。
func TestHandleWebhookDedupsUpdateID(t *testing.T) {
	webhookDuplicatesSkipped.Store(0)
	before := webhookDuplicatesSkipped.Load()

	cfg := &config.Config{TelegramWebhookSecret: "test-secret"}
	update := types.TelegramUpdate{
		UpdateID: 9001001,                                         // 本测试独占的 id，避免与其他测试的进程内去重器串扰
		Message:  &types.TelegramMessage{MessageID: 1, Text: "x"}, // Chat 为 nil：路由前 400
	}
	body, _ := json.Marshal(update)

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
		rr := httptest.NewRecorder()
		HandleWebhook(rr, req, nil, &Dependencies{}, cfg)
		return rr
	}

	first := post()
	if first.Code != http.StatusBadRequest {
		t.Fatalf("first delivery: status=%d, want 400 (passes dedup, fails routing validation)", first.Code)
	}
	second := post()
	if second.Code != http.StatusOK {
		t.Fatalf("redelivered update: status=%d, want 200 OK (deduped, not re-executed)", second.Code)
	}
	if got := webhookDuplicatesSkipped.Load() - before; got != 1 {
		t.Fatalf("webhookDuplicatesSkipped delta=%d, want 1", got)
	}
}

// acceptWebhookUpdate：TTL 内的重复 id 只放行一次。
func TestAcceptWebhookUpdateDedups(t *testing.T) {
	dir := t.TempDir() // 每个测试独立数据目录，避免与进程内全局去重器串扰
	id := int64(9001002)
	if !acceptWebhookUpdate(dir, id) {
		t.Fatal("first accept must pass")
	}
	if acceptWebhookUpdate(dir, id) {
		t.Fatal("duplicate accept inside TTL must be rejected")
	}
}

// 亲审 R1：持久化去重必须在「进程重启」（重建实例）后仍判重。
// 先写红后写绿：旧的纯内存实现下第二个 newPersistentUpdateDeduper 必然放行。
func TestPersistentUpdateDeduperSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	d1 := newPersistentUpdateDeduper(dir, persistentUpdateTTL)
	if !d1.accept(7001001) || !d1.accept(7001002) {
		t.Fatal("first accepts must pass")
	}
	d1.saveLocked() // 模拟：落盘（绕过 30 秒节流）

	// 「重启」：用同一文件重建实例
	d2 := newPersistentUpdateDeduper(dir, persistentUpdateTTL)
	if d2.accept(7001001) {
		t.Fatal("update 7001001 accepted after restart: dedup ledger lost")
	}
	if d2.accept(7001002) {
		t.Fatal("update 7001002 accepted after restart: dedup ledger lost")
	}
	if !d2.accept(7001003) {
		t.Fatal("new update 7001003 must pass")
	}
}

// 过期条目在加载时被清理：账本里的时间戳早于窗口，重建后应放行。
func TestPersistentUpdateDeduperPrunesExpiredOnLoad(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tg_update_dedup.json")
	old := map[string]int64{"7002001": time.Now().Add(-48 * time.Hour).Unix()}
	if err := dedupfile.Save(file, old); err != nil {
		t.Fatal(err)
	}
	d := newPersistentUpdateDeduper(dir, persistentUpdateTTL)
	if !d.accept(7002001) {
		t.Fatal("expired entry must be pruned on load, accept should pass")
	}
}

// 账本损坏时 fail-open：不崩溃、不拒绝新消息。
func TestPersistentUpdateDeduperCorruptLedgerFailsOpen(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tg_update_dedup.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := newPersistentUpdateDeduper(dir, persistentUpdateTTL)
	if !d.accept(7003001) {
		t.Fatal("corrupt ledger must fail open: new update should pass")
	}
}

// processPollBatch：offset 必须在整批 update 实际处理完成后才推进，
// 而不是 dispatch（起 goroutine）完就推进——否则处理中的 update 在崩溃
// 后会彻底丢失（offset 已经确认，Telegram 不再投递）。
func TestProcessPollBatchWaitsForCompletion(t *testing.T) {
	dedup := newUpdateDeduper(processedUpdateTTL)
	sem := make(chan struct{}, maxUpdateWorkers)

	var completed atomic.Int64
	release := make(chan struct{})
	handler := func(u types.TelegramUpdate) {
		<-release // 模拟慢业务：不放行就不算完成
		completed.Add(1)
	}

	updates := []types.TelegramUpdate{
		{UpdateID: 9002001}, {UpdateID: 9002002}, {UpdateID: 9002001}, // 含重复
	}
	done := make(chan int64, 1)
	go func() { done <- processPollBatch(updates, dedup, sem, handler) }()

	// handler 还没放行：processPollBatch 不能提前返回。
	select {
	case maxID := <-done:
		t.Fatalf("processPollBatch returned %d before handlers completed", maxID)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	select {
	case maxID := <-done:
		if maxID != 9002002 {
			t.Fatalf("maxUpdateID=%d, want 9002002", maxID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("processPollBatch did not return after handlers completed")
	}
	if got := completed.Load(); got != 2 {
		t.Fatalf("completed handlers=%d, want 2 (duplicate 9002001 handled once)", got)
	}
}

// 并发上限同样约束 webhook 路径：突发不能无限制打满下游。
func TestWebhookSemaphoreIsBounded(t *testing.T) {
	if cap(webhookSem) != maxUpdateWorkers {
		t.Fatalf("webhookSem cap=%d, want %d", cap(webhookSem), maxUpdateWorkers)
	}
	var _ = sync.Once{}
}
