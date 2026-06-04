package services

import (
	"database/sql"
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
	offMin := items[0].SearchOffsetMinute

	// 认领 → SEARCHING + 上锁（用该条目自己的错峰分钟）。
	claimed, err := ws.ClaimSearchableItems(offMin, 60, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v len=%d", err, len(claimed))
	}
	if claimed[0].State != WishStateSearching || claimed[0].SearchingAt == nil {
		t.Fatalf("expected SEARCHING+locked, got %+v", claimed[0])
	}

	// 锁定后再认领（TTL 内）→ 空（锁生效）。
	again, err := ws.ClaimSearchableItems(offMin, 60, 10)
	if err != nil {
		t.Fatalf("claim again: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("lock should prevent re-claim, got %d", len(again))
	}

	// 错峰：不在本条目分钟时不应被认领。
	other := (offMin + 1) % 1440
	if c, _ := ws.ClaimSearchableItems(other, 60, 10); len(c) != 0 {
		t.Fatalf("offset filter failed: claimed in wrong minute, got %d", len(c))
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
	offMin := items[0].SearchOffsetMinute

	// 认领上锁。
	if c, _ := ws.ClaimSearchableItems(offMin, 60, 10); len(c) != 1 {
		t.Fatalf("first claim failed")
	}

	// 手动把 searching_at 改成 30 分钟前（仍在 60min TTL 内 → 不应被重认领）。
	recent := time.Now().Add(-30 * time.Minute)
	if _, err := ws.db.Exec(`UPDATE wish_items SET searching_at=? WHERE id=?`, recent, id); err != nil {
		t.Fatalf("manual update recent: %v", err)
	}
	if c, _ := ws.ClaimSearchableItems(offMin, 60, 10); len(c) != 0 {
		t.Fatalf("lock within TTL should not be reclaimed, got %d", len(c))
	}

	// 手动把 searching_at 改成 90 分钟前（超过 60min 自愈锁 TTL → 应被重认领）。
	old := time.Now().Add(-90 * time.Minute)
	if _, err := ws.db.Exec(`UPDATE wish_items SET searching_at=? WHERE id=?`, old, id); err != nil {
		t.Fatalf("manual update: %v", err)
	}

	// 自愈：TTL=60min，旧锁超时应被重新认领（盲区从 24h 缩到 1h，验证坑B 解耦）。
	c, err := ws.ClaimSearchableItems(offMin, 60, 10)
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

// TestWishNoStarvationLargePool 验证 B1 修复：>200 条（这里 600 条）活跃许愿时，
// 每个 SEARCHING 条目都能在它自己的错峰分钟被 ClaimSearchableItems 选到（不饿死），
// 且每个分片每天只认领一次（错峰列在 1440 分钟里唯一命中）。
func TestWishNoStarvationLargePool(t *testing.T) {
	ws := newTestWishService(t)

	const total = 600
	// 每人上限 20，分散到多个用户避免触发 per-user / global 上限。
	// 全局上限 500，因此临时放大全局空间：这里用直插绕过上限校验，专测调度覆盖性。
	now := time.Now()
	for i := 0; i < total; i++ {
		off := i % 1440 // 直接指定不同分片，覆盖各分钟
		if _, err := ws.db.Exec(`
			INSERT INTO wish_items
				(user_id, tmdb_id, imdb_id, media_type, title, year, season, state, found_detail, search_offset_minute, created_at, updated_at)
			VALUES (?, ?, '', 'movie', ?, 0, 0, ?, '', ?, ?, ?)`,
			int64(i%50), 100000+i, "p", WishStateSearching, off, now, now); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// 收集每个 id 的分片分钟。
	rows, err := ws.db.Query(`SELECT id, search_offset_minute FROM wish_items`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	offsetByID := map[int64]int{}
	minuteHas := map[int]bool{}
	for rows.Next() {
		var id int64
		var off int
		if err := rows.Scan(&id, &off); err != nil {
			t.Fatalf("scan: %v", err)
		}
		offsetByID[id] = off
		minuteHas[off] = true
	}
	rows.Close()
	if len(offsetByID) != total {
		t.Fatalf("expected %d rows, got %d", total, len(offsetByID))
	}

	// 模拟一整天 1440 分钟逐分钟 tick，认领并标记「已搜过」（用 MarkFound 终态化以观察覆盖）。
	claimedIDs := map[int64]bool{}
	for minute := 0; minute < 1440; minute++ {
		// limit 给足够大，确保同一分钟同分片的多条都被覆盖（不被 LIMIT 截断饿死）。
		claimed, err := ws.ClaimSearchableItems(minute, 60, 10000)
		if err != nil {
			t.Fatalf("claim minute %d: %v", minute, err)
		}
		for _, it := range claimed {
			if offsetByID[it.ID] != minute {
				t.Fatalf("id=%d claimed in minute %d but offset=%d", it.ID, minute, offsetByID[it.ID])
			}
			if claimedIDs[it.ID] {
				t.Fatalf("id=%d claimed twice within one day（每片每天应仅一次）", it.ID)
			}
			claimedIDs[it.ID] = true
			// 标记 FOUND 让它退出 SEARCHING，验证「每天一次」且不重复。
			if _, err := ws.MarkFound(it.ID, "hit"); err != nil {
				t.Fatalf("MarkFound id=%d: %v", it.ID, err)
			}
		}
	}

	// 全量覆盖：每个条目都应在其分片分钟被认领过一次（无饿死）。
	if len(claimedIDs) != total {
		t.Fatalf("starvation detected: only %d/%d items claimed over a full day", len(claimedIDs), total)
	}
}

// TestWishExpirySweepMeta 验证过期扫描漏跑修复：last_expiry_sweep_at 持久化 + 周期判定。
func TestWishExpirySweepMeta(t *testing.T) {
	ws := newTestWishService(t)

	// 初始无记录 → ok=false（应触发补跑）。
	if _, ok, err := ws.GetLastExpirySweep(); err != nil || ok {
		t.Fatalf("expected no record initially, got ok=%v err=%v", ok, err)
	}

	now := time.Now()
	if err := ws.SetLastExpirySweep(now); err != nil {
		t.Fatalf("SetLastExpirySweep: %v", err)
	}
	got, ok, err := ws.GetLastExpirySweep()
	if err != nil || !ok {
		t.Fatalf("expected record after set, ok=%v err=%v", ok, err)
	}
	// RFC3339 秒级精度，允许 <2s 偏差。
	if d := got.Sub(now); d > 2*time.Second || d < -2*time.Second {
		t.Fatalf("persisted time drift too large: %v", d)
	}

	// 覆盖写（UPSERT）。
	later := now.Add(25 * time.Hour)
	if err := ws.SetLastExpirySweep(later); err != nil {
		t.Fatalf("SetLastExpirySweep update: %v", err)
	}
	got2, _, _ := ws.GetLastExpirySweep()
	if got2.Before(now.Add(24 * time.Hour)) {
		t.Fatalf("upsert failed, got %v", got2)
	}
}

// TestWishMigrationBackwardCompat 验证向后兼容迁移：旧库（无 search_offset_minute 列）
// 经 NewWishService 打开时应 ALTER TABLE 补列并回填，不崩溃，且存量行错峰散布。
func TestWishMigrationBackwardCompat(t *testing.T) {
	dir, err := os.MkdirTemp("", "wishmig")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := dir + "/wishpool.db"
	// 手工建「旧版」表（缺 search_offset_minute 列），并插 3 行存量数据。
	old, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open old: %v", err)
	}
	if _, err := old.Exec(`
		CREATE TABLE wish_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			tmdb_id INTEGER NOT NULL DEFAULT 0,
			imdb_id TEXT NOT NULL DEFAULT '',
			media_type TEXT NOT NULL DEFAULT 'movie',
			title TEXT NOT NULL DEFAULT '',
			year INTEGER NOT NULL DEFAULT 0,
			season INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'PENDING',
			found_detail TEXT NOT NULL DEFAULT '',
			searching_at DATETIME,
			notified_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	now := time.Now()
	for i := 1; i <= 3; i++ {
		if _, err := old.Exec(`INSERT INTO wish_items
			(user_id, tmdb_id, media_type, title, state, created_at, updated_at)
			VALUES (?, ?, 'movie', 'p', ?, ?, ?)`,
			1, 1000+i, WishStateSearching, now, now); err != nil {
			t.Fatalf("insert old %d: %v", i, err)
		}
	}
	old.Close()

	// 用新版打开：应迁移成功而不崩溃。
	ws, err := NewWishService(dir)
	if err != nil {
		t.Fatalf("NewWishService on old db: %v", err)
	}
	t.Cleanup(func() { ws.Close() })

	// 列已存在且存量行被回填错峰偏移（不应全为 0）。
	rows, err := ws.db.Query(`SELECT search_offset_minute FROM wish_items`)
	if err != nil {
		t.Fatalf("query offset: %v", err)
	}
	defer rows.Close()
	distinct := map[int]bool{}
	n := 0
	for rows.Next() {
		var off int
		if err := rows.Scan(&off); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if off < 0 || off >= 1440 {
			t.Fatalf("offset out of range after backfill: %d", off)
		}
		distinct[off] = true
		n++
	}
	if n != 3 {
		t.Fatalf("expected 3 migrated rows, got %d", n)
	}
	if len(distinct) < 2 {
		t.Fatalf("backfill should spread offsets, got all-same: %v", distinct)
	}

	// 二次打开（已含列）应幂等不报错。
	ws2, err := NewWishService(dir)
	if err != nil {
		t.Fatalf("re-open should be idempotent: %v", err)
	}
	ws2.Close()
}

// TestWishLockTTLDecoupled 验证坑B：自愈锁 TTL 与重搜周期解耦。
// 锁 30min 前（TTL=60）不可重认领；锁 90min 前（>TTL）可重认领，盲区 ~1h 而非 24h。
func TestWishLockTTLDecoupled(t *testing.T) {
	ws := newTestWishService(t)
	if _, err := ws.AddWish(&WishItem{UserID: 1, TmdbID: 555, MediaType: "movie", Title: "片"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	items, _ := ws.ListByUser(1)
	id := items[0].ID
	off := items[0].SearchOffsetMinute

	if c, _ := ws.ClaimSearchableItems(off, 60, 10); len(c) != 1 {
		t.Fatalf("first claim failed")
	}

	// 30min 前的锁，TTL=60 → 不可重认领。
	if _, err := ws.db.Exec(`UPDATE wish_items SET searching_at=? WHERE id=?`, time.Now().Add(-30*time.Minute), id); err != nil {
		t.Fatalf("update: %v", err)
	}
	if c, _ := ws.ClaimSearchableItems(off, 60, 10); len(c) != 0 {
		t.Fatalf("within TTL should not reclaim")
	}

	// 90min 前的锁，TTL=60 → 可重认领（自愈，盲区 < 24h）。
	if _, err := ws.db.Exec(`UPDATE wish_items SET searching_at=? WHERE id=?`, time.Now().Add(-90*time.Minute), id); err != nil {
		t.Fatalf("update: %v", err)
	}
	if c, _ := ws.ClaimSearchableItems(off, 60, 10); len(c) != 1 {
		t.Fatalf("expired lock should self-heal within ~1h")
	}
}
