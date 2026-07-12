package services

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPreferencesSetPreferenceDoesNotDeadlock(t *testing.T) {
	s := NewPreferencesService(filepath.Join(t.TempDir(), "preferences.json"))
	done := make(chan error, 1)
	go func() { done <- s.SetPreference(1001, PrefMovieNotification, false) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetPreference failed: %v", err)
		}
		if s.GetPreferences(1001).Movies {
			t.Fatal("movie preference was not updated")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SetPreference deadlocked")
	}
}

func TestRestoreTVQuotaRestoresChargedCost(t *testing.T) {
	s := NewQuotaService(filepath.Join(t.TempDir(), "quotas.json"), nil)
	if err := s.UseTVQuota(2002); err != nil {
		t.Fatalf("UseTVQuota: %v", err)
	}
	if got := s.GetQuotaInfo(2002).TVUsed; got != 3 {
		t.Fatalf("charged=%d, want 3", got)
	}
	if err := s.RestoreQuota(2002, "tv"); err != nil {
		t.Fatalf("RestoreQuota: %v", err)
	}
	if got := s.GetQuotaInfo(2002).TVUsed; got != 0 {
		t.Fatalf("restored TVUsed=%d, want 0", got)
	}
}

func TestMediaSummaryTitleFallsBackToPayloadItemName(t *testing.T) {
	payload := EmbyWebhookPayload{Item: &EmbyItem{Name: "平凡英雄", Type: "Movie"}}
	if got := resolveSummaryTitle(payload, nil); got != "平凡英雄" {
		t.Fatalf("title=%q, want %q", got, "平凡英雄")
	}
}
