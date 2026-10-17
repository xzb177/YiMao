package services

import (
	"testing"
	"time"
)

// 审计 A7①：昨天扣的配额今天退，不能扣减今天的已用量——否则今天可用"上限+退回"次。
func TestCrossDayRefundDoesNotExceedDailyLimit(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	const user = int64(21)

	// 今天已用满 5 次（上限）。
	for i := 0; i < 5; i++ {
		if err := quota.UseMovieQuota(user); err != nil {
			t.Fatalf("第 %d 次扣配额失败: %v", i+1, err)
		}
	}

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	restored, err := quota.RestoreQuotaForRequest("crossday-1", user, "movie", 1, yesterday)
	if err != nil || !restored {
		t.Fatalf("跨天退应记账成功: restored=%v err=%v", restored, err)
	}
	if got := quota.GetQuotaInfo(user).MovieUsed; got != 5 {
		t.Fatalf("跨天退后今天已用量应仍为 5（上限），实际 %d", got)
	}
	// 同一 request_id 重试应幂等。
	if restored, _ := quota.RestoreQuotaForRequest("crossday-1", user, "movie", 1, yesterday); restored {
		t.Fatal("重复退应幂等返回 false")
	}
}

// 同一天的退配额保持老行为：已用量扣减。
func TestSameDayRefundStillRestores(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	const user = int64(22)
	if err := quota.UseMovieQuota(user); err != nil {
		t.Fatal(err)
	}
	if _, err := quota.RestoreQuotaForRequest("sameday-1", user, "movie", 1, getCurrentDate()); err != nil {
		t.Fatal(err)
	}
	if got := quota.GetQuotaInfo(user).MovieUsed; got != 0 {
		t.Fatalf("当天退后已用量应为 0，实际 %d", got)
	}
}
