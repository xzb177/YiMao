package richmessage

import (
	"strings"
	"testing"
)

func TestBuildTransferDailySummaryCardCountsDistinctSeriesAndSeasons(t *testing.T) {
	series := []TransferDailySummarySeries{
		{Title: "超级飞侠 (2014) S01：E01-E52", WorkTitle: "超级飞侠", Files: 52},
		{Title: "超级飞侠 (2014) S02：E01-E26", WorkTitle: "超级飞侠", Files: 26},
		{Title: "动物神探队 (2022) S02：E01", WorkTitle: "动物神探队", Files: 1},
	}

	got := BuildTransferDailySummaryCard("2026年7月29日", nil, series, 79, "00:01:00", "23:59:00").Markdown
	if !strings.Contains(got, "📺 剧集更新（2 部 / 3 季）") {
		t.Fatalf("series heading did not distinguish works from seasons:\n%s", got)
	}
	if strings.Contains(got, "剧集更新（3 部）") {
		t.Fatalf("series-season entries were mislabeled as distinct works:\n%s", got)
	}
}

func TestBuildTransferDailySummaryCardKeepsSimpleSeriesLabel(t *testing.T) {
	series := []TransferDailySummarySeries{
		{Title: "剧集甲 (2026) S01：E01", WorkTitle: "剧集甲", Files: 1},
		{Title: "剧集乙 (2026) S01：E01", WorkTitle: "剧集乙", Files: 1},
	}

	got := BuildTransferDailySummaryCard("2026年7月29日", nil, series, 2, "00:01:00", "00:02:00").Markdown
	if !strings.Contains(got, "📺 剧集更新（2 部）") {
		t.Fatalf("unexpected simple series heading:\n%s", got)
	}
	if strings.Contains(got, "2 部 / 2 季") {
		t.Fatalf("redundant season count should be omitted:\n%s", got)
	}
}
