package types

import (
	"encoding/json"
	"testing"
)

// 审计 A8：Bot API 7.0+ 的 forward_origin 是 MessageOrigin 联合体，
// 真实用户在 sender_user 里；以前类型写成 *TelegramUser 导致 ID 恒为 0。
func TestForwardOriginUserParsed(t *testing.T) {
	raw := `{"message_id":1,"from":{"id":99,"first_name":"root"},"chat":{"id":1,"type":"private"},"date":1,
		"forward_origin":{"type":"user","sender_user":{"id":123456789,"first_name":"小号","username":"xh"},"date":1700000000},
		"text":"hello"}`
	var msg TelegramMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.ForwardOrigin == nil {
		t.Fatal("forward_origin 未解析")
	}
	u := msg.ForwardedUser()
	if u == nil || u.ID != 123456789 {
		t.Fatalf("应取出 sender_user 的 ID=123456789，实际 %+v", u)
	}
}

// hidden_user（对方隐私设置隐藏）拿不到 ID，应返回 nil 走手动输入。
func TestForwardOriginHiddenUserReturnsNil(t *testing.T) {
	raw := `{"message_id":1,"chat":{"id":1,"type":"private"},"date":1,
		"forward_origin":{"type":"hidden_user","sender_user_name":"匿名","date":1700000000},"text":"x"}`
	var msg TelegramMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.ForwardedUser() != nil {
		t.Fatal("hidden_user 应返回 nil（走手动输入）")
	}
}

// 旧格式 forward_from 仍然优先兼容。
func TestForwardFromLegacyStillWorks(t *testing.T) {
	raw := `{"message_id":1,"chat":{"id":1,"type":"private"},"date":1,
		"forward_from":{"id":555,"first_name":"旧"},"text":"x"}`
	var msg TelegramMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if u := msg.ForwardedUser(); u == nil || u.ID != 555 {
		t.Fatalf("旧格式 forward_from 应兼容，实际 %+v", u)
	}
}
