package handlers

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestBuildSearchSlideshowPreservesOriginalNumbersAndMetadata(t *testing.T) {
	results := []services.SearchResult{
		{ID: 1, Title: "无海报", Year: 2024, Type: "movie"},
		{ID: 2, Title: "第二部", Year: 2025, Type: "movie", Rating: 8.1, Poster: "https://image.tmdb.org/t/p/w500/2.jpg", Overview: "这是第一行。\n这是第二行，用来验证简介会压成一段。"},
		{ID: 3, Title: "第三部", Year: 2026, Type: "tv", Rating: 9.2, Poster: "file-id-3"},
	}
	rich := buildSearchSlideshow("测试", 2, results)
	if rich == nil || len(rich.Blocks) != 1 || len(rich.Blocks[0].Blocks) != 2 {
		t.Fatalf("rich=%+v", rich)
	}
	first := rich.Blocks[0].Blocks[0].Caption.Text
	second := rich.Blocks[0].Blocks[1].Caption.Text
	if !strings.Contains(first, "2 · 第二部\n2025 · 电影 · ⭐ 8.1") || !strings.Contains(second, "3 · 第三部\n2026 · 剧集 · ⭐ 9.2") {
		t.Fatalf("captions lost original numbering/metadata: %q / %q", first, second)
	}
	if !strings.Contains(first, "这是第一行。 这是第二行") {
		t.Fatalf("overview was not compacted into the slide caption: %q", first)
	}
	if !strings.Contains(rich.Blocks[0].Caption.Text, "左右滑动看海报，点下方片名看详情。") {
		t.Fatalf("caption=%q", rich.Blocks[0].Caption.Text)
	}
	if got := rich.Blocks[0].Blocks[0].Photo.Media; got != "https://image.tmdb.org/t/p/w500/2.jpg" {
		t.Fatalf("full poster URL changed: %q", got)
	}
	keyboard := buildSearchResultsKeyboard(results, 2, false)
	if keyboard.InlineKeyboard[1][0].CallbackData != "select:id:2:type:movie" || keyboard.InlineKeyboard[2][0].CallbackData != "select:id:3:type:tv" {
		t.Fatalf("buttons do not preserve original result IDs: %+v", keyboard)
	}
}

func TestBuildSearchSlideshowRequiresTwoPosters(t *testing.T) {
	if got := buildSearchSlideshow("测试", 1, []services.SearchResult{{Poster: "one"}, {Title: "none"}}); got != nil {
		t.Fatalf("one valid poster should not produce slideshow: %+v", got)
	}
}

func TestCompactOverviewUsesRuneSafeLimit(t *testing.T) {
	got := compactOverview("  第一行\n第二行   第三行  ", 7)
	if got != "第一行 第二行…" {
		t.Fatalf("compactOverview=%q", got)
	}
	if got := compactOverview("完整短简介", 20); got != "完整短简介" {
		t.Fatalf("short overview changed: %q", got)
	}
}

func TestBuildSearchSlideshowNormalizesTMDBPosterPath(t *testing.T) {
	rich := buildSearchSlideshow("测试", 1, []services.SearchResult{
		{Title: "一", Poster: "/one.jpg"},
		{Title: "二", Poster: "two.jpg"},
	})
	if rich == nil {
		t.Fatal("expected slideshow")
	}
	blocks := rich.Blocks[0].Blocks
	if blocks[0].Photo.Media != "https://image.tmdb.org/t/p/w500/one.jpg" || blocks[1].Photo.Media != "https://image.tmdb.org/t/p/w500/two.jpg" {
		t.Fatalf("poster URLs not normalized: %q / %q", blocks[0].Photo.Media, blocks[1].Photo.Media)
	}
}
