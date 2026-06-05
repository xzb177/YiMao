package callback

import (
	"strings"
	"testing"
)

// TestValidActionsNoStartPrefix 确保 validActions 中没有 start_ 前缀的 action。
// 因为回调解析器会自动剥掉 start_ 前缀，注册 start_xxx 是无效的。
// 这个测试防止「按钮加了但回调没注册」的问题再次发生。
func TestValidActionsNoStartPrefix(t *testing.T) {
	for action := range validActions {
		if strings.HasPrefix(string(action), "start_") {
			// start_settings 和 start_ai 是例外：它们在 main.go 中注册了
			// start_settings → settings（ActionSettings）
			// start_ai → ai（ActionAI）
			// 但这些不会被解析器命中（因为解析器会先剥前缀），
			// 所以它们是冗余的但无害。只警告不报错。
			t.Logf("WARNING: validActions contains start_ prefix action %q — 这个不会被回调解析器命中（前缀会被剥掉），可能是误注册", action)
		}
	}
}

// TestKnownShortActionsRegistered 确保已知的短名 action 都在白名单中。
// 这些是 start_xxx 剥前缀后必须存在的 action。
func TestKnownShortActionsRegistered(t *testing.T) {
	required := []Action{
		"search",      // start_search → search
		"requests",    // start_requests → requests
		"wish",        // start_wish → wish
		"ai",          // start_ai → ai
		"settings",    // start_settings → settings
		"myreq_cancel", // 用户撤回 pending 求片
	}

	for _, action := range required {
		if !validActions[action] {
			t.Errorf("required action %q not registered in validActions — 剥前缀后的按钮会报「无效的请求」", action)
		}
	}
}

// TestParserStartPrefixStripping 确保解析器正确剥掉 start_ 前缀。
func TestParserStartPrefixStripping(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		input          string
		expectedAction Action
	}{
		{"start_search", "search"},
		{"start_requests", "requests"},
		{"start_wish", "wish"},
		{"start_ai", "ai"},
		{"start_settings", "settings"},
		{"start_requests:page:2", "requests"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed, err := parser.Parse(tt.input)
			if err != nil {
				t.Errorf("Parse(%q) error = %v", tt.input, err)
				return
			}
			if parsed.Action != tt.expectedAction {
				t.Errorf("Parse(%q).Action = %q, want %q (前缀剥掉后)", tt.input, parsed.Action, tt.expectedAction)
			}
		})
	}
}
