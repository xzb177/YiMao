package bot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xzb177/yimao/pkg/types"
)

// Telegram redelivers an update until the offset is acknowledged. The same
// update_id must never reach the handler twice, or the user gets duplicate
// replies and duplicate side effects (requests, quota).
func TestDispatchUpdatesSuppressesDuplicateUpdateIDs(t *testing.T) {
	dedup := newUpdateDeduper(processedUpdateTTL)
	sem := make(chan struct{}, maxUpdateWorkers)
	var wg sync.WaitGroup

	var mu sync.Mutex
	var handled []int64
	record := func(update types.TelegramUpdate) {
		mu.Lock()
		handled = append(handled, update.UpdateID)
		mu.Unlock()
	}

	// A batch that itself repeats 101, then the whole batch redelivered.
	batch := []types.TelegramUpdate{{UpdateID: 101}, {UpdateID: 102}, {UpdateID: 101}}
	maxID := dispatchUpdates(batch, dedup, sem, &wg, record)
	if maxID != 102 {
		t.Fatalf("maxUpdateID=%d, want 102 (highest id in batch)", maxID)
	}
	redeliverMax := dispatchUpdates(batch, dedup, sem, &wg, record)
	if redeliverMax != 102 {
		t.Fatalf("redelivered maxUpdateID=%d, want 102 so the offset still advances", redeliverMax)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(handled) != 2 {
		t.Fatalf("handled=%v, want each update_id handled exactly once", handled)
	}
	seen := map[int64]int{}
	for _, id := range handled {
		seen[id]++
	}
	if seen[101] != 1 || seen[102] != 1 {
		t.Fatalf("handled counts=%v, want 101 and 102 exactly once each", seen)
	}
}

// update_id is int64 on the wire. Values beyond int32 must round-trip without
// truncation, both for dedup keys and for the returned offset base.
func TestDispatchUpdatesKeepsInt64UpdateIDs(t *testing.T) {
	dedup := newUpdateDeduper(processedUpdateTTL)
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	large := int64(9_000_000_000)
	var got int64
	maxID := dispatchUpdates([]types.TelegramUpdate{{UpdateID: large}}, dedup, sem, &wg, func(u types.TelegramUpdate) {
		atomic.StoreInt64(&got, u.UpdateID)
	})
	wg.Wait()
	if maxID != large {
		t.Fatalf("maxUpdateID=%d, want %d", maxID, large)
	}
	if atomic.LoadInt64(&got) != large {
		t.Fatalf("handler saw update_id=%d, want %d", got, large)
	}
	if dedup.accept(large) {
		t.Fatal("large update_id was not remembered for dedup")
	}
}

// The semaphore must cap how many handlers run at once, no matter how big the
// batch is, so a burst cannot fan out into unbounded goroutines.
func TestDispatchUpdatesEnforcesConcurrencyCap(t *testing.T) {
	const limit = 3
	dedup := newUpdateDeduper(processedUpdateTTL)
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	var inFlight int64
	var peak int64
	release := make(chan struct{})
	handler := func(types.TelegramUpdate) {
		current := atomic.AddInt64(&inFlight, 1)
		for {
			observed := atomic.LoadInt64(&peak)
			if current <= observed || atomic.CompareAndSwapInt64(&peak, observed, current) {
				break
			}
		}
		<-release
		atomic.AddInt64(&inFlight, -1)
	}

	updates := make([]types.TelegramUpdate, 0, 25)
	for i := 1; i <= 25; i++ {
		updates = append(updates, types.TelegramUpdate{UpdateID: int64(i)})
	}

	done := make(chan int64, 1)
	go func() { done <- dispatchUpdates(updates, dedup, sem, &wg, handler) }()

	// Wait until the cap is saturated, then prove dispatch is blocked there.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&inFlight) < limit {
		if time.Now().After(deadline) {
			close(release)
			t.Fatalf("only %d handlers started, want the cap %d saturated", atomic.LoadInt64(&inFlight), limit)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&inFlight); got > limit {
		close(release)
		t.Fatalf("in-flight handlers=%d, want at most %d", got, limit)
	}
	select {
	case <-done:
		close(release)
		t.Fatal("dispatch finished while handlers were blocked; the cap is not enforced")
	default:
	}

	close(release)
	maxID := <-done
	wg.Wait()
	if maxID != 25 {
		t.Fatalf("maxUpdateID=%d, want 25", maxID)
	}
	if got := atomic.LoadInt64(&peak); got > limit {
		t.Fatalf("peak concurrency=%d, want at most %d", got, limit)
	}
	if atomic.LoadInt64(&inFlight) != 0 {
		t.Fatalf("handlers still in flight after wait: %d", atomic.LoadInt64(&inFlight))
	}
}

// A panicking handler must release its semaphore slot, otherwise the cap leaks
// and polling eventually deadlocks.
func TestDispatchUpdatesReleasesSlotOnPanic(t *testing.T) {
	dedup := newUpdateDeduper(processedUpdateTTL)
	sem := make(chan struct{}, 1)
	var wg sync.WaitGroup
	dispatchUpdates([]types.TelegramUpdate{{UpdateID: 1}}, dedup, sem, &wg, func(types.TelegramUpdate) {
		panic("boom")
	})
	wg.Wait()
	if len(sem) != 0 {
		t.Fatalf("semaphore still holds %d slots after a panicking handler", len(sem))
	}
	var ran bool
	dispatchUpdates([]types.TelegramUpdate{{UpdateID: 2}}, dedup, sem, &wg, func(types.TelegramUpdate) { ran = true })
	wg.Wait()
	if !ran {
		t.Fatal("dispatch blocked after a panicking handler: slot leaked")
	}
}

// Expired ids are pruned so the dedup map cannot grow without bound, and an id
// is accepted again once its TTL passed.
func TestUpdateDeduperPrunesExpiredIDs(t *testing.T) {
	dedup := newUpdateDeduper(time.Minute)
	base := time.Now()
	dedup.now = func() time.Time { return base }
	if !dedup.accept(1) || !dedup.accept(2) {
		t.Fatal("fresh update ids must be accepted")
	}
	if dedup.accept(1) {
		t.Fatal("update id 1 must be treated as duplicate inside the TTL")
	}
	if got := dedup.size(); got != 2 {
		t.Fatalf("dedup size=%d, want 2", got)
	}
	dedup.now = func() time.Time { return base.Add(2 * time.Minute) }
	if removed := dedup.prune(); removed != 2 {
		t.Fatalf("pruned=%d, want 2 expired ids removed", removed)
	}
	if got := dedup.size(); got != 0 {
		t.Fatalf("dedup size after prune=%d, want 0", got)
	}
	if !dedup.accept(1) {
		t.Fatal("after the TTL expired the id must be accepted again")
	}
}
