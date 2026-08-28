package handlers

import (
	"strings"
	"testing"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

func TestRequestHeatHandlerBuildsDetailLinksWithoutIdentityLeak(t *testing.T) {
	dir := t.TempDir()
	reviews := services.NewReviewService(dir, false)
	if err := reviews.CreateRequest(&services.ReviewRequest{
		RequestID: "heat-1", TelegramID: 99887766, TmdbID: 550,
		MediaTitle: "真实热门片", MediaYear: 2026, MediaType: services.MediaTypeMovie,
	}); err != nil {
		t.Fatal(err)
	}
	carpool := services.NewCarpoolService(dir)
	carpool.Add(550, "movie", 88776655)
	handler := NewRequestHeatHandler(services.NewRequestHeatService(reviews, carpool))
	resp, err := handler.Handle(&callback.Context{UserID: 1, ChatType: "private", Callback: &callback.Callback{Action: callback.ActionRequestHeat}})
	if err != nil || resp == nil || resp.Keyboard == nil {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	if !strings.Contains(resp.Text, "真实热门片") || !strings.Contains(resp.Text, "2 人想看") {
		t.Fatalf("text=%q", resp.Text)
	}
	if strings.Contains(resp.Text, "99887766") || strings.Contains(resp.Text, "88776655") {
		t.Fatalf("identity leaked: %q", resp.Text)
	}
	callbacks := keyboardCallbacks(resp.Keyboard)
	if callbacks["detail:id:550:type:movie:source:request_heat"] == "" || callbacks["request_heat"] != "刷新状态" ||
		callbacks["start"] != "返回首页" || callbacks["start_search"] != "搜索求片" {
		t.Fatalf("callbacks=%#v", callbacks)
	}
	// The action row must be a single 3-column row, not stacked singletons.
	last := resp.Keyboard.InlineKeyboard[len(resp.Keyboard.InlineKeyboard)-1]
	if len(last) != 3 {
		t.Fatalf("action row has %d buttons, want a 3-column row: %#v", len(last), last)
	}
	for _, button := range last {
		if len([]rune(button.Text)) != 4 {
			t.Fatalf("label %q is not 4 CJK characters", button.Text)
		}
	}
	for data := range callbacks {
		if len([]byte(data)) > 64 {
			t.Fatalf("callback too long: %q", data)
		}
	}
}

func TestRequestHeatHandlerShowsSingleUserTitlesAnonymously(t *testing.T) {
	reviews := services.NewReviewService(t.TempDir(), false)
	if err := reviews.CreateRequest(&services.ReviewRequest{
		RequestID: "private-single", TelegramID: 123, TmdbID: 42,
		MediaTitle: "只有一个人求", MediaType: services.MediaTypeMovie,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewRequestHeatHandler(services.NewRequestHeatService(reviews, nil))
	resp, err := handler.Handle(&callback.Context{UserID: 1, ChatType: "private", Callback: &callback.Callback{Action: callback.ActionRequestHeat}})
	if err != nil || resp == nil {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	if !strings.Contains(resp.Text, "只有一个人求") || !strings.Contains(resp.Text, "1 人想看") {
		t.Fatalf("single-user request was hidden: %q", resp.Text)
	}
	if strings.Contains(resp.Text, "123") {
		t.Fatalf("identity leaked: %q", resp.Text)
	}
}

func TestRequestHeatHandlerEmptyStatePointsToSearch(t *testing.T) {
	reviews := services.NewReviewService(t.TempDir(), false)
	handler := NewRequestHeatHandler(services.NewRequestHeatService(reviews, nil))
	resp, err := handler.Handle(&callback.Context{UserID: 1, ChatType: "private", Callback: &callback.Callback{Action: callback.ActionRequestHeat}})
	if err != nil || resp == nil || resp.Keyboard == nil {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	if !strings.Contains(resp.Text, "还没有正在等待的求片") {
		t.Fatalf("text=%q", resp.Text)
	}
	callbacks := keyboardCallbacks(resp.Keyboard)
	if callbacks["start_search"] != "搜索求片" || callbacks["requests"] != "查看进度" || callbacks["start"] != "返回首页" {
		t.Fatalf("callbacks=%#v", callbacks)
	}
	if len(resp.Keyboard.InlineKeyboard) != 1 || len(resp.Keyboard.InlineKeyboard[0]) != 3 {
		t.Fatalf("empty state must offer one 3-column row: %#v", resp.Keyboard.InlineKeyboard)
	}
}

func TestRequestHeatTitleTruncation(t *testing.T) {
	if got := truncateRequestHeatTitle("短标题", 22); got != "短标题" {
		t.Fatalf("got=%q", got)
	}
	got := truncateRequestHeatTitle(strings.Repeat("长", 30), 22)
	if len([]rune(got)) != 22 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got=%q runes=%d", got, len([]rune(got)))
	}
}
