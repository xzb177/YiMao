package services

import (
	"strings"
	"testing"
)

// TestEscapeCarpoolMentionText 覆盖 B4：Markdown 链接文本里的特殊字符被转义，
// 避免 [..](..) 结构被昵称里的括号/星号破坏。
func TestEscapeCarpoolMentionText(t *testing.T) {
	in := "Bob [VIP] (ace) *star* a_b `c`"
	out := escapeCarpoolMentionText(in)
	for _, ch := range []string{"[", "]", "(", ")", "*", "`"} {
		if strings.Contains(out, ch) {
			t.Errorf("转义后仍含危险字符 %q: %q", ch, out)
		}
	}
}

// TestCarpoolMaxMentions 确认长度保护常量合理（>0 且不至于撑爆 4096）。
func TestCarpoolMaxMentions(t *testing.T) {
	if carpoolMaxMentions <= 0 {
		t.Fatalf("carpoolMaxMentions 必须为正: %d", carpoolMaxMentions)
	}
	// 粗略上界：每个 mention 约 50 字符，20 人约 1000 字符，远低于 4096。
	if carpoolMaxMentions*60 > 4096 {
		t.Fatalf("carpoolMaxMentions=%d 可能撑爆 4096 上限", carpoolMaxMentions)
	}
}
