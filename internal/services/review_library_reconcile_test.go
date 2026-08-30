package services

import (
	"testing"
	"time"
)

func TestReconcileLibraryCompletionsBackfillsExactApprovedRequestIdempotently(t *testing.T) {
	rs := NewReviewService(t.TempDir(), false)
	now := time.Now()
	review := &ReviewRequest{RequestID: "醉玲珑-request", BusinessType: BusinessTypeRequest, TelegramID: 42, TmdbID: 1234, MediaTitle: "醉玲珑", MediaYear: 2017, MediaType: MediaTypeTV, Season: 1, Status: "approved", SubscriptionID: 77, SubscriptionState: "R"}
	if err := rs.CreateRequest(review); err != nil {
		t.Fatal(err)
	}
	stored, _ := rs.GetRequest(review.RequestID)
	if _, err := rs.Approve(review.RequestID, 9, stored.ApproveToken); err != nil {
		t.Fatal(err)
	}
	record := CompletionRecord{RequestID: review.RequestID, TelegramID: 42, Title: "醉玲珑", Year: 2017, MediaType: string(MediaTypeTV), Source: "confirmed_library", CompletedAt: now}
	if got, err := rs.ReconcileLibraryCompletions([]CompletionRecord{record}); err != nil || got != 1 {
		t.Fatalf("reconcile=(%d,%v), want 1,nil", got, err)
	}
	stored, ok := rs.GetRequest(review.RequestID)
	if !ok || stored.LibraryNotifiedAt == nil || !stored.LibraryNotifiedAt.Equal(now) {
		t.Fatalf("library marker=%v, want %v", stored.LibraryNotifiedAt, now)
	}
	if got, err := rs.ReconcileLibraryCompletions([]CompletionRecord{record}); err != nil || got != 0 {
		t.Fatalf("second reconcile=(%d,%v), want 0,nil", got, err)
	}
}

func TestReconcileLibraryCompletionsRejectsMismatchedIdentity(t *testing.T) {
	cases := []CompletionRecord{
		{RequestID: "other-id", TelegramID: 42, Title: "醉玲珑", Year: 2017, MediaType: string(MediaTypeTV), CompletedAt: time.Now()},
		{RequestID: "match", TelegramID: 99, Title: "醉玲珑", Year: 2017, MediaType: string(MediaTypeTV), CompletedAt: time.Now()},
		{RequestID: "match", TelegramID: 42, Title: "other", Year: 2017, MediaType: string(MediaTypeTV), CompletedAt: time.Now()},
		{RequestID: "match", TelegramID: 42, Title: "醉玲珑", Year: 2017, MediaType: string(MediaTypeMovie), CompletedAt: time.Now()},
		{RequestID: "match", TelegramID: 42, Title: "醉玲珑", Year: 2017, MediaType: string(MediaTypeTV), Season: 2, Source: "confirmed_library", CompletedAt: time.Now()},
		{RequestID: "match", TelegramID: 42, Title: "醉玲珑", Year: 2017, MediaType: string(MediaTypeTV), CompletedAt: time.Now()},
	}
	for i, record := range cases {
		rs := NewReviewService(t.TempDir(), false)
		if err := rs.CreateRequest(&ReviewRequest{RequestID: "match", BusinessType: BusinessTypeRequest, TelegramID: 42, TmdbID: 1234, MediaTitle: "醉玲珑", MediaYear: 2017, MediaType: MediaTypeTV, Season: 1}); err != nil {
			t.Fatal(err)
		}
		approved, _ := rs.GetRequest("match")
		if _, err := rs.Approve("match", 9, approved.ApproveToken); err != nil {
			t.Fatal(err)
		}
		got, err := rs.ReconcileLibraryCompletions([]CompletionRecord{record})
		if err != nil || got != 0 {
			t.Fatalf("case %d reconcile=(%d,%v), want 0,nil", i, got, err)
		}
		stored, _ := rs.GetRequest("match")
		if stored.LibraryNotifiedAt != nil {
			t.Fatalf("case %d incorrectly marked library complete", i)
		}
	}
}

func TestCanonicalLibraryCompletionRequiresMatchingUserTypeAndSeason(t *testing.T) {
	rs := NewReviewService(t.TempDir(), false)
	now := time.Now()
	for _, r := range []*ReviewRequest{
		{RequestID: "tv-s1", BusinessType: BusinessTypeRequest, TelegramID: 42, TmdbID: 900, MediaTitle: "剧", MediaType: MediaTypeTV, Season: 1, LibraryNotifiedAt: &now},
		{RequestID: "tv-s2", BusinessType: BusinessTypeRequest, TelegramID: 42, TmdbID: 900, MediaTitle: "剧", MediaType: MediaTypeTV, Season: 2},
	} {
		if err := rs.CreateRequest(r); err != nil {
			t.Fatal(err)
		}
		approved, _ := rs.GetRequest(r.RequestID)
		if _, err := rs.Approve(r.RequestID, 9, approved.ApproveToken); err != nil {
			t.Fatal(err)
		}
	}
	if !rs.IsLibraryCompletedForMedia(42, 900, "tv", 1) {
		t.Fatal("matching TV season not completed")
	}
	if rs.IsLibraryCompletedForMedia(42, 900, "tv", 2) {
		t.Fatal("uncompleted TV season reported completed")
	}
	if rs.IsLibraryCompletedForMedia(99, 900, "tv", 1) {
		t.Fatal("other user reported completed")
	}
	if rs.IsLibraryCompletedForMedia(42, 900, "movie", 0) {
		t.Fatal("wrong media type reported completed")
	}
}

func TestRecordLibraryCompletionPersistsMarkerAndLedgerIdempotently(t *testing.T) {
	dir := t.TempDir()
	rs := NewReviewService(dir, false)
	ledger := NewFulfillmentStatsService(dir)
	if err := rs.CreateRequest(&ReviewRequest{RequestID: "confirmed", TelegramID: 42, TmdbID: 10, MediaTitle: "片", MediaYear: 2026, MediaType: MediaTypeMovie}); err != nil {
		t.Fatal(err)
	}
	approved, _ := rs.GetRequest("confirmed")
	if _, err := rs.Approve("confirmed", 9, approved.ApproveToken); err != nil {
		t.Fatal(err)
	}
	record := CompletionRecord{RequestID: "confirmed", TelegramID: 42, Title: "片", Year: 2026, MediaType: string(MediaTypeMovie), Source: "confirmed_library", CompletedAt: time.Now()}
	if err := rs.RecordLibraryCompletion(record, ledger); err != nil {
		t.Fatal(err)
	}
	if err := rs.RecordLibraryCompletion(record, ledger); err != nil {
		t.Fatal(err)
	}
	stored, _ := rs.GetRequest("confirmed")
	if stored.LibraryNotifiedAt == nil {
		t.Fatal("confirmed marker missing")
	}
	if got := ledger.CompletionRecords(); len(got) != 1 {
		t.Fatalf("ledger records=%d, want 1", len(got))
	}
}
