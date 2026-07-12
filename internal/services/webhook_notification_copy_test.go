package services

import (
	"strings"
	"testing"
)

func TestEmbyEntryFormattingHidesUnknownAndReferenceFile(t *testing.T) {
	s := &WebhookService{}
	enhanced := &EmbyEnhancedInfo{Title: "测试影片", FileSize: 0, FileCount: 0}

	for name, got := range map[string]string{
		"enhanced": s.formatEmbyNotificationEnhanced(EmbyWebhookPayload{}, enhanced),
		"photo":    s.formatPhotoCaption(EmbyWebhookPayload{}, enhanced),
	} {
		if !strings.Contains(got, "测试影片") {
			t.Fatalf("%s missing title fallback: %q", name, got)
		}
		for _, forbidden := range []string{"未知", "0B", "引用文件", "文件数量：0"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("%s leaked %q: %q", name, forbidden, got)
			}
		}
	}
}

func TestEmbyEntryFormattingUsesNonEmptyGenericTitle(t *testing.T) {
	s := &WebhookService{}
	got := s.formatEmbyNotificationSimple(EmbyWebhookPayload{}, nil)
	if strings.Contains(got, "《》") {
		t.Fatalf("empty title rendered: %q", got)
	}
}

func TestSanitizeAlertDetail(t *testing.T) {
	got := sanitizeAlertDetail("request failed: https://example.test/api?token=secret&api_key=also-secret\nstatus=500")
	if strings.Contains(got, "secret") || strings.Contains(got, "\n") {
		t.Fatalf("detail was not sanitized: %q", got)
	}
	if !strings.Contains(got, "status=500") {
		t.Fatalf("useful error context was removed: %q", got)
	}
}
