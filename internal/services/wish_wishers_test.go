package services

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

// 本测试覆盖「许愿众筹计数」（wish_wishers）最易出 bug 的核心：
//   - 多人许同片：人数累计但 wish_items 仍只 1 条。
//   - 幂等：同 user 重复许 N 次仍只算 1。
//   - 迁移回填：旧库存量 wish_items 的发起人被回填进 wish_wishers，重复跑不增重。
//   - 退群计数：可达过滤为纯函数，剔退群者但不减计数。
//   - canonical key 与去重维度严格对齐（复用 CanonicalKey）。

// TestCanonicalKeyAlignment 验证 CanonicalKey 与 wish_items 去重维度一致：
// tmdb 优先、无则 imdb，带 media_type/season，media_type 空归一 movie。
func TestCanonicalKeyAlignment(t *testing.T) {
	cases := []struct {
		tmdb     int
		imdb, mt string
		season   int
		want     string
	}{
		{100, "tt1", "movie", 0, "tmdb-100-movie-0"}, // tmdb 优先（imdb 被忽略）
		{0, "tt9", "movie", 0, "imdb-tt9-movie-0"},   // 无 tmdb 用 imdb
		{200, "", "tv", 2, "tmdb-200-tv-2"},          // 带 season
		{300, "", "", 0, "tmdb-300-movie-0"},         // media_type 空归一 movie
		{0, " tt7 ", "tv", 1, "imdb-tt7-tv-1"},       // imdb 去空白
	}
	for _, c := range cases {
		got := CanonicalKey(c.tmdb, c.imdb, c.mt, c.season)
		if got != c.want {
			t.Errorf("CanonicalKey(%d,%q,%q,%d)=%q, want %q", c.tmdb, c.imdb, c.mt, c.season, got, c.want)
		}
	}

	// 与错峰偏移、群公示 key 共用同一维度（不同维度必产生不同 canonical）。
	if CanonicalKey(100, "", "movie", 0) == CanonicalKey(100, "", "tv", 0) {
		t.Errorf("media_type 不同应产生不同 canonical key")
	}
	if CanonicalKey(200, "", "tv", 1) == CanonicalKey(200, "", "tv", 2) {
		t.Errorf("season 不同应产生不同 canonical key")
	}
}

// TestWishersMultiUserSameTitle 多人许同片：3 个不同 user → CountWishers=3，wish_items 仍只 1 条。
func TestWishersMultiUserSameTitle(t *testing.T) {
	ws := newTestWishService(t)

	item := func(uid int64) *WishItem {
		return &WishItem{UserID: uid, TmdbID: 777, MediaType: "movie", Title: "众筹片"}
	}

	// user 1 首次 → Created。
	res, err := ws.AddWish(item(1))
	if err != nil || !res.Created {
		t.Fatalf("user1 add: %v %+v", err, res)
	}
	// user 2、user 3 重复许同片 → Duplicate，但仍记 wisher。
	for _, uid := range []int64{2, 3} {
		res, err := ws.AddWish(item(uid))
		if err != nil {
			t.Fatalf("user%d add: %v", uid, err)
		}
		if !res.Duplicate {
			t.Fatalf("user%d 期望 Duplicate, got %+v", uid, res)
		}
	}

	ck := CanonicalKey(777, "", "movie", 0)
	if n := ws.CountWishers(ck); n != 3 {
		t.Fatalf("CountWishers=%d, want 3", n)
	}

	// wish_items 仍只 1 条（去重不变，状态机不受影响）。
	var cnt int
	if err := ws.db.QueryRow(`SELECT COUNT(*) FROM wish_items WHERE tmdb_id=777`).Scan(&cnt); err != nil {
		t.Fatalf("count wish_items: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("wish_items 应仍只 1 条, got %d", cnt)
	}

	// ListWishers 返回全 3 人。
	got := ws.ListWishers(ck)
	if len(got) != 3 {
		t.Fatalf("ListWishers len=%d, want 3: %v", len(got), got)
	}
}

// TestWishersIdempotent 幂等：同一 user 重复许同片 N 次 → CountWishers 仍=1（INSERT OR IGNORE）。
func TestWishersIdempotent(t *testing.T) {
	ws := newTestWishService(t)

	ck := CanonicalKey(888, "", "movie", 0)
	// 首次入池。
	if _, err := ws.AddWish(&WishItem{UserID: 5, TmdbID: 888, MediaType: "movie", Title: "幂等片"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// 同 user 再许 4 次（都 Duplicate，且 INSERT OR IGNORE 不增）。
	for i := 0; i < 4; i++ {
		if _, err := ws.AddWish(&WishItem{UserID: 5, TmdbID: 888, MediaType: "movie", Title: "幂等片"}); err != nil {
			t.Fatalf("re-add %d: %v", i, err)
		}
	}
	if n := ws.CountWishers(ck); n != 1 {
		t.Fatalf("同 user 重复许应仍 1 人, got %d", n)
	}

	// 直接调 AddWisher 多次也幂等。
	for i := 0; i < 3; i++ {
		if err := ws.AddWisher(ck, 5); err != nil {
			t.Fatalf("AddWisher: %v", err)
		}
	}
	if n := ws.CountWishers(ck); n != 1 {
		t.Fatalf("AddWisher 幂等失败, got %d", n)
	}

	// 空 key / user=0 安全跳过（不报错、不写入）。
	if err := ws.AddWisher("", 9); err != nil {
		t.Fatalf("AddWisher 空 key: %v", err)
	}
	if err := ws.AddWisher(ck, 0); err != nil {
		t.Fatalf("AddWisher user=0: %v", err)
	}
	if n := ws.CountWishers(ck); n != 1 {
		t.Fatalf("空 key/user=0 不应写入, got %d", n)
	}
	if n := ws.CountWishers(""); n != 0 {
		t.Fatalf("空 key 计数应为 0, got %d", n)
	}
}

// TestWishersMigrationBackfill 迁移回填：模拟旧库（只有 wish_items 无 wish_wishers）→
// 跑迁移（NewWishService）→ 每条 wish_items 的发起人出现在 wish_wishers；重复跑不增重。
func TestWishersMigrationBackfill(t *testing.T) {
	dir, err := os.MkdirTemp("", "wishersmig")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := dir + "/wishpool.db"
	old, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open old: %v", err)
	}
	// 旧库：完整 wish_items（含 search_offset_minute），但无 wish_wishers 表。
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
			search_offset_minute INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`); err != nil {
		t.Fatalf("create old wish_items: %v", err)
	}
	now := time.Now()
	// 3 条存量：2 条 tmdb（含一条 tv+season、一条 media_type 空），1 条 imdb-only，外加 1 条无 id 脏行（应跳过）。
	rows := []struct {
		uid    int64
		tmdb   int
		imdb   string
		mt     string
		season int
	}{
		{11, 1000, "", "movie", 0},
		{12, 2000, "", "tv", 2},
		{13, 0, "tt500", "movie", 0},
		{14, 3000, "", "", 0},   // media_type 空，回填应归一 movie
		{15, 0, "", "movie", 0}, // 无 canonical key 脏行 → 应跳过
	}
	for i, r := range rows {
		if _, err := old.Exec(`INSERT INTO wish_items
			(user_id, tmdb_id, imdb_id, media_type, title, season, state, search_offset_minute, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			r.uid, r.tmdb, r.imdb, r.mt, "p", r.season, WishStateSearching, i, now, now); err != nil {
			t.Fatalf("insert old %d: %v", i, err)
		}
	}
	old.Close()

	// 跑迁移：NewWishService 应建 wish_wishers 并回填。
	ws, err := NewWishService(dir)
	if err != nil {
		t.Fatalf("NewWishService migrate: %v", err)
	}
	t.Cleanup(func() { ws.Close() })

	// 每条有 canonical key 的 wish_items 发起人都应出现在 wish_wishers。
	check := func(ck string, uid int64) {
		var c int
		if err := ws.db.QueryRow(
			`SELECT COUNT(*) FROM wish_wishers WHERE canonical_key=? AND user_id=?`, ck, uid,
		).Scan(&c); err != nil {
			t.Fatalf("query %s: %v", ck, err)
		}
		if c != 1 {
			t.Fatalf("回填缺失 canonical=%s user=%d, got %d", ck, uid, c)
		}
	}
	check(CanonicalKey(1000, "", "movie", 0), 11)
	check(CanonicalKey(2000, "", "tv", 2), 12)
	check(CanonicalKey(0, "tt500", "movie", 0), 13)
	check(CanonicalKey(3000, "", "movie", 0), 14) // media_type 空归一 movie 一致

	// 脏行（无 id）不应被回填。
	var total int
	if err := ws.db.QueryRow(`SELECT COUNT(*) FROM wish_wishers`).Scan(&total); err != nil {
		t.Fatalf("count total: %v", err)
	}
	if total != 4 {
		t.Fatalf("回填总数应为 4（脏行跳过）, got %d", total)
	}

	// 重复跑迁移不增重（幂等）。
	if err := ws.backfillWishers(); err != nil {
		t.Fatalf("re-backfill: %v", err)
	}
	var total2 int
	ws.db.QueryRow(`SELECT COUNT(*) FROM wish_wishers`).Scan(&total2)
	if total2 != 4 {
		t.Fatalf("重复回填增重, got %d", total2)
	}

	// 再开一次（模拟重启）也不增重。
	ws2, err := NewWishService(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer ws2.Close()
	var total3 int
	ws2.db.QueryRow(`SELECT COUNT(*) FROM wish_wishers`).Scan(&total3)
	if total3 != 4 {
		t.Fatalf("重启后回填增重, got %d", total3)
	}
}

// TestFilterWisherTargets 退群计数：可达过滤纯函数 —— 剔退群者 + 发起人，但计数不减。
func TestFilterWisherTargets(t *testing.T) {
	all := []int64{1, 2, 3, 4, 2} // 含发起人 1、重复的 2
	// 模拟 user 3 退群（不可达）。
	unreachable := map[int64]bool{3: true}
	reach := func(uid int64) bool { return !unreachable[uid] }

	got := filterWisherTargets(all, 1, reach)

	// 期望：剔除发起人 1、不可达 3、去重；剩 [2,4]。
	want := []int64{2, 4}
	if len(got) != len(want) {
		t.Fatalf("targets=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets[%d]=%d, want %d (got=%v)", i, got[i], want[i], got)
		}
	}

	// 关键：退群用户不从「总数」减——总数来自 wish_wishers，与过滤无关。
	// 这里用 CountWishers 验证「计数只升不降」。
	ws := newTestWishService(t)
	ck := CanonicalKey(999, "", "movie", 0)
	for _, uid := range []int64{1, 2, 3, 4} {
		if err := ws.AddWisher(ck, uid); err != nil {
			t.Fatalf("AddWisher %d: %v", uid, err)
		}
	}
	if n := ws.CountWishers(ck); n != 4 {
		t.Fatalf("总数应为 4（含退群者）, got %d", n)
	}
	// 即便 user 3 退群，CountWishers 仍 4（不减），过滤后通知目标不含 3。
	targets := filterWisherTargets(ws.ListWishers(ck), 1, reach)
	for _, uid := range targets {
		if uid == 3 {
			t.Fatalf("退群者 3 不应在通知目标里: %v", targets)
		}
	}
	if n := ws.CountWishers(ck); n != 4 {
		t.Fatalf("过滤后总数仍应为 4, got %d", n)
	}
}

// TestFilterWisherTargetsNilReach 可达函数为 nil 时，仅做剔发起人+去重（不剔任何人）。
func TestFilterWisherTargetsNilReach(t *testing.T) {
	got := filterWisherTargets([]int64{1, 2, 3, 0, 2}, 1, nil)
	want := []int64{2, 3}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}
