package services

import (
	"testing"
	"time"
)

// 审计第 14 条：许愿状态机死状态。
// (a) FOUND 通知失败后没有任何路径会重试 —— 需要重试扫瞄捡回；
// (b) NOTIFIED 没有 TTL —— 需要到期清理；
// (c) 发起人撤回连带作废所有跟随者 —— 需要只撤自己、单子保留。

// makeFoundItem 建一条走到 FOUND 的条目。
func makeFoundItem(t *testing.T, ws *WishService, userID int64, tmdbID int) *WishItem {
	t.Helper()
	wish := &WishItem{UserID: userID, TmdbID: tmdbID, MediaType: "movie", Title: "死状态片"}
	res, err := ws.AddWish(wish)
	if err != nil || !res.Created {
		t.Fatalf("AddWish: res=%+v err=%v", res, err)
	}
	nowMinute := (wish.CreatedAt.Hour()*60 + wish.CreatedAt.Minute()) % 1440
	if _, err := ws.db.Exec(`UPDATE wish_items SET search_offset_minute = ? WHERE id = ?`, nowMinute, wish.ID); err != nil {
		t.Fatalf("set offset: %v", err)
	}
	if _, err := ws.ClaimSearchableItems(nowMinute, 60, 10); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if changed, err := ws.MarkFound(wish.ID, "site:x"); err != nil || !changed {
		t.Fatalf("MarkFound: changed=%v err=%v", changed, err)
	}
	return wish
}

func backdateUpdatedAt(t *testing.T, ws *WishService, id int64, d time.Duration) {
	t.Helper()
	if _, err := ws.db.Exec(`UPDATE wish_items SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-d), id); err != nil {
		t.Fatalf("backdate: %v", err)
	}
}

func TestListStaleFoundItems(t *testing.T) {
	ws := newTestWishService(t)
	stale := makeFoundItem(t, ws, 1, 700)
	fresh := makeFoundItem(t, ws, 2, 701)
	backdateUpdatedAt(t, ws, stale.ID, 2*time.Hour)

	items, err := ws.ListStaleFoundItems(30 * time.Minute)
	if err != nil {
		t.Fatalf("ListStaleFoundItems: %v", err)
	}
	seen := map[int64]bool{}
	for _, it := range items {
		seen[it.ID] = true
	}
	if !seen[stale.ID] {
		t.Fatalf("stale FOUND item %d not returned for retry", stale.ID)
	}
	if seen[fresh.ID] {
		t.Fatalf("fresh FOUND item %d should not be retried yet", fresh.ID)
	}
}

func TestExpireStaleNotified(t *testing.T) {
	ws := newTestWishService(t)
	stale := makeFoundItem(t, ws, 1, 710)
	fresh := makeFoundItem(t, ws, 2, 711)
	if _, err := ws.db.Exec(`UPDATE wish_items SET state = ?, notified_at = ? WHERE id = ?`,
		WishStateNotified, time.Now().Add(-100*time.Hour), stale.ID); err != nil {
		t.Fatalf("setup stale: %v", err)
	}
	if _, err := ws.db.Exec(`UPDATE wish_items SET state = ?, notified_at = ? WHERE id = ?`,
		WishStateNotified, time.Now(), fresh.ID); err != nil {
		t.Fatalf("setup fresh: %v", err)
	}

	n, err := ws.ExpireStaleNotified(72 * time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleNotified: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 expired, got %d", n)
	}
	got, _ := ws.GetByID(stale.ID)
	if got == nil || got.State != WishStateExpired {
		t.Fatalf("stale NOTIFIED should be EXPIRED, got %+v", got)
	}
	got, _ = ws.GetByID(fresh.ID)
	if got == nil || got.State != WishStateNotified {
		t.Fatalf("fresh NOTIFIED should stay NOTIFIED, got %+v", got)
	}
}

func addExtraWishers(t *testing.T, ws *WishService, wish *WishItem, users ...int64) {
	t.Helper()
	ck := CanonicalKey(wish.TmdbID, wish.ImdbID, wish.MediaType, wish.Season)
	for _, u := range users {
		if err := ws.AddWisher(ck, u); err != nil {
			t.Fatalf("AddWisher: %v", err)
		}
	}
}

func wisherIDs(t *testing.T, ws *WishService, wish *WishItem) []int64 {
	t.Helper()
	ck := CanonicalKey(wish.TmdbID, wish.ImdbID, wish.MediaType, wish.Season)
	return ws.ListWishers(ck)
}

func TestRevokeWishTransfersToNextWisher(t *testing.T) {
	ws := newTestWishService(t)
	wish := &WishItem{UserID: 1, TmdbID: 720, MediaType: "movie", Title: "众筹片"}
	if res, err := ws.AddWish(wish); err != nil || !res.Created {
		t.Fatalf("AddWish: res=%+v err=%v", res, err)
	}
	addExtraWishers(t, ws, wish, 2, 3)

	outcome, err := ws.RevokeWish(1, wish.ID)
	if err != nil {
		t.Fatalf("RevokeWish: %v", err)
	}
	if outcome != RevokeTransferred {
		t.Fatalf("expected RevokeTransferred, got %v", outcome)
	}

	// 单子保留：发起人转给最早的跟随者（user 2），状态不变。
	got, _ := ws.GetByID(wish.ID)
	if got == nil || got.State != WishStatePending || got.UserID != 2 {
		t.Fatalf("wish should stay active with new originator=2, got %+v", got)
	}
	// 撤回者的 wisher 行被删掉。
	for _, u := range wisherIDs(t, ws, wish) {
		if u == 1 {
			t.Fatalf("revoker should be removed from wishers, got %v", wisherIDs(t, ws, wish))
		}
	}
}

func TestRevokeWishFollowerLeaves(t *testing.T) {
	ws := newTestWishService(t)
	wish := &WishItem{UserID: 1, TmdbID: 721, MediaType: "movie", Title: "众筹片2"}
	if res, err := ws.AddWish(wish); err != nil || !res.Created {
		t.Fatalf("AddWish: res=%+v err=%v", res, err)
	}
	addExtraWishers(t, ws, wish, 2, 3)

	outcome, err := ws.RevokeWish(2, wish.ID)
	if err != nil {
		t.Fatalf("RevokeWish: %v", err)
	}
	if outcome != RevokeSelfWithdrawn {
		t.Fatalf("expected RevokeSelfWithdrawn, got %v", outcome)
	}
	got, _ := ws.GetByID(wish.ID)
	if got == nil || got.State != WishStatePending || got.UserID != 1 {
		t.Fatalf("wish should stay active with originator=1, got %+v", got)
	}
	for _, u := range wisherIDs(t, ws, wish) {
		if u == 2 {
			t.Fatalf("follower should be removed from wishers")
		}
	}
}

func TestRevokeWishLastWisherExpires(t *testing.T) {
	ws := newTestWishService(t)
	wish := &WishItem{UserID: 1, TmdbID: 722, MediaType: "movie", Title: "独苗片"}
	if res, err := ws.AddWish(wish); err != nil || !res.Created {
		t.Fatalf("AddWish: res=%+v err=%v", res, err)
	}

	outcome, err := ws.RevokeWish(1, wish.ID)
	if err != nil {
		t.Fatalf("RevokeWish: %v", err)
	}
	if outcome != RevokeCancelled {
		t.Fatalf("expected RevokeCancelled, got %v", outcome)
	}
	got, _ := ws.GetByID(wish.ID)
	if got == nil || got.State != WishStateExpired {
		t.Fatalf("last wisher revoke should EXPIRE the item, got %+v", got)
	}
}

func TestRevokeWishNonWisherRejected(t *testing.T) {
	ws := newTestWishService(t)
	wish := &WishItem{UserID: 1, TmdbID: 723, MediaType: "movie", Title: "别人的片"}
	if res, err := ws.AddWish(wish); err != nil || !res.Created {
		t.Fatalf("AddWish: res=%+v err=%v", res, err)
	}

	if _, err := ws.RevokeWish(99, wish.ID); err == nil {
		t.Fatalf("non-wisher revoke should be rejected")
	}
	got, _ := ws.GetByID(wish.ID)
	if got == nil || got.State != WishStatePending {
		t.Fatalf("item should be untouched, got %+v", got)
	}
}
