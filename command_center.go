package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
)

// CommandCenter manages all bot commands
type CommandCenter struct {
	commands  map[string]*CommandHandler
	cmdMutex  sync.RWMutex
	categories map[string][]*CommandHandler
}

var commandCenter = &CommandCenter{
	commands:   make(map[string]*CommandHandler),
	categories: make(map[string][]*CommandHandler),
}

// CommandHandler represents a command handler
type CommandHandler struct {
	Command     string
	Aliases     []string
	Description string
	Category    string
	AdminOnly   bool
	NeedAuth    bool
	Handler     func(userID int64, args string) (string, *TelegramInlineKeyboard)
}

// CommandCategory represents a command category
type CommandCategory struct {
	Name        string
	Icon        string
	Description string
	Order       int
}

// All command categories
var CommandCategories = map[string]CommandCategory{
	"basic": {
		Name:        "基础",
		Icon:        "📱",
		Description: "入门命令",
		Order:       1,
	},
	"search": {
		Name:        "搜索",
		Icon:        "🔍",
		Description: "搜索和请求内容",
		Order:       2,
	},
	"personal": {
		Name:        "个人",
		Icon:        "👤",
		Description: "个人中心",
		Order:       3,
	},
	"social": {
		Name:        "社交",
		Icon:        "🏆",
		Description: "排行和成就",
		Order:       4,
	},
	"account": {
		Name:        "账号",
		Icon:        "🔗",
		Description: "账号绑定",
		Order:       5,
	},
	"admin": {
		Name:        "管理",
		Icon:        "🔧",
		Description: "管理员功能",
		Order:       6,
	},
}

// InitCommands initializes all commands
func InitCommands() {
	log.Println("CommandCenter: Initializing commands...")

	// Basic commands
	registerCmd(&CommandHandler{
		Command:     "start",
		Aliases:     []string{},
		Description: "👋 开始使用",
		Category:    "basic",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "help",
		Aliases:     []string{"h", "?"},
		Description: "❓ 帮助指南",
		Category:    "basic",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	// Search & Request
	registerCmd(&CommandHandler{
		Command:     "search",
		Aliases:     []string{"s", "find"},
		Description: "🔍 搜索内容",
		Category:    "search",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "request",
		Aliases:     []string{"req", "add"},
		Description: "📋 发起请求",
		Category:    "search",
		AdminOnly:   false,
		NeedAuth:    true,
	})

	registerCmd(&CommandHandler{
		Command:     "recommend",
		Aliases:     []string{"rec", "suggest"},
		Description: "🎯 智能推荐",
		Category:    "search",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "trending",
		Aliases:     []string{"hot"},
		Description: "🔥 热门搜索",
		Category:    "search",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "ai",
		Aliases:     []string{},
		Description: "🤖 AI助手",
		Category:    "search",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "history",
		Aliases:     []string{"hist"},
		Description: "📜 搜索历史",
		Category:    "search",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	// Personal
	registerCmd(&CommandHandler{
		Command:     "profile",
		Aliases:     []string{"me", "card", "rank"},
		Description: "👤 我的资料",
		Category:    "personal",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "daily",
		Aliases:     []string{"checkin", "bonus", "signin"},
		Description: "🎁 每日签到",
		Category:    "personal",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "my",
		Aliases:     []string{"myrequests", "status"},
		Description: "📋 我的请求",
		Category:    "personal",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "quota",
		Aliases:     []string{"limit"},
		Description: "📊 配额查询",
		Category:    "personal",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "prefs",
		Aliases:     []string{"settings", "config"},
		Description: "⚙️ 通知设置",
		Category:    "personal",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	// Social
	registerCmd(&CommandHandler{
		Command:     "leaderboard",
		Aliases:     []string{"rank", "lb"},
		Description: "🏆 排行榜",
		Category:    "social",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "challenges",
		Aliases:     []string{"tasks", "dailies"},
		Description: "🎯 每日挑战",
		Category:    "social",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "badges",
		Aliases:     []string{"achievements", "trophies"},
		Description: "🏅 我的成就",
		Category:    "social",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "top",
		Aliases:     []string{},
		Description: "🔥 热门内容",
		Category:    "social",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "activity",
		Aliases:     []string{"active"},
		Description: "👥 活跃用户",
		Category:    "social",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	// Account
	registerCmd(&CommandHandler{
		Command:     "link",
		Aliases:     []string{"bind", "connect"},
		Description: "🔗 绑定账号",
		Category:    "account",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "quicklink",
		Aliases:     []string{"fastbind"},
		Description: "🚀 快速绑定",
		Category:    "account",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "unlink",
		Aliases:     []string{"unbind", "disconnect"},
		Description: "🔓 解绑账号",
		Category:    "account",
		AdminOnly:   false,
		NeedAuth:    false,
	})

	// Admin commands
	registerCmd(&CommandHandler{
		Command:     "pending",
		Aliases:     []string{"queue"},
		Description: "⏳ 待处理",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "approve",
		Aliases:     []string{"accept"},
		Description: "✅ 批准",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "decline",
		Aliases:     []string{"reject", "deny"},
		Description: "❌ 拒绝",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "users",
		Aliases:     []string{"userlist"},
		Description: "👥 用户列表",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "bindrequests",
		Aliases:     []string{"bindreq", "breq"},
		Description: "📋 绑定请求",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "addadmin",
		Aliases:     []string{},
		Description: "➕ 添加管理员",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "deladmin",
		Aliases:     []string{"removeadmin"},
		Description: "➖ 删除管理员",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	registerCmd(&CommandHandler{
		Command:     "stats",
		Aliases:     []string{"statistics"},
		Description: "📊 系统统计",
		Category:    "admin",
		AdminOnly:   true,
		NeedAuth:    false,
	})

	log.Printf("CommandCenter: Registered %d commands in %d categories",
		len(commandCenter.commands), len(commandCenter.categories))
}

// registerCmd registers a command with its aliases
func registerCmd(cmd *CommandHandler) {
	commandCenter.cmdMutex.Lock()
	defer commandCenter.cmdMutex.Unlock()

	// Store main command
	commandCenter.commands[cmd.Command] = cmd

	// Store aliases
	for _, alias := range cmd.Aliases {
		commandCenter.commands[alias] = cmd
	}

	// Add to category
	commandCenter.categories[cmd.Category] = append(commandCenter.categories[cmd.Category], cmd)
}

// GetCommand returns a command handler by name
func GetCommand(command string) (*CommandHandler, bool) {
	commandCenter.cmdMutex.RLock()
	defer commandCenter.cmdMutex.RUnlock()

	// Normalize command (remove leading slash and lowercase)
	command = strings.TrimPrefix(command, "/")
	command = strings.ToLower(command)

	cmd, exists := commandCenter.commands[command]
	return cmd, exists
}

// GetAllCommands returns all registered commands
func GetAllCommands() map[string]*CommandHandler {
	commandCenter.cmdMutex.RLock()
	defer commandCenter.cmdMutex.RUnlock()

	result := make(map[string]*CommandHandler)
	for k, v := range commandCenter.commands {
		result[k] = v
	}
	return result
}

// GetCommandsByCategory returns commands grouped by category
func GetCommandsByCategory() map[string][]*CommandHandler {
	commandCenter.cmdMutex.RLock()
	defer commandCenter.cmdMutex.RUnlock()

	result := make(map[string][]*CommandHandler)
	for cat, cmds := range commandCenter.categories {
		result[cat] = make([]*CommandHandler, len(cmds))
		copy(result[cat], cmds)
	}
	return result
}

// isAdmin checks if user is admin
func isAdmin(telegramID int64) bool {
	telegramIDStr := strconv.FormatInt(telegramID, 10)
	adminsMutex.RLock()
	_, exists := admins[telegramIDStr]
	adminsMutex.RUnlock()
	return exists
}

// canExecuteCommand checks if user can execute a command
func canExecuteCommand(userID int64, cmd *CommandHandler) bool {
	if cmd.AdminOnly && !isAdmin(userID) {
		return false
	}
	if cmd.NeedAuth {
		if userSyncMgr == nil {
			return false
		}
		_, linked := userSyncMgr.GetJellyseerrUserID(userID)
		return linked
	}
	return true
}

// FormatCommandsByCategory formats commands by category for display
func FormatCommandsByCategory() string {
	categories := GetCommandsByCategory()

	msg := "📋 *完整命令列表*\n\n"

	// Sort categories by order
	categoryOrder := []string{"basic", "search", "personal", "social", "account", "admin"}

	for _, catKey := range categoryOrder {
		catInfo, exists := CommandCategories[catKey]
		if !exists {
			continue
		}

		cmds := categories[catKey]
		if len(cmds) == 0 {
			continue
		}

		msg += fmt.Sprintf("*%s %s*\n", catInfo.Icon, catInfo.Name)

		// Show only main commands (not aliases)
		shown := make(map[string]bool)
		for _, cmd := range cmds {
			if shown[cmd.Command] {
				continue
			}
			msg += fmt.Sprintf("/%s - %s\n", cmd.Command, cmd.Description)
			shown[cmd.Command] = true
		}
		msg += "\n"
	}

	return msg
}

// GetMenuCommands returns commands for the menu button
// Returns format required by Telegram setChatMenuButton API
func GetMenuCommands() map[string]map[string]string {
	commands := map[string]map[string]string{
		"basic": {
			"start": "👋 开始",
			"help":  "❓ 帮助",
		},
		"search": {
			"search":    "🔍 搜索",
			"recommend": "🎯 推荐",
			"random":    "🎲 随机",
		},
		"personal": {
			"my":    "📋 我的请求",
			"quota": "📊 配额",
			"daily": "🎁 签到",
		},
		"account": {
			"link":   "🔗 绑定账号",
			"prefs": "⚙️ 设置",
		},
		"admin": {
			"pending": "⏳ 待处理",
			"users":   "👥 用户",
			"stats":   "📊 统计",
		},
	}
	return commands
}

// FormatHelpMessage formats a help message with all available commands
func FormatHelpMessage(isAdmin bool) string {
	msg := "📖 使用指南\n\n"

	msg += "🔍 搜索\n"
	msg += "直接输入片名或 `/search` 关键词\n\n"

	msg += "📋 我的\n"
	msg += "`/my` - 请求状态 | `/quota` - 配额\n\n"

	msg += "🎯 推荐\n"
	msg += "`/recommend` - AI推荐 | `/random` - 随机\n\n"

	msg += "🔗 账号\n"
	msg += "`/link` 账号 密码 - 绑定 | `/unlink` - 解绑\n\n"

	msg += "⚙️ 设置\n"
	msg += "`/prefs` - 通知设置 | `/daily` - 签到\n"

	if isAdmin {
		msg += "\n🔧 管理员\n"
		msg += "`/pending` - 待处理 | `/users` - 用户\n"
		msg += "`/approve` ID - 批准 | `/decline` ID - 拒绝\n"
	}

	msg += "\n💡 点击左下角菜单快速访问"

	return msg
}
