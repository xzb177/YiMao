package handlers

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/richmessage"
)

func TestBuildEphemeralMediaCaption(t *testing.T) {
	caption := buildEphemeralMediaCaption(richmessage.MediaInfo{
		Title: "A&B <Test>", Year: 2026, Rating: 8.7,
		Genres: []string{"剧情", "科幻"}, MediaType: "movie", Runtime: 128,
		Overview: strings.Repeat("简介", 300),
	})
	if !strings.Contains(caption, "A&amp;B &lt;Test&gt;") {
		t.Fatalf("title not escaped: %s", caption)
	}
	if !strings.Contains(caption, "⭐ 8.7") || !strings.Contains(caption, "⏱ 128 分钟") {
		t.Fatalf("metadata missing: %s", caption)
	}
	if len([]rune(caption)) > 1024 {
		t.Fatalf("caption too long: %d", len([]rune(caption)))
	}
}
