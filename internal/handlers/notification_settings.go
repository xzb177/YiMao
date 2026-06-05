package handlers

import (
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/services"
)

// NotificationSettingsHandler 处理通知偏好设置。
type NotificationSettingsHandler struct {
	prefs *services.PreferencesService
}

// NewNotificationSettingsHandler 创建通知设置 handler。
func NewNotificationSettingsHandler(prefs *services.PreferencesService) *NotificationSettingsHandler {
	return &NotificationSettingsHandler{prefs: prefs}
}

// Handle 显示通知设置页面。
func (h *NotificationSettingsHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	status := h.prefs.GetNotifyStatus(ctx.UserID)

	msg := services.NewMessageBuilder()
	msg.Bold("🔔 通知设置").Newline()
	msg.Newline()
	msg.Text("选择要开关的通知类型").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton(notifyToggle("📥 入库通知", status[services.NotifyDownload]), "notify_toggle:key:download")
	kb.AddButton(notifyToggle("🎬 每日推荐", status[services.NotifyRecommend]), "notify_toggle:key:recommend")
	kb.NewRow()
	kb.AddButton(notifyToggle("📊 观影周报", status[services.NotifyWeekly]), "notify_toggle:key:weekly")
	kb.AddButton(notifyToggle("📢 系统公告", status[services.NotifyAnnounce]), "notify_toggle:key:announce")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// HandleToggle 处理通知开关切换。
func (h *NotificationSettingsHandler) HandleToggle(ctx *callback.Context) (*callback.Response, error) {
	notifyKey := ctx.Callback.Params["key"]
	if notifyKey == "" {
		return &callback.Response{CallbackMsg: "参数错误", ShowAlert: true}, nil
	}

	// 反转当前状态
	current := h.prefs.IsNotifyEnabled(ctx.UserID, notifyKey)
	if err := h.prefs.SetNotify(ctx.UserID, notifyKey, !current); err != nil {
		return &callback.Response{CallbackMsg: "设置失败", ShowAlert: true}, nil
	}

	stateText := "已开启"
	if current {
		stateText = "已关闭"
	}

	// 重新渲染设置页
	status := h.prefs.GetNotifyStatus(ctx.UserID)

	msg := services.NewMessageBuilder()
	msg.Bold("🔔 通知设置").Newline()
	msg.Newline()
	msg.Textf("✅ %s%s\n\n", notifyName(notifyKey), stateText)
	msg.Text("选择要开关的通知类型").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton(notifyToggle("📥 入库通知", status[services.NotifyDownload]), "notify_toggle:key:download")
	kb.AddButton(notifyToggle("🎬 每日推荐", status[services.NotifyRecommend]), "notify_toggle:key:recommend")
	kb.NewRow()
	kb.AddButton(notifyToggle("📊 观影周报", status[services.NotifyWeekly]), "notify_toggle:key:weekly")
	kb.AddButton(notifyToggle("📢 系统公告", status[services.NotifyAnnounce]), "notify_toggle:key:announce")
	kb.NewRow()
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}, nil
}

// notifyToggle 根据开关状态给按钮文字加标记。
func notifyToggle(label string, enabled bool) string {
	if enabled {
		return label + " ✅"
	}
	return label + " ❌"
}

// notifyName 通知类型的中文名。
func notifyName(key string) string {
	switch key {
	case services.NotifyDownload:
		return "入库通知"
	case services.NotifyRecommend:
		return "每日推荐"
	case services.NotifyWeekly:
		return "观影周报"
	case services.NotifyAnnounce:
		return "系统公告"
	default:
		return key
	}
}

// NotifyPrefKeyFromCallback 从回调参数提取通知 key（供 main.go 注册用）。
func NotifyPrefKeyFromCallback(ctx *callback.Context) string {
	return ctx.Callback.Params["key"]
}

// SettingsPageWithNotify 设置页加「通知设置」入口。
func SettingsPageWithNotify() *callback.Response {
	msg := services.NewMessageBuilder()
	msg.Bold("⚙️ 设置").Newline()
	msg.Newline()
	msg.Text("管理你的账号和偏好").Newline()

	kb := services.NewKeyboardBuilder()
	kb.AddButton("🔔 通知设置", "notify_settings")
	kb.AddButton("🔗 绑定账号", "start_link")
	kb.NewRow()
	kb.AddButton("🐞 我的反馈", "my_feedback")
	kb.AddButton("📊 观影周报", "weekly_report")
	kb.NewRow()
	kb.AddButton("❓ 帮助", "help")
	kb.AddButton("⬅️ 返回主菜单", "start")

	return &callback.Response{
		Text:     msg.Build(),
		Edit:     true,
		Keyboard: convertKeyboard(kb.Build()),
	}
}
