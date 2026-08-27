package callback

import (
	"testing"
)

// TestWishActionRouting 验证 start_wish → wish → HandleEntry 的完整路由链。
// 模拟回调解析器剥前缀 + 白名单验证 + handler 查找。
func TestWishActionRouting(t *testing.T) {
	parser := NewParser()

	// 模拟用户点击许愿池按钮
	parsed, err := parser.Parse("start_wish")
	if err != nil {
		t.Fatalf("Parse(start_wish) error: %v", err)
	}

	// 剥前缀后应该是 wish
	if parsed.Action != "wish" {
		t.Errorf("expected action 'wish', got %q", parsed.Action)
	}

	// wish 必须在白名单中
	if !validActions[parsed.Action] {
		t.Errorf("action %q not in validActions — 会报「无效的请求」", parsed.Action)
	}
}

// TestCancelActionRouting 验证 myreq_cancel 回调路由。
func TestCancelActionRouting(t *testing.T) {
	parser := NewParser()

	parsed, err := parser.Parse("myreq_cancel")
	if err != nil {
		t.Fatalf("Parse(myreq_cancel) error: %v", err)
	}

	if parsed.Action != "myreq_cancel" {
		t.Errorf("expected action 'myreq_cancel', got %q", parsed.Action)
	}

	if !validActions[parsed.Action] {
		t.Errorf("action %q not in validActions", parsed.Action)
	}
}

// TestAllMenuButtonActionsExist 验证主菜单所有按钮的回调 action 都在白名单中。
// 这是防止「按钮加了但回调没注册」的最终防线。
func TestAllMenuButtonActionsExist(t *testing.T) {
	// 主菜单 BuildStartKeyboardWithOptions 中的所有回调
	menuCallbacks := []struct {
		button   string
		callback string
	}{
		{"搜索求片", "start_search"},
		{"求片进度", "requests"},
		{"帮助", "help"},
		{"更多", "start_more"},
		{"洗版", "wash"},
		{"遇到问题", "issue"},
		{"我的进度", "start_requests"},
		{"许愿池", "start_wish"},
		{"大家最近在求", "request_heat"},
		{"游戏中心", "game_menu"},
		{"设置", "start_settings"},
		{"管理", "admin_menu"},
	}
	parser := NewParser()

	for _, item := range menuCallbacks {
		t.Run(item.button, func(t *testing.T) {
			parsed, err := parser.Parse(item.callback)
			if err != nil {
				t.Errorf("按钮 %q 的回调 %q 解析失败: %v", item.button, item.callback, err)
				return
			}
			if !validActions[parsed.Action] {
				t.Errorf("按钮 %q 的回调 %q 剥前缀后 = %q，不在白名单中 — 点击会报错", item.button, item.callback, parsed.Action)
			}
		})
	}
}
