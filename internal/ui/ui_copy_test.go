package ui

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/services"
)

func TestThemeCriticalCopyIsConsistent(t *testing.T) {
	styles := []UIStyle{StyleCard}
	for _, style := range styles {
		t.Run(string(style), func(t *testing.T) {
			builder := NewBuilder(style)
			if got := builder.BuildSearchResults("test", nil, 1, 0); !strings.Contains(got, emptySearchCopy) {
				t.Fatalf("search empty copy mismatch: %q", got)
			}
			if got := builder.BuildRequestList(nil, 1, 1, 0); !strings.Contains(got, emptyRequestCopy) {
				t.Fatalf("request empty copy mismatch: %q", got)
			}
			detail := builder.BuildMediaDetail(&services.SearchResult{ID: 123, Title: "测试", Type: "movie"})
			if strings.Contains(strings.ToUpper(detail), "TMDB") || strings.Contains(detail, "123") {
				t.Fatalf("detail exposes persistent TMDB ID: %q", detail)
			}
		})
	}
}

func TestRequestStatusLabels(t *testing.T) {
	cases := map[string]string{
		"pending": "待审核", "searching": "正在找资源", "recycled": "正在找资源",
		"downloading": "下载中", "completed": "已入库", "failed": "处理失败", "cancelled": "已取消", //nolint:misspell // legacy state compatibility
	}
	for state, want := range cases {
		if got := getRequestStatusLabel(state); got != want {
			t.Errorf("%s: got %q, want %q", state, got, want)
		}
	}
}

func TestStartButtonsUseMainMenuCopy(t *testing.T) {
	keyboards := []*struct{ InlineKeyboard [][]struct{} }{}
	_ = keyboards // callback shape is checked below without coupling to concrete aliases.

	keyboard := NewKeyboardBuilder().BuildNoResultsKeyboard()
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "start" && button.Text != "主菜单" && button.Text != "🏠 主菜单" {
				t.Fatalf("start callback text = %q", button.Text)
			}
		}
	}
}
