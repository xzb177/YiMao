package bot

import (
	"sync"
	"time"

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
}

func newUpdateDeduper(ttl time.Duration) *updateDeduper {
	if ttl <= 0 {
		ttl = processedUpdateTTL
	}
	return &updateDeduper{seen: make(map[int64]time.Time), ttl: ttl, now: time.Now}
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
