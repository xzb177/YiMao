package richmessage

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/xzb177/yimao/pkg/types"
)

// fourCharLexicon is the complete approved user-visible button vocabulary.
var fourCharLexicon = map[string]bool{
	"搜索求片": true, "查看进度": true, "帮助说明": true, "更多功能": true,
	"返回首页": true, "刷新状态": true, "申请洗版": true, "进入许愿": true,
	"系统设置": true, "问题反馈": true, "游戏中心": true, "通知设置": true,
	"绑定账号": true, "重置密码": true, "我的反馈": true, "观影周报": true,
	"取消操作": true, "求片热度": true, "管理后台": true,
}

func buttonRows(t *testing.T, card Card) [][]types.TelegramRichMessageButton {
	t.Helper()
	rows := [][]types.TelegramRichMessageButton{}
	for _, block := range card.Blocks {
		if block.Type == "buttons" && len(block.Buttons) > 0 {
			rows = append(rows, block.Buttons)
		}
	}
	return rows
}

func buttonText(t *testing.T, btn types.TelegramRichMessageButton) string {
	t.Helper()
	return fmt.Sprint(btn.Text)
}

// assertGrid enforces the 3-column contract: every row is 3 wide, or a
// deliberate 2-wide row, or a single full-width closing 返回首页.
func assertGrid(t *testing.T, name string, rows [][]types.TelegramRichMessageButton) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("%s: card emits no button rows", name)
	}
	for i, row := range rows {
		switch len(row) {
		case 3, 2:
		case 1:
			if text := buttonText(t, row[0]); text != "返回首页" {
				t.Errorf("%s row %d: orphan button %q is only allowed as a full-width 返回首页", name, i, text)
			}
			if i != len(rows)-1 {
				t.Errorf("%s row %d: a full-width closing button must be the last row", name, i)
			}
		default:
			t.Errorf("%s row %d: %d buttons, want 3 (or a deliberate 2 / closing 1)", name, i, len(row))
		}
		for _, btn := range row {
			text := buttonText(t, btn)
			runes := []rune(text)
			if len(runes) != 4 {
				t.Errorf("%s: label %q has %d characters, want exactly 4", name, text, len(runes))
			}
			for _, r := range runes {
				if !unicode.Is(unicode.Han, r) {
					t.Errorf("%s: label %q must be CJK only, found %q", name, text, r)
				}
			}
			if !fourCharLexicon[text] {
				t.Errorf("%s: label %q is outside the approved lexicon", name, text)
			}
			if text != types.CleanButtonText(text) {
				t.Errorf("%s: label %q carries an emoji prefix", name, text)
			}
			switch btn.Style {
			case types.ButtonStyleSuccess:
				if text != "搜索求片" {
					t.Errorf("%s: only 搜索求片 may be success, got %q", name, text)
				}
			case types.ButtonStyleDanger:
				if text != "取消操作" {
					t.Errorf("%s: only 取消操作 may be danger, got %q", name, text)
				}
			case types.ButtonStylePrimary:
				if text == "搜索求片" {
					t.Errorf("%s: 搜索求片 must stay success, got primary", name)
				}
			default:
				t.Errorf("%s: label %q has empty/unknown style %q", name, text, btn.Style)
			}
		}
	}
}

// rowMatrix flattens rows into a comparable [][]string of labels.
func rowMatrix(t *testing.T, rows [][]types.TelegramRichMessageButton) [][]string {
	t.Helper()
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		labels := make([]string, 0, len(row))
		for _, btn := range row {
			labels = append(labels, buttonText(t, btn))
		}
		out = append(out, labels)
	}
	return out
}

func TestWelcomeCardEmitsExactlyTheApprovedSixButtonGrid(t *testing.T) {
	rows := buttonRows(t, BuildPage(welcomePage()))
	assertGrid(t, "welcome", rows)
	got := rowMatrix(t, rows)
	want := [][]string{
		{"搜索求片", "查看进度", "申请洗版"},
		{"进入许愿", "帮助说明", "更多功能"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("welcome grid = %v, want %v", got, want)
	}
	// 搜索求片 is the only green control on the welcome screen.
	success := 0
	for _, row := range rows {
		for _, btn := range row {
			if btn.Style == types.ButtonStyleSuccess {
				success++
				if buttonText(t, btn) != "搜索求片" {
					t.Errorf("unexpected success button %q", buttonText(t, btn))
				}
			}
		}
	}
	if success != 1 {
		t.Fatalf("welcome must have exactly one success button, got %d", success)
	}
}

// TestEveryUserVisibleCardUsesTheThreeColumnGrid walks each card the user can
// actually see and enforces the shared grid + lexicon contract.
func TestEveryUserVisibleCardUsesTheThreeColumnGrid(t *testing.T) {
	cards := map[string]Card{
		"welcome":        BuildPage(welcomePage()),
		"more":           BuildPage(morePage()),
		"search_prompt":  BuildPage(searchPromptPage()),
		"help":           BuildPage(helpPage()),
		"settings_bound": BuildPage(settingsPage(true)),
		"settings_new":   BuildPage(settingsPage(false)),
		"progress_empty": BuildPage(progressEmptyPage()),
		"wash_prompt":    BuildPage(washPromptPage()),
		"playbill":       BuildPlaybillCard(PlaybillCard{Title: "醉玲珑", Year: "2017", Kind: "剧集", Next: "入库确认"}),
	}
	for name, card := range cards {
		assertGrid(t, name, buttonRows(t, card))
	}
}

// TestCallbackDataIsFrozen pins the callback payload for every visible button so
// a label or layout change can never silently repoint an action.
func TestCallbackDataIsFrozen(t *testing.T) {
	expected := map[string]string{
		"搜索求片": "search:menu",
		"查看进度": "requests",
		"申请洗版": "wash",
		"进入许愿": "start_wish",
		"帮助说明": "help",
		"更多功能": "start_more",
		"返回首页": "start",
		"刷新状态": "requests",
		"系统设置": "start_settings",
		"问题反馈": "issue",
		"游戏中心": "game_menu",
		"通知设置": "notify_settings",
		"绑定账号": "start_link",
		"重置密码": "resetpw",
		"我的反馈": "my_feedback",
		"观影周报": "weekly_report",
		"取消操作": "cancel",
	}
	cards := []Card{
		BuildPage(welcomePage()),
		BuildPage(morePage()),
		BuildPage(searchPromptPage()),
		BuildPage(helpPage()),
		BuildPage(settingsPage(true)),
		BuildPage(progressEmptyPage()),
		BuildPage(washPromptPage()),
		BuildPlaybillCard(PlaybillCard{Title: "醉玲珑", Year: "2017", Kind: "剧集", Next: "入库确认"}),
	}
	seen := map[string]bool{}
	for _, card := range cards {
		for _, row := range buttonRows(t, card) {
			for _, btn := range row {
				text := buttonText(t, btn)
				want, ok := expected[text]
				if !ok {
					t.Errorf("button %q has no frozen callback expectation", text)
					continue
				}
				if btn.CallbackData != want {
					t.Errorf("button %q callback_data=%q, want %q", text, btn.CallbackData, want)
				}
				seen[text] = true
			}
		}
	}
	for _, need := range []string{"搜索求片", "查看进度", "申请洗版", "进入许愿", "帮助说明", "更多功能", "返回首页", "刷新状态"} {
		if !seen[need] {
			t.Errorf("frozen callback check never exercised %q", need)
		}
	}
}

func TestStatusActionButtonsAreAThreeColumnRow(t *testing.T) {
	row := StatusActionButtons("requests")
	if len(row) != 3 {
		t.Fatalf("StatusActionButtons returned %d buttons, want 3", len(row))
	}
	assertGrid(t, "status_actions", [][]types.TelegramRichMessageButton{row})
	if buttonText(t, row[0]) != "返回首页" || buttonText(t, row[1]) != "刷新状态" || buttonText(t, row[2]) != "搜索求片" {
		t.Fatalf("unexpected status action order: %v", rowMatrix(t, [][]types.TelegramRichMessageButton{row}))
	}
	if row[1].CallbackData != "requests" {
		t.Fatalf("刷新状态 must keep the caller refresh callback, got %q", row[1].CallbackData)
	}
}

func TestRequesterReceiptCardsUseTheGridAndKeepCallbacks(t *testing.T) {
	rich := BuildReviewApprovedCard(ReviewApprovedData{Title: "银翼杀手", Year: 1982, MediaType: "电影"})
	if !strings.Contains(rich.Markdown, "查看进度") {
		t.Fatal("approved receipt must offer 查看进度")
	}
	for _, label := range []string{"帮助", "更多", "主菜单", "刷新", "洗版", "许愿池"} {
		for _, exact := range []string{"·" + label + "·", " " + label + " "} {
			if strings.Contains(rich.Markdown, exact) {
				t.Errorf("approved receipt leaks stale label %q", label)
			}
		}
	}
}

func TestNoUserVisibleButtonCarriesAnEmoji(t *testing.T) {
	cards := []Card{
		BuildPage(welcomePage()),
		BuildPage(morePage()),
		BuildPage(searchPromptPage()),
		BuildPage(helpPage()),
		BuildPage(settingsPage(false)),
		BuildPage(progressEmptyPage()),
		BuildPage(washPromptPage()),
	}
	for _, card := range cards {
		for _, row := range buttonRows(t, card) {
			for _, btn := range row {
				text := buttonText(t, btn)
				for _, r := range text {
					if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) || r == 0xfe0f {
						t.Errorf("button %q contains emoji rune %q", text, r)
					}
				}
			}
		}
	}
}
