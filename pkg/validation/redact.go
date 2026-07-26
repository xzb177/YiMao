package validation

import "strings"

// RedactSensitiveText 返回适合写日志的消息文本。
// /link、/resetpw 等命令的参数里可能带明文密码，任何日志都不允许保留：
// 只保留命令词本身，参数一律用 [redacted] 替代。
// 非命令文本原样返回（搜索词等不含凭据）。
func RedactSensitiveText(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return text
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return text
	}
	cmd := strings.ToLower(fields[0])
	// 去掉 @botname 后缀再判断
	if at := strings.Index(cmd, "@"); at > 0 {
		cmd = cmd[:at]
	}
	sensitive := map[string]bool{
		"/link":    true,
		"/resetpw": true,
	}
	if !sensitive[cmd] {
		return text
	}
	if len(fields) == 1 {
		return fields[0]
	}
	return fields[0] + " [redacted]"
}
