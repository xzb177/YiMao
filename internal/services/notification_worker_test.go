package services

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestNotificationService(t *testing.T) *NotificationService {
	t.Helper()
	s := NewNotificationService(nil, nil, nil, nil, t.TempDir())
	s.resolveUser = func(int) int64 { return 42 }
	t.Cleanup(s.StopNotificationWorker)
	return s
}

func status(id int, state string) *SubscribeStatus {
	return &SubscribeStatus{ID: id, Name: "movie", State: state}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestNotifyStatusUpdateReturnsWithoutSending(t *testing.T) {
	s := newTestNotificationService(t)
	blocked := make(chan struct{})
	s.sendStatus = func(int64, string) error { <-blocked; return nil }
	s.StartNotificationWorker()

	start := time.Now()
	if err := s.NotifyStatusUpdate(1, status(1, StateDownloading)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("NotifyStatusUpdate blocked for %s", elapsed)
	}
	close(blocked)
}

func TestConcurrentIdenticalNotifyUpsertIsAtomicAndDeduplicated(t *testing.T) {
	s := newTestNotificationService(t)
	const count = 40
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.NotifyStatusUpdate(7, status(7, "state")); err != nil {
				t.Errorf("notify: %v", err)
			}
		}(i)
	}
	wg.Wait()

	updates, err := s.LoadStatusUpdates()
	if err != nil {
		t.Fatalf("load valid JSON: %v", err)
	}
	if len(updates) != 1 || updates[0].Revision != 1 {
		t.Fatalf("updates = %#v, want one update at revision 1", updates)
	}
}

func TestChangedStatusAdvancesRevision(t *testing.T) {
	s := newTestNotificationService(t)
	if err := s.NotifyStatusUpdate(1, status(1, StateDownloading)); err != nil {
		t.Fatal(err)
	}
	if err := s.NotifyStatusUpdate(1, status(1, StateCompleted)); err != nil {
		t.Fatal(err)
	}
	updates, err := s.LoadStatusUpdates()
	if err != nil || len(updates) != 1 || updates[0].Revision != 2 {
		t.Fatalf("updates=%#v err=%v, want revision 2", updates, err)
	}
}

func TestFailedSendRemainsPendingAndRetries(t *testing.T) {
	s := newTestNotificationService(t)
	var calls atomic.Int32
	s.sendStatus = func(int64, string) error {
		if calls.Add(1) == 1 {
			return errors.New("temporary")
		}
		return nil
	}
	if err := s.NotifyStatusUpdate(1, status(1, StateDownloading)); err != nil {
		t.Fatal(err)
	}
	s.StartNotificationWorker()
	waitFor(t, func() bool { return calls.Load() >= 1 })
	updates, err := s.LoadStatusUpdates()
	if err != nil || len(updates) != 1 || updates[0].Notified {
		t.Fatalf("failed update must remain pending: %#v, %v", updates, err)
	}
	s.signalWorker()
	waitFor(t, func() bool {
		u, err := s.LoadStatusUpdates()
		return err == nil && len(u) == 1 && u[0].Notified
	})
}

func TestOldRevisionCannotMarkNewRevisionNotified(t *testing.T) {
	s := newTestNotificationService(t)
	sent := make(chan struct{})
	release := make(chan struct{})
	s.sendStatus = func(int64, string) error {
		close(sent)
		<-release
		return nil
	}
	if err := s.NotifyStatusUpdate(1, status(1, StateDownloading)); err != nil {
		t.Fatal(err)
	}
	s.StartNotificationWorker()
	<-sent
	if err := s.NotifyStatusUpdate(1, status(1, StateCompleted)); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitFor(t, func() bool {
		u, err := s.LoadStatusUpdates()
		return err == nil && len(u) == 1 && u[0].Revision == 2
	})
	u, _ := s.LoadStatusUpdates()
	if u[0].Notified {
		t.Fatal("old send marked newer revision notified")
	}
}

func TestStartStopNotificationWorkerAreIdempotent(t *testing.T) {
	s := newTestNotificationService(t)
	var active atomic.Int32
	var maxActive atomic.Int32
	s.sendStatus = func(int64, string) error {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maxActive.Load()
			if n <= old || maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return errors.New("retry")
	}
	if err := s.NotifyStatusUpdate(1, status(1, StateDownloading)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		s.StartNotificationWorker()
	}
	waitFor(t, func() bool { return maxActive.Load() > 0 })
	if maxActive.Load() != 1 {
		t.Fatalf("multiple workers sent concurrently: %d", maxActive.Load())
	}
	for i := 0; i < 10; i++ {
		s.StopNotificationWorker()
	}
	s.StartNotificationWorker()
	s.StopNotificationWorker()
}
