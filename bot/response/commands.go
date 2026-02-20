package response

import (
	"fmt"
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

	// Different message for new vs returning users
	if b.context.IsNewUser {
		return b.buildWelcomeForNewUser()
	}

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("👋 欢迎回来！").
		WithMessage(fmt.Sprintf("你好，%s！", b.getDisplayName())).
		WithDetails("我可以帮你搜索和请求影视内容").
		WithSuggestions(
			"🔍 搜索内容 - 直接输入电影/剧集名称",
			"🎯 智能推荐 - 使用 /recommend",
			"📋 查看帮助 - 使用 /help",
		).
		Build()
}

// buildWelcomeForNewUser creates a welcome message for first-time users
func (b *CommandResponseBuilder) buildWelcomeForNewUser() *Response {
	builder := NewBuilder()

	message := "🎉 *欢迎来到云海看板娘！*\n\n"
	message += "我是你的智能影视助手，帮你：\n\n"
	message += "🔍 *搜索内容* - 直接输入电影/剧集名称\n"
	message += "📋 *发起请求* - 自动下载你想看的内容\n"
	message += "🔔 *自动通知* - 完成后第一时间通知你\n\n"
	message += "💡 *快速开始*\n"
	message += "试试输入：「复仇者联盟」"

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("👋 欢迎使用").
		WithMessage(message).
		WithAlert(true).
		Build()
}

// BuildHelpCommand creates the /help command response
// This provides comprehensive help information
func (b *CommandResponseBuilder) BuildHelpCommand() *Response {
	builder := NewBuilder()

	message := "📖 *使用指南*\n\n"
	message += "🔍 *搜索内容*\n"
	message += "直接输入电影或剧集名称即可搜索\n\n"

	message += "📋 *发起请求*\n"
	message += "搜索后点击「📋 请求」按钮\n\n"

	message += "🎯 *高级功能*\n"
	message += "`/recommend` - 智能推荐\n"
	message += "`/trending` - 热门搜索\n"
	message += "`/profile` - 我的资料\n"
	message += "`/link` - 绑定账号\n\n"

	message += "💡 *小贴士*\n"
	message += "• 支持中英文搜索\n"
	message += "• 可以按年份、类型筛选\n"
	message += "• 完成后自动通知你"

	return builder.
		WithType(ResponseTypeInfo).
		WithTitle("❓ 帮助中心").
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
