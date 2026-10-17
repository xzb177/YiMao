package services

import (
	"strings"
	"sync"
	"testing"
)

// 审计第 15 条：并发写报「数据库被锁」。
// 许愿池 32 并发里曾有 3 个失败（DEFERRED 事务读升级写触发 BUSY_SNAPSHOT）；
// 搜索历史库 64 并发里 60 个失败（没开 WAL、没设 busy_timeout）。
// 要求：修复后并发写零锁错误。

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "busy_snapshot") ||
		strings.Contains(msg, "database table is locked")
}

func TestWishConcurrentAddNoLockErrors(t *testing.T) {
	ws := newTestWishService(t)
	const n = 32
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := ws.AddWish(&WishItem{
				UserID:    int64(1000 + i),
				TmdbID:    900,
				MediaType: "movie",
				Title:     "并发片",
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	var lockErrs int
	var created, dups int
	for i, err := range errs {
		if err != nil {
			if isBusyError(err) {
				lockErrs++
				t.Logf("goroutine %d: %v", i, err)
			} else {
				t.Errorf("goroutine %d unexpected error: %v", i, err)
			}
			continue
		}
		// AddWish 的结果不直接返回，错误为 nil 即成功（新建或去重）。
		_ = created
		_ = dups
	}
	if lockErrs > 0 {
		t.Fatalf("database locked errors: %d/%d (audit item 15)", lockErrs, n)
	}

	// 语义检查：同一 canonical 最终只有一条活跃行，但 32 个用户都记进了 wisher。
	active, err := ws.ListForWisher(1000)
	if err != nil {
		t.Fatalf("ListForWisher: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected exactly 1 active wish for the canonical, got %d", len(active))
	}
	if n := ws.CountWishers(CanonicalKey(900, "", "movie", 0)); n != 32 {
		t.Fatalf("expected 32 wishers recorded, got %d", n)
	}
}

func TestSearchHistoryConcurrentNoLockErrors(t *testing.T) {
	dir := t.TempDir()
	db, err := NewSearchHistoryDB(dir)
	if err != nil {
		t.Fatalf("NewSearchHistoryDB: %v", err)
	}
	defer db.Close()

	const n = 64
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = db.AddSearch(int64(2000+i), "并发搜索词")
		}(i)
	}
	wg.Wait()

	var lockErrs int
	for i, err := range errs {
		if err != nil {
			if isBusyError(err) {
				lockErrs++
			} else {
				t.Errorf("goroutine %d unexpected error: %v", i, err)
			}
		}
	}
	if lockErrs > 0 {
		t.Fatalf("database locked errors: %d/%d (audit item 15)", lockErrs, n)
	}
}
