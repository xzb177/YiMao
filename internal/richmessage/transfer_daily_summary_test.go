package richmessage

import (
	"strings"
	"testing"
)

func TestBuildTransferDailySummaryCardCountsDistinctSeriesAndSeasons(t *testing.T) {
	series := []TransferDailySummarySeries{
		{Title: "📺超级飞侠 (2014) S01E01-E52", WorkTitle: "超级飞侠", Files: 52},
		{Title: "📺超级飞侠 (2014) S02E01-E26", WorkTitle: "超级飞侠", Files: 26},
		{Title: "📺动物神探队 (2022) S02E01", WorkTitle: "动物神探队", Files: 1},
	}

	got := BuildTransferDailySummaryCard("2026年7月29日", nil, series, 79, "00:01:00", "23:59:00").Markdown
	if !strings.Contains(got, "2 部 / 3 季") {
		t.Fatalf("stats table did not distinguish works from seasons:\n%s", got)
	}
	// Detail lines are one per entry: the episode range must share its own
	// title's line instead of squashing everything into one paragraph.
	clean := strings.ReplaceAll(got, "\\", "")
	lines := strings.Split(clean, "\n")
	lineOf := func(needle string) string {
		for _, l := range lines {
			if strings.Contains(l, needle) {
				return l
			}
		}
		return ""
	}
	s1 := lineOf("超级飞侠 (2014) S01E01-E52")
	s2 := lineOf("超级飞侠 (2014) S02E01-E26")
	if s1 == "" || s2 == "" || s1 == s2 {
		t.Fatalf("series details are not one-per-line:\n%s", got)
	}
	if strings.Contains(got, "剧集更新（3 部）") {
		t.Fatalf("series-season entries were mislabeled as distinct works:\n%s", got)
	}
}

func TestBuildTransferDailySummaryCardKeepsSimpleSeriesLabel(t *testing.T) {
	series := []TransferDailySummarySeries{
		{Title: "📺剧集甲 (2026) S01E01", WorkTitle: "剧集甲", Files: 1},
		{Title: "📺剧集乙 (2026) S01E01", WorkTitle: "剧集乙", Files: 1},
	}

	got := BuildTransferDailySummaryCard("2026年7月29日", nil, series, 2, "00:01:00", "00:02:00").Markdown
	clean := strings.ReplaceAll(got, "\\", "")
	if !strings.Contains(clean, "电视剧更新") || !strings.Contains(clean, "📺剧集甲 (2026) S01E01") {
		t.Fatalf("series detail line missing first entry:\n%s", got)
	}
	if !strings.Contains(clean, "📺剧集乙 (2026) S01E01") {
		t.Fatalf("series detail line missing second entry:\n%s", got)
	}
	if !strings.Contains(got, "2 部") {
		t.Fatalf("simple series count missing:\n%s", got)
	}
	if strings.Contains(got, "2 部 / 2 季") {
		t.Fatalf("redundant season count should be omitted:\n%s", got)
	}
}
