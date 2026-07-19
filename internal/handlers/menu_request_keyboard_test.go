package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/session"
)

func keyboardCallbacks(keyboard *callback.Keyboard) map[string]string {
	got := map[string]string{}
	if keyboard == nil {
		return got
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			got[button.CallbackData] = button.Text
		}
	}
	return got
}

func TestMovieDetailKeyboardHasOneClearPrimaryAction(t *testing.T) {
	h := &DetailHandler{}
	keyboard := convertKeyboard(h.buildMovieActionKeyboard(101, true, true))
	if len(keyboard.InlineKeyboard) != 4 {
		t.Fatalf("rows=%d want=4: %#v", len(keyboard.InlineKeyboard), keyboard)
	}
	primary := keyboard.InlineKeyboard[0]
	if len(primary) != 1 || primary[0].Text != "🎬 立即求片" || primary[0].CallbackData != "request:id:101:type:movie" {
		t.Fatalf("primary row=%#v", primary)
	}
	if len(keyboard.InlineKeyboard[1]) != 1 || !strings.Contains(keyboard.InlineKeyboard[1][0].Text, "加入想看") {
		t.Fatalf("social row=%#v", keyboard.InlineKeyboard[1])
	}
	tools := keyboard.InlineKeyboard[2]
	if len(tools) != 2 || tools[0].Text != "🔍 候选资源" || tools[1].Text != "🐛 反馈问题" {
		t.Fatalf("tool row=%#v", tools)
	}
	if keyboard.InlineKeyboard[3][0].Text != "⬅️ 返回结果" {
		t.Fatalf("navigation row=%#v", keyboard.InlineKeyboard[3])
	}
}

func TestTVDetailKeyboardDefersSeasonGridToPicker(t *testing.T) {
	h := &DetailHandler{}
	keyboard := convertKeyboard(h.buildTVActionKeyboard(202, 12, true, true))
	if len(keyboard.InlineKeyboard) != 5 {
		t.Fatalf("rows=%d want=5: %#v", len(keyboard.InlineKeyboard), keyboard)
	}
	if row := keyboard.InlineKeyboard[0]; len(row) != 1 || row[0].CallbackData != "detail_seasons:id:202" || row[0].Text != "🗂️ 选择季度" {
		t.Fatalf("safe primary row=%#v", row)
	}
	if row := keyboard.InlineKeyboard[1]; len(row) != 1 || row[0].CallbackData != "request:id:202:type:tv:season:0" || row[0].Text != "📺 求全部季度" {
		t.Fatalf("full-show row=%#v", row)
	}
	callbacks := keyboardCallbacks(keyboard)
	for data := range callbacks {
		if strings.Contains(data, ":season:") && data != "request:id:202:type:tv:season:0" {
			t.Fatalf("season button leaked onto detail page: %q", data)
		}
	}
	if callbacks["detail_seasons:id:202"] != "🗂️ 选择季度" || callbacks["request:id:202:type:tv:season:0"] != "📺 求全部季度" || callbacks["back"] != "⬅️ 返回结果" {
		t.Fatalf("callbacks=%#v", callbacks)
	}
}

func TestSeasonPickerStaysFocused(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	sess := manager.GetOrCreate(9)
	sess.SetSearchResults([]session.SearchItem{{
		ID: "303", Title: "测试剧集", Type: "tv",
		Seasons: []session.Season{{SeasonNumber: 0, EpisodeCount: 2, Name: "特别篇"}, {SeasonNumber: 1, EpisodeCount: 10, Name: "第一季"}, {SeasonNumber: 2, EpisodeCount: 8, Name: "第二季"}},
	}}, 1, "测试")
	h := NewDetailHandler(manager, nil, nil, nil)
	resp, err := h.HandleSeasons(&callback.Context{
		UserID:   9,
		Callback: &callback.Callback{Action: callback.ActionDetailSeasons, Params: map[string]string{"id": "303"}},
	})
	if err != nil || resp == nil || resp.Keyboard == nil {
		t.Fatalf("HandleSeasons resp=%#v err=%v", resp, err)
	}
	callbacks := keyboardCallbacks(resp.Keyboard)
	for _, forbidden := range []string{"start", "feedback:id:303:type:tv"} {
		if _, ok := callbacks[forbidden]; ok {
			t.Fatalf("season picker exposes unrelated callback %q: %#v", forbidden, callbacks)
		}
	}
	if callbacks["request:id:303:type:tv:season:1"] == "" || callbacks["request:id:303:type:tv:season:2"] == "" {
		t.Fatalf("season choices missing: %#v", callbacks)
	}
	if callbacks["request:id:303:type:tv:season:0"] != "📺 求全部季度" {
		t.Fatalf("specials duplicated season=0 or full-show label wrong: %#v", callbacks)
	}
	if callbacks["detail:id:303:type:tv:source:seasons"] != "⬅️ 返回详情" {
		t.Fatalf("detail return missing: %#v", callbacks)
	}
}

func TestReturningFromSeasonPickerDoesNotDuplicateNavigation(t *testing.T) {
	manager := session.NewManager(time.Hour, 10)
	sess := manager.GetOrCreate(9)
	sess.SetSearchResults([]session.SearchItem{{ID: "404", Title: "导航测试", Year: 2026, Type: "tv"}}, 1, "导航测试")
	sess.PushNavEntry("search", "导航测试", "导航测试")

	h := NewDetailHandler(manager, nil, nil, nil)
	resp, err := h.Handle(&callback.Context{
		UserID: 9,
		Callback: &callback.Callback{Action: callback.ActionDetail, Params: map[string]string{
			"id": "404", "type": "tv", "source": "seasons",
		}},
	})
	if err != nil || resp == nil || !resp.DeleteMessage || resp.Edit {
		t.Fatalf("season return resp=%#v err=%v", resp, err)
	}
	entry, ok := sess.PopNavEntry()
	if !ok || entry.Source != "search" {
		t.Fatalf("original navigation missing: entry=%#v ok=%v", entry, ok)
	}
	if extra, exists := sess.PopNavEntry(); exists {
		t.Fatalf("season return duplicated navigation: %#v", extra)
	}
}
