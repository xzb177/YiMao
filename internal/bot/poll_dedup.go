package bot

import (
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/dedupfile"
	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

const (
	// maxUpdateWorkers bounds how many Telegram updates are handled
	// concurrently. Without a cap a burst of updates spawns one goroutine each,
	// and every one of them may open MoviePilot/TMDB/Emby calls.
	maxUpdateWorkers = 20

	// processedUpdateTTL is how long a processed update_id is remembered. Telegram
	// redelivers an update until the offset is confirmed, so the window only has
	// to outlive one delivery retry cycle.
	processedUpdateTTL = 10 * time.Minute

	// persistentUpdateTTL 是持久化去重的窗口：进程内 10 分钟只能覆盖超时重投，
	// 崩溃重启后的重投可能隔很久；落盘后窗口延到 24 小时。
	persistentUpdateTTL = 24 * time.Hour

	// dedupSaveInterval 是账本落盘节流：每次 accept 都写盘太频繁，
	// 30 秒内最多写一次；崩溃丢失的至多是 30 秒内的记录（仍好过全丢）。
	dedupSaveInterval = 30 * time.Second

	// processedUpdatePruneInterval is how often expired update IDs are dropped.
	processedUpdatePruneInterval = 5 * time.Minute
)

// updateDeduper remembers recently processed Telegram update IDs so the same
// update is never handled twice. Telegram keys updates by int64 update_id and
// redelivers them until the offset is acknowledged; a redelivery must not run
// the handler again (duplicate replies, duplicate requests, duplicate quota).
type updateDeduper struct {
	mu   sync.Mutex
	seen map[int64]time.Time
	ttl  time.Duration
	now  func() time.Time
	// file != "" 时启用持久化：账本落盘，重启后仍能判重。
	file     string
	lastSave time.Time
}

func newUpdateDeduper(ttl time.Duration) *updateDeduper {
	if ttl <= 0 {
		ttl = processedUpdateTTL
	}
	return &updateDeduper{seen: make(map[int64]time.Time), ttl: ttl, now: time.Now}
}

// newPersistentUpdateDeduper 创建带落盘账本的去重器。dataDir == "" 时退化为
// 纯内存行为（测试/未配置场景）；账本文件不存在或损坏时只记日志
// （fail-open：宁可重复执行，不丢消息）。
func newPersistentUpdateDeduper(dataDir string, ttl time.Duration) *updateDeduper {
	d := newUpdateDeduper(ttl)
	if dataDir == "" {
		return d
	}
	dataFile := filepath.Join(dataDir, "tg_update_dedup.json")
	d.file = dataFile
	raw := dedupfile.Load(dataFile, ttl)
	for k, ts := range raw {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil {
			d.seen[id] = time.Unix(ts, 0)
		}
	}
	if n := len(d.seen); n > 0 {
		logger.Info("[Dedup] 从 %s 恢复 %d 条去重记录", dataFile, n)
	}
	return d
}

// saveLocked 把内存表写盘（调用方必须持有锁）。失败只记日志（fail-open）。
func (d *updateDeduper) saveLocked() {
	if d.file == "" {
		return
	}
	entries := make(map[string]int64, len(d.seen))
	for id, t := range d.seen {
		entries[strconv.FormatInt(id, 10)] = t.Unix()
	}
	if err := dedupfile.Save(d.file, entries); err != nil {
		logger.Info("[Dedup] 账本落盘失败 %s: %v（继续内存去重）", d.file, err)
		return
	}
	d.lastSave = d.now()
}

// accept records an update_id and reports whether it is new. A duplicate (or an
// id already seen inside the same batch) returns false and must be skipped.
func (d *updateDeduper) accept(updateID int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if seenAt, exists := d.seen[updateID]; exists && now.Sub(seenAt) < d.ttl {
		return false
	}
	d.seen[updateID] = now
	if d.file != "" && now.Sub(d.lastSave) >= dedupSaveInterval {
		d.saveLocked()
	}
	return true
}

// prune drops entries older than the TTL so the map cannot grow without bound.
func (d *updateDeduper) prune() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := d.now().Add(-d.ttl)
	removed := 0
	for id, seenAt := range d.seen {
		if seenAt.Before(cutoff) {
			delete(d.seen, id)
			removed++
		}
	}
	if removed > 0 {
		d.saveLocked() // 落盘账本同步清理，防无限膨胀
	}
	return removed
}

func (d *updateDeduper) size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seen)
}

// pruneProcessedUpdates periodically expires remembered update IDs.
func (d *updateDeduper) pruneProcessedUpdates(interval time.Duration) {
	if interval <= 0 {
		interval = processedUpdatePruneInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if removed := d.prune(); removed > 0 {
			logger.Info("[Poll] Cleaned up %d stale update IDs", removed)
		}
	}
}

// dispatchUpdates hands a batch of updates to handle, skipping duplicates and
// never running more than cap(sem) handlers at once. It returns the highest
// update_id in the batch so the caller can advance the polling offset only
// after every update in the batch has been dispatched.
func dispatchUpdates(
	updates []types.TelegramUpdate,
	dedup *updateDeduper,
	sem chan struct{},
	wg *sync.WaitGroup,
	handle func(types.TelegramUpdate),
) int64 {
	maxUpdateID := int64(0)
	for _, update := range updates {
		if update.UpdateID > maxUpdateID {
			maxUpdateID = update.UpdateID
		}
		if dedup != nil && !dedup.accept(update.UpdateID) {
			logger.Info("[Poll] Skipping duplicate update %d", update.UpdateID)
			continue
		}
		if sem != nil {
			sem <- struct{}{} // blocks once maxUpdateWorkers handlers are in flight
		}
		if wg != nil {
			wg.Add(1)
		}
		update := update // capture loop variable
		go func() {
			defer func() {
				if sem != nil {
					<-sem
				}
				if wg != nil {
					wg.Done()
				}
				if r := recover(); r != nil {
					logger.Info("[Poll] Panic while handling update %d: %v", update.UpdateID, r)
				}
			}()
			handle(update)
		}()
	}
	return maxUpdateID
}
