package services

import (
	"testing"
	"time"
)

func TestFulfillmentEstimateNeedsMinimumSamples(t *testing.T) {
	s := NewFulfillmentStatsService(t.TempDir())
	s.AddSample("movie", 2026, 600)
	s.AddSample("movie", 2026, 700)
	if _, _, ok := s.Estimate("movie", 2026); ok {
		t.Fatal("2 samples should not produce an estimate")
	}
	s.AddSample("movie", 2026, 800)
	d, n, ok := s.Estimate("movie", 2026)
	if !ok || n != 3 || d != 700*time.Second {
		t.Fatalf("estimate=%v n=%d ok=%v", d, n, ok)
	}
}

func TestFulfillmentEstimatePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	s := NewFulfillmentStatsService(dir)
	for i := 0; i < 5; i++ {
		s.AddSample("tv", 2020, int64(3600*(i+1)))
	}
	reloaded := NewFulfillmentStatsService(dir)
	if _, n, ok := reloaded.Estimate("tv", 2020); !ok || n != 5 {
		t.Fatalf("reloaded estimate n=%d ok=%v", n, ok)
	}
}

func TestFulfillmentEstimateTextAndBuckets(t *testing.T) {
	s := NewFulfillmentStatsService(t.TempDir())
	// 旧片桶：3 条 2 天级样本
	for i := 0; i < 3; i++ {
		s.AddSample("movie", 2001, 2*24*3600)
	}
	// 新片桶：3 条 30 分钟级样本
	for i := 0; i < 3; i++ {
		s.AddSample("movie", time.Now().Year(), 1800)
	}
	dOld, _, _ := s.Estimate("movie", 2000)
	dNew, _, _ := s.Estimate("movie", time.Now().Year())
	if dOld <= dNew {
		t.Fatalf("old-title bucket should be slower: old=%v new=%v", dOld, dNew)
	}
	if txt := s.EstimateText("movie", time.Now().Year()); txt == "" {
		t.Fatal("estimate text should not be empty with enough samples")
	}
	if txt := s.EstimateText("tv", 2026); txt != "" {
		t.Fatalf("no tv samples but got text %q", txt)
	}
}

func TestWatchFeedbackRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewFulfillmentStatsService(dir)
	s.AddWatchFeedback("req-1", "w")
	s.AddWatchFeedback("req-2", "l")
	s.AddWatchFeedback("req-3", "w")
	counts := NewFulfillmentStatsService(dir).WatchFeedbackCounts()
	if counts["w"] != 2 || counts["l"] != 1 {
		t.Fatalf("counts=%v", counts)
	}
}

func TestCompletionLedgerAndStaleTitles(t *testing.T) {
	dir := t.TempDir()
	s := NewFulfillmentStatsService(dir)
	s.AddCompletion(CompletionRecord{RequestID: "old", Title: "旧片", TelegramID: 1, CompletedAt: time.Now().Add(-100 * 24 * time.Hour)})
	s.AddCompletion(CompletionRecord{RequestID: "new", Title: "新片", TelegramID: 1, CompletedAt: time.Now().Add(-2 * 24 * time.Hour)})
	s.AddWatchFeedbackTitled("old", "旧片", "l")
	s.AddWatchFeedbackTitled("new", "新片", "w")
	got := s.StaleUnwatchedTitles(90, 10)
	if len(got) != 1 || got[0] != "旧片" {
		t.Fatalf("stale titles=%v", got)
	}
	if reloaded := NewFulfillmentStatsService(dir).StaleUnwatchedTitles(90, 10); len(reloaded) != 1 || reloaded[0] != "旧片" {
		t.Fatalf("reloaded stale titles=%v", reloaded)
	}
}

func TestHumanizeFulfillment(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "几分钟",
		25 * time.Minute: "25 分钟",
		3 * time.Hour:    "3 小时",
		72 * time.Hour:   "3 天",
	}
	for d, want := range cases {
		if got := humanizeFulfillment(d); got != want {
			t.Errorf("%v: got %q want %q", d, got, want)
		}
	}
}
