package services

import (
	"os"
	"testing"
	"time"
)

// 本测试覆盖 #6 许愿池最易出 bug 的核心：canonical key 去重、容量上限、
// searching_at 锁 + 自愈、状态跃迁的并发安全（前置状态校验）。

func newTestWishService(t *testing.T) *WishService {
	t.Helper()
	dir, err := os.MkdirTemp("", "wishtest")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ws, err := NewWishService(dir)
	if err != nil {
		t.Fatalf("NewWishService: %v", err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func TestWishAddAndCanonicalDedup(t *testing.T) {
	ws := newTestWishService(t)

	// 首次入池成功。
	res, err := ws.AddWish(&WishItem{UserID: 1, TmdbID: 100, MediaType: "movie", Title: "片A"})
	if err != nil {
		t.Fatalf("AddWish: %v", err)
	}
	if !res.Created {
		t.Fatalf("expected Created, got %+v", res)
	}

	// 同 canonical key 再入 → Duplicate。
	res, err = ws.AddWish(&WishItem{UserID: 2, TmdbID: 100, MediaType: "movie", Title: "片A"})
	if err != nil {
		t.Fatalf("AddWish dup: %v", err)
	}
	if !res.Duplicate || res.Existing == nil {
		t.Fatalf("expected Duplicate with Existing, got %+v", res)
	}

	// 不同 season 视为不同 canonical key（tv）。
	if _, err := ws.AddWish(&WishItem{UserID: 1, TmdbID: 200, MediaType: "tv", Title: "剧B", Season: 1}); err != nil {
		t.Fatalf("AddWish tv s1: %v", err)
	}
	res, err = ws.AddWish(&WishItem{UserID: 1, TmdbID: 200, MediaType: "tv", Title: "剧B", Season: 2})
	if err != nil {
		t.Fatalf("AddWish tv s2: %v", err)
	}
	if !res.Created {
		t.Fatalf("different season should be new item, got %+v", res)
	}

	// 无 canonical key → 拒绝。
	if _, err := ws.AddWish(&WishItem{UserID: 1, MediaType: "movie", Title: "无ID"}); err == nil {
		t.Fatalf("expected error for missing canonical key")
	}

	// 仅 imdb（tmdb=0）→ 允许，且按 imdb 去重。
	if _, err := ws.AddWish(&WishItem{UserID: 1, ImdbID: "tt999", MediaType: "movie", Title: "imdbOnly"}); err != nil {
		t.Fatalf("AddWish imdb: %v", err)
	}
	res, err = ws.AddWish(&WishItem{UserID: 3, ImdbID: "tt999", MediaType: "movie", Title: "imdbOnly"})
	if err != nil {
		t.Fatalf("AddWish imdb dup: %v", err)
	}
	if !res.Duplicate {
		t.Fatalf("imdb dedup failed, got %+v", res)
	}
}

func TestWishStateMachineTransitions(t *testing.T) {
	ws := newTestWishService(t)
	res, err := ws.AddWish(&WishItem{UserID: 1, TmdbID: 1, MediaType: "movie", Title: "片"})
	if err != nil || !res.Created {
		t.Fatalf("add: %v %+v", err, res)
	}
	// 取 id。
	items, _ := ws.ListByUser(1)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	id := items[0].ID

	// 认领 → SEARCHING + 上锁。
	claimed, err := ws.ClaimSearchableItems(24, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v len=%d", err, len(claimed))
	}
	if claimed[0].State != WishStateSearching || claimed[0].SearchingAt == nil {
		t.Fatalf("expected SEARCHING+locked, got %+v", claimed[0])
	}

	// 锁定后再认领（interval 内）→ 空（锁生效）。
	again, err := ws.ClaimSearchableItems(24, 10)
	if err != nil {
		t.Fatalf("claim again: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("lock should prevent re-claim, got %d", len(again))
	}

	// MarkFound（SEARCHING→FOUND），首次成功，二次返回 false（防重复推）。
	changed, err := ws.MarkFound(id, "siteX · 做种 5")
	if err != nil || !changed {
		t.Fatalf("MarkFound: %v changed=%v", err, changed)
	}
	changed, _ = ws.MarkFound(id, "again")
	if changed {
		t.Fatalf("second MarkFound should be no-op")
	}

	// FOUND→NOTIFIED。
	changed, err = ws.MarkNotified(id)
	if err != nil || !changed {
		t.Fatalf("MarkNotified: %v changed=%v", err, changed)
	}
	changed, _ = ws.MarkNotified(id)
	if changed {
		t.Fatalf("second MarkNotified should be no-op")
	}

	// NOTIFIED→FULFILLED。
	changed, err = ws.MarkFulfilled(id)
	if err != nil || !changed {
		t.Fatalf("MarkFulfilled: %v changed=%v", err, changed)
	}

	// FULFILLED 后不再活跃。
	items, _ = ws.ListByUser(1)
	if len(items) != 0 {
		t.Fatalf("expected no active items after fulfilled, got %d", len(items))
	}
}

func TestWishSearchLockSelfHeal(t *testing.T) {
	ws := newTestWishService(t)
	res, _ := ws.AddWish(&WishItem{UserID: 1, TmdbID: 1, MediaType: "movie", Title: "片"})
	if !res.Created {
		t.Fatalf("add failed")
	}
	items, _ := ws.ListByUser(1)
	id := items[0].ID

	// 认领上锁。
	if c, _ := ws.ClaimSearchableItems(24, 10); len(c) != 1 {
		t.Fatalf("first claim failed")
	}

	// 手动把 searching_at 改成很久以前（模拟崩溃残留旧锁）。
	old := time.Now().Add(-48 * time.Hour)
	if _, err := ws.db.Exec(`UPDATE wish_items SET searching_at=? WHERE id=?`, old, id); err != nil {
		t.Fatalf("manual update: %v", err)
	}

	// 自愈：interval=24h，旧锁超时应被重新认领。
	c, err := ws.ClaimSearchableItems(24, 10)
	if err != nil {
		t.Fatalf("self-heal claim: %v", err)
	}
	if len(c) != 1 {
		t.Fatalf("self-heal failed, expected 1 reclaim, got %d", len(c))
	}
}

func TestWishCapacityLimits(t *testing.T) {
	ws := newTestWishService(t)
	// 每人上限。
	for i := 0; i < WishMaxPerUser; i++ {
		res, err := ws.AddWish(&WishItem{UserID: 7, TmdbID: 1000 + i, MediaType: "movie", Title: "p"})
		if err != nil || !res.Created {
			t.Fatalf("fill %d: %v %+v", i, err, res)
		}
	}
	res, err := ws.AddWish(&WishItem{UserID: 7, TmdbID: 9999, MediaType: "movie", Title: "over"})
	if err != nil {
		t.Fatalf("over add: %v", err)
	}
	if !res.OverPerUser {
		t.Fatalf("expected OverPerUser, got %+v", res)
	}
}

func TestSearchOffsetDeterministic(t *testing.T) {
	a := SearchOffsetMinutes(42)
	b := SearchOffsetMinutes(42)
	if a != b {
		t.Fatalf("offset not deterministic: %d vs %d", a, b)
	}
	if a < 0 || a >= 1440 {
		t.Fatalf("offset out of range: %d", a)
	}
}
