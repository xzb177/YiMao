package services

import (
	"testing"
	"time"
)

func TestSummarizeTransferHistoryUsesCompleteNaturalDay(t *testing.T) {
	rows := []TransferHistoryItem{
		{Title: "幸运女神", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E01", Date: "2026-07-17 00:34:20", Status: true},
		{Title: "幸运女神", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E02", Date: "2026-07-17 00:35:20", Status: true},
		{Title: "浪漫的绝对值", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E01", Date: "2026-07-17 23:50:42", Status: true},
		{Title: "浪漫的绝对值", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E04", Date: "2026-07-17 23:54:35", Status: true},
		{Title: "次日内容", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E01", Date: "2026-07-18 00:00:00", Status: true},
		{Title: "失败内容", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E01", Date: "2026-07-17 12:00:00", Status: false},
	}
	day := time.Date(2026, 7, 17, 0, 0, 0, 0, time.Local)

	summary := SummarizeTransferHistory(rows, day)

	if summary.SeriesCount != 2 || summary.FileCount != 4 {
		t.Fatalf("counts = series %d files %d, want 2 and 4", summary.SeriesCount, summary.FileCount)
	}
	if got := summary.FirstAt.Format("15:04:05"); got != "00:34:20" {
		t.Fatalf("first = %s", got)
	}
	if got := summary.LastAt.Format("15:04:05"); got != "23:54:35" {
		t.Fatalf("last = %s", got)
	}
	if len(summary.Series) != 2 || summary.Series[0].Title != "幸运女神" || summary.Series[1].Title != "浪漫的绝对值" {
		t.Fatalf("unexpected series: %#v", summary.Series)
	}
	if got := summary.Series[1].EpisodeDisplay(); got != "E01,E04" {
		t.Fatalf("episodes = %s", got)
	}
}

func TestSummarizeTransferHistoryCountsEverySuccessfulFileAndDeduplicatesEpisodeDisplay(t *testing.T) {
	rows := []TransferHistoryItem{
		{ID: 1, Title: "眼镜蛇", Year: FlexibleYear(2018), Type: "电视剧", Seasons: "S06", Episodes: "E01", Date: "2026-07-17 10:00:00", Status: true},
		{ID: 2, Title: "眼镜蛇", Year: FlexibleYear(2018), Type: "电视剧", Seasons: "S06", Episodes: "E01", Date: "2026-07-17 10:01:00", Status: true},
		{ID: 3, Title: "眼镜蛇", Year: FlexibleYear(2018), Type: "电视剧", Seasons: "S06", Episodes: "E02", Date: "2026-07-17 10:02:00", Status: true},
	}
	day := time.Date(2026, 7, 17, 0, 0, 0, 0, time.Local)

	summary := SummarizeTransferHistory(rows, day)

	if summary.FileCount != 3 || summary.SeriesCount != 1 {
		t.Fatalf("counts = %#v", summary)
	}
	if got := summary.Series[0].EpisodeDisplay(); got != "E01-E02" {
		t.Fatalf("episodes = %s", got)
	}
}

func TestSummarizeTransferHistorySeparatesMoviesFromSeries(t *testing.T) {
	rows := []TransferHistoryItem{
		{Title: "电影甲", Year: FlexibleYear(2026), Type: "电影", Date: "2026-07-17 09:00:00", Status: true},
		{Title: "剧集乙", Year: FlexibleYear(2026), Type: "电视剧", Seasons: "S01", Episodes: "E01", Date: "2026-07-17 10:00:00", Status: true},
	}
	summary := SummarizeTransferHistory(rows, time.Date(2026, 7, 17, 0, 0, 0, 0, time.Local))
	if summary.MovieCount != 1 || summary.SeriesCount != 1 || summary.FileCount != 2 {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if got := summary.Movies[0].DisplayTitle(); got != "电影甲 (2026)：1 个文件" {
		t.Fatalf("movie title = %q", got)
	}
}

func TestDailySummaryDueRetriesAfterScheduledTime(t *testing.T) {
	now := time.Date(2026, 7, 19, 0, 12, 0, 0, time.Local)
	if !dailySummaryDue(now, "00:10", "") {
		t.Fatal("summary should be due after scheduled time")
	}
	if dailySummaryDue(now, "00:10", "2026-07-18") {
		t.Fatal("summary should not repeat after the report day is marked sent")
	}
	if dailySummaryDue(time.Date(2026, 7, 19, 0, 9, 0, 0, time.Local), "00:10", "") {
		t.Fatal("summary should not run before scheduled time")
	}
	if dailySummaryDue(now, "invalid", "") {
		t.Fatal("invalid time must fail closed")
	}
}
