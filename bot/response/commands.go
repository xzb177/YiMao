package response

import (
	"fmt"
	"time"
)

// CommandType represents different command types for responses
type CommandType string

const (
	CommandStart  CommandType = "start"
	CommandHelp   CommandType = "help"
	CommandSearch CommandType = "search"
	CommandAI     CommandType = "ai"
)

// CommandContext provides context for command responses
type CommandContext struct {
	UserID      int64
	Username    string
	FirstName   string
	IsAdmin     bool
	IsNewUser   bool
	IsReturning bool
}

// CommandResponseBuilder builds command-specific responses
type CommandResponseBuilder struct {
	context *CommandContext
}

// NewCommandBuilder creates a new command response builder
func NewCommandBuilder(ctx *CommandContext) *CommandResponseBuilder {
	if ctx == nil {
		ctx = &CommandContext{}
	}
	return &CommandResponseBuilder{context: ctx}
}

// BuildStartCommand creates the /start command response
// This is the first interaction users have with the bot
func (b *CommandResponseBuilder) BuildStartCommand() *Response {
	builder := NewBuilder()

	// 不同用户返回不同消息
	if b.context.IsNewUser {
		return b.buildWelcomeForNewUser()
	}

	// 回归用户 - 个性化欢迎
	displayName := b.getDisplayName()

	// 根据时间段生成问候
	hour := time.Now().Hour()
	var greeting string
	switch {
	case hour >= 5 && hour < 9:
		greeting = "早安"
	case hour >= 9 && hour < 12:
		greeting = "上午好"
	case hour >= 12 && hour < 14:
		greeting = "中午好"
	case hour >= 14 && hour < 18:
		greeting = "下午好"
	case hour >= 18 && hour < 23:
		greeting = "晚上好"
	default:
		greeting = "夜深了"
	}

	message := fmt.Sprintf("👋 *%s，%s！*\n\n", greeting, displayName)
	message += "又见面啦~ 今天想看点什么？\n\n"
	message += "━━━━━━━━━━━━━━━━━━━━━━\n\n"
	message += "🎬 *探索新片*\n"
	message += "直接输入影片名称搜索\n\n"

	message += "🤖 *AI 推荐*\n"
	message += "`/ai 推荐` - 智能推荐\n"
	message += "`/trending` - 热门榜单\n\n"

	message += "📋 *我的请求*\n"
	message += "`/my` - 查看我的请求\n\n"

	message += "⚙️ *更多*\n"
	message += "`/help` - 查看所有命令"

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("👋 欢迎回来").
		WithMessage(message).
		WithDetails("今天想看什么呢？").
		WithSuggestions(
			"🔍 搜索影片 - 输入名称即可",
			"🤖 AI推荐 - /ai 推荐",
			"🔥 热门榜单 - /trending",
			"📋 我的请求 - /my",
		).
		Build()
}

// buildWelcomeForNewUser creates a welcome message for first-time users
func (b *CommandResponseBuilder) buildWelcomeForNewUser() *Response {
	builder := NewBuilder()

	displayName := b.getDisplayName()

	// 根据时间段生成问候
	hour := time.Now().Hour()
	var timeGreeting string
	switch {
	case hour >= 5 && hour < 9:
		timeGreeting = "美好的一天"
	case hour >= 9 && hour < 12:
		timeGreeting = "充满活力的上午"
	case hour >= 12 && hour < 14:
		timeGreeting = "惬意的午后"
	case hour >= 14 && hour < 18:
		timeGreeting = "美好的下午"
	case hour >= 18 && hour < 23:
		timeGreeting = "精彩的夜晚"
	default:
		timeGreeting = "宁静的深夜"
	}

	message := fmt.Sprintf("🌟 *欢迎，%s！*\n\n", displayName)
	message += "我是云海看板娘，你的智能影视助手~\n\n"
	message += "━━━━━━━━━━━━━━━━━━━━━━\n\n"
	message += "🎬 *我能为你做什么？*\n\n"

	message += "🔍 **一键搜索**\n"
	message += "直接告诉我片名，「复仇者联盟」「权力的游戏」都行\n\n"

	message += "📋 **智能求片**\n"
	message += "搜索后点击「📋 请求」，管理员批准后自动下载\n"
	message += "下载完成第一时间通知你~\n\n"

	message += "🤖 **AI 推荐**\n"
	message += "不知道看什么？试试：\n"
	message += "• `/ai 推荐` - 随机推荐好片\n"
	message += "• `/ai 推荐 喜剧` - 按类型推荐\n"
	message += "• `/ai 推荐 怀疑片` - 看你的口味\n\n"

	message += "━━━━━━━━━━━━━━━━━━━━━━\n\n"
	message += "💡 *新手必看*\n"
	message += "第一次使用？试试输入：**盗梦空间**"

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("🎉 欢迎使用").
		WithMessage(message).
		WithAlert(true).
		WithDetails("开始你的影视之旅").
		WithSuggestions(
			"🔍 搜索影片 - 输入「盗梦空间」试试",
			"🤖 AI 推荐 - /ai 推荐",
			"📋 查看帮助 - /help",
		).
		Build()
}

// BuildHelpCommand creates the /help command response
// This provides comprehensive help information
func (b *CommandResponseBuilder) BuildHelpCommand() *Response {
	builder := NewBuilder()

	message := "🤖 *影视助手使用指南*\n\n"
	message += "━━━━━━━━━━━━━━━━━━━━━━\n\n"

	message += "🔍 *搜索影片*\n"
	message += "直接输入影片名称即可\n"
	message += "支持中文、英文、拼音搜索\n\n"

	message += "📋 *发起请求*\n"
	message += "搜索结果中点击「📋 请求」按钮\n"
	message += "管理员批准后自动下载\n\n"

	message += "🤖 *AI 智能推荐*\n"
	message += "`/ai 推荐 <类型>` - 智能推荐\n"
	message += "`/ai 推荐 喜剧` - 推荐喜剧片\n"
	message += "`/ai 推荐 悬疑` - 推荐悬疑片\n"
	message += "`/trending` - 查看热门搜索\n\n"

	message += "👤 *个人中心*\n"
	message += "`/my` 或 `/me` - 我的请求\n"
	message += "`/prefs` - 通知设置\n\n"

	message += "🔗 *账号绑定*\n"
	message += "`/link` - 绑定 Jellyseerr 账号\n"
	message += "`/verify` - 获取验证码\n"
	message += "绑定后可直接从 Jellyseerr 请求\n\n"

	message += "━━━━━━━━━━━━━━━━━━━━━━\n\n"
	message += "💡 *使用小贴士*\n"
	message += "• 搜索结果支持按类型/年份筛选\n"
	message += "• 请求完成后会收到通知\n"
	message += "• 建议绑定 Jellyseerr 账号获得更好体验"

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("📖 使用指南").
		WithMessage(message).
		Build()
}

// BuildAdminHelpCommand creates the /help command response for admins
func (b *CommandResponseBuilder) BuildAdminHelpCommand() *Response {
	builder := NewBuilder()

	message := "🔧 *管理员功能*\n\n"
	message += "📋 *请求管理*\n"
	message += "`/pending` - 查看待处理请求\n"
	message += "`/approve <ID>` - 批准请求\n"
	message += "`/decline <ID>` - 拒绝请求\n\n"

	message += "👥 *用户管理*\n"
	message += "`/users` - 查看用户列表\n"
	message += "`/bindrequests` - 绑定请求\n\n"

	message += "📊 *系统管理*\n"
	message += "`/stats` - 系统统计\n"
	message += "`/addadmin` - 添加管理员\n"

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("🔧 管理员帮助").
		WithMessage(message).
		Build()
}

// getDisplayName returns the display name for the user
func (b *CommandResponseBuilder) getDisplayName() string {
	if b.context.FirstName != "" {
		return b.context.FirstName
	}
	if b.context.Username != "" {
		return "@" + b.context.Username
	}
	return "朋友"
}

// GetCommandResponse returns a formatted command response message
// This is a legacy compatibility method
func GetCommandResponse(cmdType CommandType, ctx *CommandContext) string {
	builder := NewCommandBuilder(ctx)

	switch cmdType {
	case CommandStart:
		resp := builder.BuildStartCommand()
		return resp.String()

	case CommandHelp:
		resp := builder.BuildHelpCommand()
		return resp.String()

	default:
		return "❓ 未知命令"
	}
}

// FormatStartMessage returns the formatted /start message (legacy)
func FormatStartMessage(username string, isNewUser bool) string {
	ctx := &CommandContext{
		Username:  username,
		IsNewUser: isNewUser,
	}
	resp := NewCommandBuilder(ctx).BuildStartCommand()
	return resp.String()
}

// FormatHelpCommandMessage returns the formatted /help message
// This is the response package version, different from command_center's FormatHelpMessage
func FormatHelpCommandMessage(isAdmin bool) string {
	ctx := &CommandContext{
		IsAdmin: isAdmin,
	}

	var builder *CommandResponseBuilder
	var resp *Response

	if isAdmin {
		builder = NewCommandBuilder(ctx)
		resp = builder.BuildAdminHelpCommand()
	} else {
		builder = NewCommandBuilder(ctx)
		resp = builder.BuildHelpCommand()
	}

	return resp.String()
}
