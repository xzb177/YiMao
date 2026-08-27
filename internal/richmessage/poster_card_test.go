package richmessage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequesterReceiptCardUsesAPI103BlocksAndStatusCopy(t *testing.T) {
	card := BuildRequesterReceiptCard("浪漫的绝对值", 2026, "tv", 1, StatusPending, "审核通过后自动下载，完成后可播放。", "https://example.com/p.jpg")
	body := string(mustJSON(t, card.Input()))
	for _, want := range []string{`"type":"photo"`, `"type":"heading"`, `"type":"table"`, `"is_compact":true`, `"type":"buttons"`, StatusPending, "浪漫的绝对值", "第 1 季"} {
		if !strings.Contains(body, want) {
			t.Fatalf("receipt card missing %q in %s", want, body)
		}
	}
	approved := BuildRequesterReceiptCard("浪漫的绝对值", 2026, "tv", 1, StatusApproved, "匹配资源后自动下载，完成后可播放。", "")
	out := string(mustJSON(t, approved.Input()))
	if strings.Contains(out, StatusPending) || strings.Contains(out, "求片已提交") {
		t.Fatalf("approved card still pending: %s", out)
	}
	if !strings.Contains(out, StatusApproved) {
		t.Fatalf("approved card missing status: %s", out)
	}
}

func TestMediaAndWelcomeCardsUseCompactTable(t *testing.T) {
	media := BuildMediaInfoCard(MediaInfo{Title: "测试", Year: 2024, Rating: 8.2, Runtime: 120, Overview: "很长的剧情介绍用来展开引用。", PosterURL: "https://example.com/a.jpg", MediaType: "movie", Status: StatusInLibrary})
	body := string(mustJSON(t, media.Input()))
	for _, want := range []string{`"type":"photo"`, `"type":"table"`, `"is_compact":true`, `"type":"expandable_blockquote"`, StatusInLibrary, "测试"} {
		if !strings.Contains(body, want) {
			t.Fatalf("media card missing %q in %s", want, body)
		}
	}
	welcome := BuildWelcomeMessage("春暖花开")
	wbody := string(mustJSON(t, welcome.Input()))
	if !strings.Contains(wbody, `"type":"heading"`) || !strings.Contains(wbody, `"type":"buttons"`) {
		t.Fatalf("welcome card: %s", wbody)
	}
}

func TestApprovedRejectedCardsDropOldCopy(t *testing.T) {
	approved := BuildReviewApprovedCard(ReviewApprovedData{Title: "浪漫的绝对值", Year: 2026, MediaType: "剧集", SeasonText: "第 1 季"})
	body := string(mustJSON(t, approved.Input()))
	for _, want := range []string{StatusApproved, "浪漫的绝对值", `"type":"table"`, `"is_compact":true`, `"type":"buttons"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("approved missing %q: %s", want, body)
		}
	}
	rejected := BuildReviewRejectedCard("测试", 2020, "")
	if !strings.Contains(string(mustJSON(t, rejected.Input())), StatusRejected) {
		t.Fatal("rejected missing status")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
