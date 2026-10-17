package services

import (
	"sync"
	"testing"
)

// 审计 A7③：并发写偏好时，落盘快照必须按变更顺序串行化——
// 旧快照不能盖掉新快照。压测后磁盘内容必须与内存一致。
func TestPreferencesConcurrentWritesStayOrdered(t *testing.T) {
	dir := t.TempDir()
	s := NewPreferencesService(dir)

	var wg sync.WaitGroup
	for u := int64(1); u <= 20; u++ {
		for k := 0; k < 20; k++ {
			wg.Add(1)
			go func(uid int64, i int) {
				defer wg.Done()
				_ = s.SetPreference(uid, PrefMovieNotification, i%2 == 0)
				_ = s.SetNotify(uid, NotifyDownload, i%2 == 1)
			}(u, k)
		}
	}
	wg.Wait()

	reloaded := NewPreferencesService(dir)
	for u := int64(1); u <= 20; u++ {
		mem := s.GetPreferences(u)
		disk := reloaded.GetPreferences(u)
		if mem.Movies != disk.Movies {
			t.Fatalf("用户 %d 磁盘与内存不一致：mem.Movies=%v disk.Movies=%v（旧快照盖掉了新快照）",
				u, mem.Movies, disk.Movies)
		}
		if (mem.NotifyDownload != nil) != (disk.NotifyDownload != nil) ||
			(mem.NotifyDownload != nil && *mem.NotifyDownload != *disk.NotifyDownload) {
			t.Fatalf("用户 %d NotifyDownload 磁盘与内存不一致", u)
		}
	}
}
