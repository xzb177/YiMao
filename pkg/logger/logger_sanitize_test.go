package logger

import (
	"strings"
	"testing"
)

// 审计第 1 条：日志脱敏正则把 Bot Token 分组写反，Token 原样留在日志里。
// 触发场景：getUpdates 超时，http.Client.Get 的 url.Error 带着完整 URL（含 token）。
func TestSanitizeMasksBotTokenInURL(t *testing.T) {
	token := "123456789:AAECa1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6"
	msg := `Get "https://api.telegram.org/bot` + token + `/getUpdates?offset=0&limit=100&timeout=10": context deadline exceeded`

	got := Sanitize(msg)
	if strings.Contains(got, token) {
		t.Fatalf("bot token leaked in sanitized log: %q", got)
	}
	if strings.Contains(got, "AAECa1b2") {
		t.Fatalf("bot token fragment leaked in sanitized log: %q", got)
	}
}

// 审计第 4 条：PT 站 passkey / sign 拼进请求 URL，出错时随 url.Error 进日志，
// 但脱敏规则里没有覆盖这些字段。
func TestSanitizeMasksPasskeyAndSign(t *testing.T) {
	passkey := "abcdef1234567890passkeysecret"
	sign := "xyz987signsecret654"
	msg := `Get "https://hdsky.me/torrentrss.php?passkey=` + passkey + `&rows=50&search=iron": dial tcp: i/o timeout; ` +
		`Get "https://rss.m-team.cc/api/rss/fetch?uid=12345&sign=` + sign + `&pageSize=100": dial tcp: i/o timeout`

	got := Sanitize(msg)
	if strings.Contains(got, passkey) {
		t.Fatalf("passkey leaked in sanitized log: %q", got)
	}
	if strings.Contains(got, sign) {
		t.Fatalf("sign leaked in sanitized log: %q", got)
	}
}

// 错误返回路径用全量遮蔽（不保留首尾字符），避免错误对象把 token 带出进程外。
func TestRedactTokensFully(t *testing.T) {
	token := "123456789:AAECa1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6"
	got := RedactTokens(`request to https://api.telegram.org/bot` + token + `/sendMessage failed`)
	if strings.Contains(got, token) || strings.Contains(got, "AAECa1b2") {
		t.Fatalf("bot token leaked in redacted error: %q", got)
	}
	if strings.Contains(got, "sendMessage") == false {
		t.Fatalf("non-sensitive content should be preserved: %q", got)
	}
}
