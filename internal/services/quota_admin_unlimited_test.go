package services

import (
	"testing"
)

// 审计第 16 条：管理员「无限配额」在扣减时不生效。
// CanRequest 返回 true，但第 6 部提交时 UseMovieQuota 报配额超限。
// 要求：扣减路径与检查路径用同一套无限配额判定。
func TestAdminUnlimitedQuotaAppliesOnUse(t *testing.T) {
	dir := t.TempDir()
	s := NewQuotaService(dir, nil)

	const adminID int64 = 9001
	s.SetAdminIDs([]int64{adminID})

	if !s.CanRequest(adminID, "movie") {
		t.Fatalf("admin CanRequest(movie) = false, want true")
	}

	// 连续提交远超普通上限（默认 5）：管理员必须全部成功。
	for i := 0; i < 12; i++ {
		if err := s.UseMovieQuota(adminID); err != nil {
			t.Fatalf("admin UseMovieQuota #%d failed: %v (unlimited quota not honored on deduct path)", i+1, err)
		}
	}
	if !s.CanRequest(adminID, "movie") {
		t.Fatalf("admin CanRequest(movie) = false after 12 uses, want true")
	}
}

func TestAdminUnlimitedQuotaAppliesOnUseTV(t *testing.T) {
	dir := t.TempDir()
	s := NewQuotaService(dir, nil)

	const adminID int64 = 9002
	s.SetAdminIDs([]int64{adminID})

	if !s.CanRequest(adminID, "tv") {
		t.Fatalf("admin CanRequest(tv) = false, want true")
	}

	// 剧集每部扣 3，默认上限 5：普通用户第 2 部就超限，管理员必须能一直扣。
	for i := 0; i < 6; i++ {
		if err := s.UseTVQuota(adminID); err != nil {
			t.Fatalf("admin UseTVQuota #%d failed: %v (unlimited quota not honored on deduct path)", i+1, err)
		}
	}
}
