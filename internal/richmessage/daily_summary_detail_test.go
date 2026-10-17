package richmessage

import (
	"fmt"
	"strings"
	"testing"
)

// detailLines splits rendered markdown into non-empty lines with markdown
// escapes stripped, so assertions can read like the Telegram client shows.
func detailLines(md string) []string {
	clean := strings.ReplaceAll(md, "\\", "")
	raw := strings.Split(clean, "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

func lineContaining(lines []string, needle string) string {
	for _, l := range lines {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}

// Transfer 版：剧集明细一条一行，集数范围必须在它自己标题的同一行。
func TestTransferDailySummarySeriesDetailsOnePerLine(t *testing.T) {
	series := []TransferDailySummarySeries{
		{Title: "📺回来的女儿 (2022) S01E05-E12", WorkTitle: "回来的女儿", Files: 8},
		{Title: "📺明天，我会成为谁的女友 (2022) S01E01-E12", WorkTitle: "明天，我会成为谁的女友", Files: 12},
	}
	movies := []TransferDailySummarySeries{{Title: "🎬希望 (2026)", Files: 1}}
	got := BuildTransferDailySummaryCard("2026年10月16日", movies, series, 21, "00:33:15", "23:29:42").Markdown
	lines := detailLines(got)

	line := lineContaining(lines, "回来的女儿 (2022)")
	if line == "" || !strings.Contains(line, "S01E05-E12") {
		t.Fatalf("集数范围没有和它自己的标题在同一行，回来的女儿所在行=%q\n%s", line, got)
	}
	line2 := lineContaining(lines, "明天，我会成为谁的女友 (2022)")
	if line2 == "" || !strings.Contains(line2, "S01E01-E12") {
		t.Fatalf("第二部剧的集数范围没有和它自己的标题在同一行，所在行=%q\n%s", line2, got)
	}
	if line == line2 {
		t.Fatalf("两部剧被挤在同一行：%q", line)
	}
	movieLine := lineContaining(lines, "希望 (2026)")
	if movieLine == "" || !strings.Contains(movieLine, "🎬") {
		t.Fatalf("电影明细没有独立成行并带图标，所在行=%q\n%s", movieLine, got)
	}
	// 统计表部分不动
	if !strings.Contains(got, "入库时间") {
		t.Fatalf("统计表被改动了：\n%s", got)
	}
}

// 同剧多季：每一季各占一行。
func TestTransferDailySummaryMultiSeasonMultiLine(t *testing.T) {
	series := []TransferDailySummarySeries{
		{Title: "📺超级飞侠 (2014) S01E01-E52", WorkTitle: "超级飞侠", Files: 52},
		{Title: "📺超级飞侠 (2014) S02E01-E26", WorkTitle: "超级飞侠", Files: 26},
	}
	got := BuildTransferDailySummaryCard("2026年7月29日", nil, series, 78, "00:01:00", "23:59:00").Markdown
	lines := detailLines(got)
	s1 := lineContaining(lines, "S01E01-E52")
	s2 := lineContaining(lines, "S02E01-E26")
	if s1 == "" || s2 == "" {
		t.Fatalf("两季没有各自独立成行：\n%s", got)
	}
	if s1 == s2 {
		t.Fatalf("两季被挤在同一行：%q", s1)
	}
	if !strings.Contains(got, "1 部 / 2 季") {
		t.Fatalf("统计表部/季口径被改动：\n%s", got)
	}
}

// 超上限：最多 50 行明细，末尾汇总尾行带真实总数。
func TestTransferDailySummaryDetailsOverflowCap(t *testing.T) {
	movies := make([]TransferDailySummarySeries, 55)
	for i := range movies {
		movies[i] = TransferDailySummarySeries{Title: fmt.Sprintf("🎬电影%02d (2026)", i), Files: 1}
	}
	got := BuildTransferDailySummaryCard("2026年10月16日", movies, nil, 55, "00:01:00", "23:59:00").Markdown
	lines := detailLines(got)
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "🎬电影") {
			n++
		}
	}
	if n != 50 {
		t.Fatalf("明细行应截断到 50 行，实际 %d 行", n)
	}
	if !strings.Contains(got, "……等共 55 部") {
		t.Fatalf("超上限没有汇总尾行：\n%s", got)
	}
}

// 旧版 BuildDailySummaryCard：同样一条一行，集数范围跟在自己标题后。
func TestDailySummaryCardDetailsOnePerLine(t *testing.T) {
	movies := []DailySummaryMovie{{Title: "希望", Year: 2026}}
	series := []DailySummarySeries{
		{Title: "回来的女儿 (2022) S01E05-E12"},
		{Title: "狂热先生 (2026) S01E03"},
	}
	got := BuildDailySummaryCard("2026年10月16日", movies, series, 3).Markdown
	lines := detailLines(got)

	movieLine := lineContaining(lines, "希望 (2026)")
	if movieLine == "" || !strings.Contains(movieLine, "🎬") {
		t.Fatalf("电影没有独立成行带年份：%q\n%s", movieLine, got)
	}
	line := lineContaining(lines, "回来的女儿 (2022)")
	if line == "" || !strings.Contains(line, "S01E05-E12") {
		t.Fatalf("集数范围没有和它自己的标题在同一行：%q\n%s", line, got)
	}
	line2 := lineContaining(lines, "狂热先生 (2026)")
	if line2 == "" || !strings.Contains(line2, "S01E03") || line2 == line {
		t.Fatalf("第二部剧明细异常：%q\n%s", line2, got)
	}
	if !strings.Contains(got, "📊 合计") {
		t.Fatalf("统计表被改动了：\n%s", got)
	}
}
