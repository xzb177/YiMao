package services

import "testing"

func TestAdventureRequestNeverCreatesRefundableQuota(t *testing.T) {
	dir := t.TempDir()
	quota := NewQuotaService(dir, nil)
	s := NewReviewService(dir, false)
	if err := s.CreateRequest(&ReviewRequest{
		RequestID: "adv", TelegramID: 11, MediaType: MediaTypeTV,
		RequestOrigin: "adventure", QuotaCost: 0,
	}); err != nil {
		t.Fatal(err)
	}
	r, _ := s.GetRequest("adv")
	if r.QuotaCost != 0 {
		t.Fatalf("adventure quota cost = %d, want 0", r.QuotaCost)
	}
	restored, err := s.RestoreQuotaOnce("adv", quota)
	if err != nil || restored {
		t.Fatalf("adventure restore = %v, %v; want false, nil", restored, err)
	}
	if got := quota.GetQuotaInfo(11).TVUsed; got != 0 {
		t.Fatalf("adventure refund created quota: TVUsed=%d", got)
	}
}
