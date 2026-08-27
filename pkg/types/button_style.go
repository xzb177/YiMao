package types

import "strings"

const (
	ButtonStylePrimary = "primary"
	ButtonStyleSuccess = "success"
	ButtonStyleDanger  = "danger"
)

func ButtonStyleFor(text, callback string) string {
	raw := strings.TrimSpace(text)
	lower := strings.ToLower(raw)
	action := strings.ToLower(strings.TrimSpace(callback))
	actionName := action
	if i := strings.IndexByte(actionName, ':'); i >= 0 {
		actionName = actionName[:i]
	}

	for _, marker := range []string{"删除", "清空", "解绑", "撤回", "拒绝", "移除", "注销", "取消订阅", "停止追问", "delete", "clear", "unlink", "withdraw", "reject", "remove", "abandon", "cancel_subscription", "stop_follow"} {
		if strings.Contains(lower, marker) || strings.Contains(actionName, marker) {
			return ButtonStyleDanger
		}
	}
	if strings.Contains(raw, "关闭") && strings.Contains(actionName, "close") {
		return ButtonStyleDanger
	}
	if strings.Contains(raw, "求片进度") || strings.Contains(raw, "我的进度") {
		return ButtonStylePrimary
	}
	if strings.Contains(raw, "搜索求片") || strings.Contains(raw, "立即求片") || strings.Contains(raw, "洗版") {
		return ButtonStyleSuccess
	}
	if raw == "求片" || strings.HasSuffix(raw, " 求片") {
		return ButtonStyleSuccess
	}
	for _, marker := range []string{"帮助", "更多", "返回", "主菜单", "刷新"} {
		if strings.Contains(raw, marker) {
			return ButtonStylePrimary
		}
	}
	for _, marker := range []string{"确认", "批准", "已解决", "confirm", "approve", "fixed"} {
		if strings.Contains(lower, marker) || strings.Contains(actionName, marker) {
			return ButtonStyleSuccess
		}
	}
	switch actionName {
	case "start_search", "search", "request", "wash", "force_subscribe", "wish_add":
		return ButtonStyleSuccess
	case "requests", "help", "more", "start", "start_more", "admin_todo", "admin_pending":
		return ButtonStylePrimary
	}
	return ""
}
