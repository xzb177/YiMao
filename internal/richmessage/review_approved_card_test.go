package richmessage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildReviewApprovedCardHasClearProgressAndNoInternalDetails(t *testing.T) {
	card := BuildReviewApprovedCard(ReviewApprovedData{Title: "浪漫的绝对值", Year: 2026, MediaType: "剧集", SeasonText: "第 1 季"})
	raw, _ := json.Marshal(card.Input())
	body := string(raw) + card.Markdown
	for _, want := range []string{StatusApproved, "浪漫的绝对值", "2026", "第 1 季"} {
		if !strings.Contains(body, want) {
			t.Fatalf("approved card missing %q", want)
		}
	}
	for _, forbidden := range []string{"MoviePilot", "subscription", "TMDB"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("internal detail %q", forbidden)
		}
	}
}

func TestBuildGroupApprovedCardIsCompactPublicHighlight(t *testing.T) {
	card := BuildGroupApprovedCard(GroupApprovedData{Title: "浪漫的绝对值", Year: 2026, MediaType: "剧集", SeasonText: "第 1 季", Requester: "春暖花开"})
	raw, _ := json.Marshal(card.Input())
	body := string(raw) + card.Markdown
	for _, want := range []string{StatusApproved, "浪漫的绝对值", "春暖花开", "第 1 季"} {
		if !strings.Contains(body, want) {
			t.Fatalf("group approved missing %q", want)
		}
	}
}
