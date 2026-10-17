package services

import (
	"os"
	"path/filepath"
	"testing"
)

// 以下测试定义拼车按季区分的新语义（审计 A7② 单独立项）：
//  - 剧集拼车 key 带季号：tv:<id>:s<season>；season=0 表示任意季
//  - 电影不受影响（key 不变，season 被忽略）
//  - 到货通知只匹配同季 + 任意季，其他季保留
//  - 历史数据（旧 key tv:<id> 无季后缀）加载后视为任意季

func newCarpoolTestService(t *testing.T) *CarpoolService {
	t.Helper()
	return NewCarpoolService(t.TempDir())
}

// TestCarpoolSeasonIsolatedKeys 分季隔离：同剧不同季是独立拼车池。
func TestCarpoolSeasonIsolatedKeys(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(100, "tv", 1, 111)
	s.Add(100, "tv", 2, 222)

	if got := s.Get(100, "tv", 1); len(got) != 1 || got[0] != 111 {
		t.Fatalf("S1 成员=%v，想要 [111]", got)
	}
	if got := s.Get(100, "tv", 2); len(got) != 1 || got[0] != 222 {
		t.Fatalf("S2 成员=%v，想要 [222]", got)
	}
	if got := s.Get(100, "tv", 0); len(got) != 0 {
		t.Fatalf("任意季成员=%v，想要空", got)
	}
	// 同一用户可对同一剧的不同季分别拼车
	s.Add(100, "tv", 2, 111)
	if !s.Contains(100, "tv", 1, 111) || !s.Contains(100, "tv", 2, 111) {
		t.Fatalf("同一用户应可同时拼 S1 和 S2")
	}
}

// TestCarpoolSameSeasonIdempotent 同季重复加入幂等。
func TestCarpoolSameSeasonIdempotent(t *testing.T) {
	s := newCarpoolTestService(t)
	if n := s.Add(100, "tv", 1, 111); n != 1 {
		t.Fatalf("首次加入人数=%d，想要 1", n)
	}
	if n := s.Add(100, "tv", 1, 111); n != 1 {
		t.Fatalf("重复加入人数=%d，想要仍为 1", n)
	}
}

// TestCarpoolMovieIgnoresSeason 电影不受季影响：key 保持旧格式。
func TestCarpoolMovieIgnoresSeason(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(200, "movie", 5, 111)
	if !s.Contains(200, "movie", 0, 111) {
		t.Fatalf("电影用 season=5 加入后，应能用 season=0 查到")
	}
	if got := s.Get(200, "movie", 0); len(got) != 1 {
		t.Fatalf("电影成员=%v，想要 1 人", got)
	}
}

// TestCarpoolArrivalNotifiesMatchedSeasonsOnly 到货通知语义：
// S1 到货 → 通知并结清 S1 + 任意季(0)；S2 的人保留不动。
func TestCarpoolArrivalNotifiesMatchedSeasonsOnly(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(100, "tv", 1, 111) // S1
	s.Add(100, "tv", 2, 222) // S2
	s.Add(100, "tv", 0, 333) // 任意季

	notified := s.GetAndClearArrival(100, "tv", 1)
	if len(notified) != 2 {
		t.Fatalf("S1 到货应通知 2 人（S1+任意季），实际 %v", notified)
	}
	if got := s.Get(100, "tv", 2); len(got) != 1 || got[0] != 222 {
		t.Fatalf("S1 到货后 S2 成员应保留，实际 %v", got)
	}
	if got := s.Get(100, "tv", 0); len(got) != 0 {
		t.Fatalf("S1 到货后任意季应已结清，实际 %v", got)
	}
	if got := s.Get(100, "tv", 1); len(got) != 0 {
		t.Fatalf("S1 到货后 S1 应已结清，实际 %v", got)
	}
}

// TestCarpoolArrivalUnknownSeasonIsAnySeason 季号未知（0）的到货：
// 只通知任意季成员，指定季成员保留（等该季真正到货）。
func TestCarpoolArrivalUnknownSeasonIsAnySeason(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(100, "tv", 1, 111)
	s.Add(100, "tv", 0, 333)

	notified := s.GetAndClearArrival(100, "tv", 0)
	if len(notified) != 1 || notified[0] != 333 {
		t.Fatalf("未知季到货应只通知任意季成员，实际 %v", notified)
	}
	if got := s.Get(100, "tv", 1); len(got) != 1 {
		t.Fatalf("未知季到货后 S1 成员应保留，实际 %v", got)
	}
}

// TestCarpoolCancelClearsMatchedSeasonsOnly 取消语义：
// 取消 S1 → 清 S1 + 任意季；取消整部（season=0）→ 全清。
func TestCarpoolCancelClearsMatchedSeasonsOnly(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(100, "tv", 1, 111)
	s.Add(100, "tv", 2, 222)
	s.Add(100, "tv", 0, 333)

	canceled := s.ClearArrival(100, "tv", 1)
	if len(canceled) != 2 {
		t.Fatalf("取消 S1 应影响 2 人，实际 %v", canceled)
	}
	if got := s.Get(100, "tv", 2); len(got) != 1 {
		t.Fatalf("取消 S1 后 S2 应保留，实际 %v", got)
	}

	s.Add(100, "tv", 0, 444)
	all := s.ClearArrival(100, "tv", 0)
	if len(all) != 2 { // S2 的 222 + 任意季的 444
		t.Fatalf("整部取消应影响 2 人，实际 %v", all)
	}
	if got := s.Get(100, "tv", 2); len(got) != 0 {
		t.Fatalf("整部取消后 S2 应清空，实际 %v", got)
	}
}

// TestCarpoolLegacyKeyLoadsAsAnySeason 历史数据兼容：
// 旧格式 key（tv:<id> 无季后缀）加载后视为任意季，能被任意季到货通知到。
func TestCarpoolLegacyKeyLoadsAsAnySeason(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"entries":{"tv:100":[111,222],"movie:200":[333]},"metadata":{}}`
	if err := os.WriteFile(filepath.Join(dir, "carpool.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewCarpoolService(dir)

	if !s.Contains(100, "tv", 0, 111) {
		t.Fatalf("旧 key tv:100 应被视为任意季")
	}
	notified := s.GetAndClearArrival(100, "tv", 1)
	if len(notified) != 2 {
		t.Fatalf("S1 到货应通知到旧数据的 2 人，实际 %v", notified)
	}
	if !s.Contains(200, "movie", 0, 333) {
		t.Fatalf("电影旧数据应不受影响")
	}
}

// TestCarpoolListForUserExposesSeason 列表带季号展示。
func TestCarpoolListForUserExposesSeason(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(100, "tv", 2, 111)
	s.Add(200, "movie", 0, 111)

	items := s.ListForUser(111)
	byID := map[int]CarpoolItem{}
	for _, it := range items {
		byID[it.TMDBID] = it
	}
	if byID[100].Season != 2 {
		t.Fatalf("剧集条目 Season=%d，想要 2", byID[100].Season)
	}
	if byID[200].Season != 0 {
		t.Fatalf("电影条目 Season=%d，想要 0", byID[200].Season)
	}
}

// TestCarpoolRemoveIsSeasonScoped 移除只删指定季。
func TestCarpoolRemoveIsSeasonScoped(t *testing.T) {
	s := newCarpoolTestService(t)
	s.Add(100, "tv", 1, 111)
	s.Add(100, "tv", 2, 111)
	if !s.Remove(100, "tv", 1, 111) {
		t.Fatalf("移除 S1 应返回 true")
	}
	if s.Contains(100, "tv", 1, 111) {
		t.Fatalf("S1 应已被移除")
	}
	if !s.Contains(100, "tv", 2, 111) {
		t.Fatalf("S2 应保留")
	}
}

// TestCarpoolSeasonLabel 季号展示文案。
func TestCarpoolSeasonLabel(t *testing.T) {
	if got := seasonLabel(0); got != "任意季" {
		t.Fatalf("seasonLabel(0)=%q，想要 任意季", got)
	}
	if got := seasonLabel(2); got != "第2季" {
		t.Fatalf("seasonLabel(2)=%q，想要 第2季", got)
	}
}
