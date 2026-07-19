package richmessage

import (
	"strings"
	"testing"
)

func TestBuildReviewApprovedCardHasClearProgressAndNoInternalDetails(t *testing.T) {
	card := BuildReviewApprovedCard(ReviewApprovedData{
		Title:      "浪漫的绝对值",
		Year:       2026,
		MediaType:  "剧集",
		MediaIcon:  "📺",
		SeasonText: "第 1 季",
	})

	for _, want := range []string{
		"审核通过，已经安排上了",
		"浪漫的绝对值",
		"2026",
		"第 1 季",
		"正在寻找资源",
		"匹配资源 → 下载 → 入库",
		"入库后第一时间通知你",
	} {
		if !strings.Contains(card.Markdown, want) {
			t.Fatalf("approved card missing %q:\n%s", want, card.Markdown)
		}
	}

	for _, forbidden := range []string{"MoviePilot", "subscription", "TMDB"} {
		if strings.Contains(card.Markdown, forbidden) {
			t.Fatalf("approved card exposes internal detail %q:\n%s", forbidden, card.Markdown)
		}
	}
}

func TestBuildGroupApprovedCardIsCompactPublicHighlight(t *testing.T) {
	card := BuildGroupApprovedCard(GroupApprovedData{
		Title:      "浪漫的绝对值",
		Year:       2026,
		MediaType:  "剧集",
		MediaIcon:  "📺",
		SeasonText: "第 1 季",
		Requester:  "春暖花开",
	})

	for _, want := range []string{
		"新片上车",
		"审核通过",
		"浪漫的绝对值",
		"春暖花开",
		"第 1 季",
		"正在寻找资源",
		"入库后通知求片人",
	} {
		if !strings.Contains(card.Markdown, want) {
			t.Fatalf("group approved card missing %q:\n%s", want, card.Markdown)
		}
	}

	for _, forbidden := range []string{"MoviePilot", "TMDB", "审批人"} {
		if strings.Contains(card.Markdown, forbidden) {
			t.Fatalf("group approved card exposes internal detail %q:\n%s", forbidden, card.Markdown)
		}
	}
}
