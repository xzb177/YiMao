package services

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// 审计第 13 条：许愿过期/完成后，同一部片再也许不了。
// 唯一索引没有带状态条件，终态的行会报 UNIQUE 冲突。
func TestWishRewishAfterTerminal(t *testing.T) {
	ws := newTestWishService(t)

	// 入池 → 走完流程到终态 FULFILLED。
	wish := &WishItem{UserID: 1, TmdbID: 500, MediaType: "movie", Title: "片T"}
	res, err := ws.AddWish(wish)
	if err != nil || !res.Created {
		t.Fatalf("AddWish: res=%+v err=%v", res, err)
	}
	itemID := wish.ID
	// PENDING→SEARCHING：把错峰分钟调到当前分钟再认领。
	nowMinute := (wish.CreatedAt.Hour()*60 + wish.CreatedAt.Minute()) % 1440
	if _, err := ws.db.Exec(`UPDATE wish_items SET search_offset_minute = ? WHERE id = ?`, nowMinute, itemID); err != nil {
		t.Fatalf("set offset: %v", err)
	}
	claimed, err := ws.ClaimSearchableItems(nowMinute, 60, 10)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim: claimed=%d err=%v", len(claimed), err)
	}
	if changed, err := ws.MarkFound(itemID, "site:abc"); err != nil || !changed {
		t.Fatalf("MarkFound: changed=%v err=%v", changed, err)
	}
	if changed, err := ws.MarkNotified(itemID); err != nil || !changed {
		t.Fatalf("MarkNotified: changed=%v err=%v", changed, err)
	}
	if changed, err := ws.MarkFulfilled(itemID); err != nil || !changed {
		t.Fatalf("MarkFulfilled: changed=%v err=%v", changed, err)
	}

	// 同一部片再许一次：终态行不应再占唯一键，必须能新建。
	res, err = ws.AddWish(&WishItem{UserID: 2, TmdbID: 500, MediaType: "movie", Title: "片T"})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			t.Fatalf("re-wish after terminal hit UNIQUE constraint (audit item 13): %v", err)
		}
		t.Fatalf("AddWish re-wish: %v", err)
	}
	if !res.Created {
		t.Fatalf("re-wish after terminal should create a new item, got %+v", res)
	}

	// 但活跃条目仍被唯一键保护：同时只能有一个活跃的同 canonical。
	res, err = ws.AddWish(&WishItem{UserID: 3, TmdbID: 500, MediaType: "movie", Title: "片T"})
	if err != nil {
		t.Fatalf("duplicate check on active item should be handled as Duplicate, not error: %v", err)
	}
	if !res.Duplicate {
		t.Fatalf("active duplicate should return Duplicate, got %+v", res)
	}
}

// openRawWishDB 打开测试用的裸 SQLite 连接（构造旧结构库）。
func openRawWishDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dir+"/wishpool.db")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func execSQL(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

// 迁移测试：用「旧结构」（全量唯一索引 + 终态数据）建库，再跑 NewWishService。
// 要求：升级不报错、终态数据不丢、之后能对同 canonical 重新许愿。
func TestWishMigrationStateAwareUniqueIndex(t *testing.T) {
	dir := t.TempDir()

	// 手工建一个旧库：全量唯一索引 + 一条终态(FULFILLED)行 + 一条活跃行。
	db := openRawWishDB(t, dir)
	execSQL(t, db, `
		CREATE TABLE wish_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL, tmdb_id INTEGER NOT NULL DEFAULT 0,
			imdb_id TEXT NOT NULL DEFAULT '', media_type TEXT NOT NULL DEFAULT 'movie',
			title TEXT NOT NULL DEFAULT '', year INTEGER NOT NULL DEFAULT 0,
			season INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'PENDING',
			found_detail TEXT NOT NULL DEFAULT '', searching_at DATETIME, notified_at DATETIME,
			search_offset_minute INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
		);
		CREATE UNIQUE INDEX idx_wish_canonical_tmdb ON wish_items(tmdb_id, media_type, season) WHERE tmdb_id != 0;
		CREATE UNIQUE INDEX idx_wish_canonical_imdb ON wish_items(imdb_id, media_type, season) WHERE tmdb_id = 0 AND imdb_id != '';
		INSERT INTO wish_items (user_id, tmdb_id, media_type, title, state, created_at, updated_at)
			VALUES (1, 600, 'movie', '旧终态片', 'FULFILLED', datetime('now'), datetime('now'));
		INSERT INTO wish_items (user_id, tmdb_id, media_type, title, state, created_at, updated_at)
			VALUES (2, 601, 'movie', '旧活跃片', 'SEARCHING', datetime('now'), datetime('now'));
	`)
	db.Close()

	// 升级：绝不能报错。
	ws, err := NewWishService(dir)
	if err != nil {
		t.Fatalf("NewWishService on legacy DB: %v", err)
	}
	defer ws.Close()

	// 存量数据都在。
	got, err := ws.GetByID(1)
	if err != nil || got == nil || got.State != WishStateFulfilled || got.Title != "旧终态片" {
		t.Fatalf("legacy terminal row lost: %+v err=%v", got, err)
	}
	got, err = ws.GetByID(2)
	if err != nil || got == nil || got.State != WishStateSearching {
		t.Fatalf("legacy active row lost: %+v err=%v", got, err)
	}

	// 终态片可以重新许愿。
	res, err := ws.AddWish(&WishItem{UserID: 3, TmdbID: 600, MediaType: "movie", Title: "旧终态片"})
	if err != nil || !res.Created {
		t.Fatalf("re-wish of migrated terminal item: res=%+v err=%v", res, err)
	}

	// 活跃片重复许愿仍是 Duplicate（唯一性仍在）。
	res, err = ws.AddWish(&WishItem{UserID: 4, TmdbID: 601, MediaType: "movie", Title: "旧活跃片"})
	if err != nil || !res.Duplicate {
		t.Fatalf("active duplicate after migration: res=%+v err=%v", res, err)
	}
}
