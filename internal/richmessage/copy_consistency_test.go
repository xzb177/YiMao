package richmessage

import (
	"strings"
	"testing"
)

func TestWelcomeCopyKeepsCanonicalProductOrder(t *testing.T) {
	markdown := BuildWelcomeMessage("").Markdown
	search := strings.Index(markdown, "搜索求片")
	progress := strings.Index(markdown, "求片进度")
	adventure := strings.Index(markdown, "电影冒险")
	if search < 0 || progress < 0 || adventure < 0 {
		t.Fatalf("welcome copy misses canonical labels: %q", markdown)
	}
	if !(search < progress && progress < adventure) {
		t.Fatalf("welcome hierarchy is not request-first: search=%d progress=%d adventure=%d", search, progress, adventure)
	}
	for _, legacy := range []string{"普通求片", "趣味求片", "通关才给下载"} {
		if strings.Contains(markdown, legacy) {
			t.Errorf("welcome copy contains legacy phrase %q", legacy)
		}
	}
}

func TestMultiplierRewardCopyIsNeutralAndComplete(t *testing.T) {
	item := BlindBoxItemView{Title: "测试影片", Year: 2026, Rarity: "SR", Rating: 8.5, Genres: "剧情"}
	cards := []RichMessage{
		BuildGambleOfferCard(GambleOfferCardData{Grade: "S", ItemCount: 1, MovieTitle: "测试"}),
		BuildGambleResultCard(GambleResultCardData{Grade: "S", Items: []BlindBoxItemView{item, item}, Won: true, Multiplier: 2, MovieTitle: "测试"}),
		BuildGambleResultCard(GambleResultCardData{Grade: "S", Items: []BlindBoxItemView{item}, Won: false, MovieTitle: "测试"}),
		BuildGambleResultCard(GambleResultCardData{Grade: "S", Won: false, MovieTitle: "测试"}),
	}
	for _, card := range cards {
		for _, phrase := range []string{"赌赢", "赌局", "豪赌", "命运眷顾"} {
			if strings.Contains(card.Markdown, phrase) {
				t.Errorf("reward copy contains pressure phrase %q: %q", phrase, card.Markdown)
			}
		}
	}
	if !strings.Contains(cards[2].Markdown, "本次保留 1 个盲盒") || !strings.Contains(cards[2].Markdown, "测试影片") {
		t.Fatalf("partial reward is not rendered: %q", cards[2].Markdown)
	}
}

func TestAdventureStatsUsesNeutralIncompleteRecordIcon(t *testing.T) {
	card := BuildAdventureStatsCard(AdventureStatsCardData{
		UserName:        "影迷",
		TotalChallenges: 1,
		RecentRecords: []AdventureRecordView{{
			MovieName: "测试影片",
			Score:     20,
			Grade:     "D",
			Success:   false,
			TimeAgo:   "刚刚",
		}},
	})
	if strings.Contains(card.Markdown, "💀") {
		t.Fatalf("incomplete record should not use death imagery: %q", card.Markdown)
	}
}
