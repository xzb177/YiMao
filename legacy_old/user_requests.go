package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// UserRequestManager handles user self-service requests
type UserRequestManager struct {
	pendingSearches map[int64]*PendingSearch // userID -> search result
	searchMutex     sync.RWMutex
}

// PendingSearch holds search results with timeout
type PendingSearch struct {
	UserID    int64
	Query     string
	Results   []JellyseerrSearchResult
	CreatedAt time.Time
}

var userRequestMgr *UserRequestManager

// InitUserRequestManager initializes the user request manager
func InitUserRequestManager() {
	userRequestMgr = &UserRequestManager{
		pendingSearches: make(map[int64]*PendingSearch),
	}

	// Clean up old searches periodically
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanupOldSearches()
		}
	}()

	log.Println("User request manager initialized")
}

// cleanupOldSearches removes searches older than 10 minutes
func cleanupOldSearches() {
	userRequestMgr.searchMutex.Lock()
	defer userRequestMgr.searchMutex.Unlock()

	now := time.Now()
	for userID, search := range userRequestMgr.pendingSearches {
		if now.Sub(search.CreatedAt) > 10*time.Minute {
			delete(userRequestMgr.pendingSearches, userID)
		}
	}
}

// HandleMentionCommand handles @bot commands in group
func HandleMentionCommand(update *TelegramUpdate) bool {
	if update.Message == nil {
		return false
	}

	// Check if message mentions the bot
	text := update.Message.Text
	if !strings.Contains(text, "@yunhaisese_bot") && !strings.Contains(text, "/search") {
		return false
	}

	userID := update.Message.From.ID

	// Parse command
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return false
	}

	command := strings.ToLower(parts[0])
	var args string
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}

	// Handle @bot commands
	switch {
	case command == "@yunhaisese_bot" && len(parts) >= 2:
		subCommand := strings.ToLower(parts[1])
		switch subCommand {
		case "search", "搜索", "s":
			if len(parts) >= 3 {
				query := strings.Join(parts[2:], " ")
				handleSearchCommand(userID, query, update.Message.Chat.ID)
			} else {
				// Show quick search menu - disabled
				sendGroupMessage(update.Message.Chat.ID, "🔍 请输入搜索关键词")
			}
		case "help", "帮助":
			sendMentionHelp(userID, update.Message.Chat.ID)
		case "status", "状态":
			handleStatusCommand(userID, update.Message.Chat.ID)
		case "my", "我的", "myrequests":
			handleMyRequestsCommand(userID, update.Message.Chat.ID)
		default:
			sendMentionHelp(userID, update.Message.Chat.ID)
		}
		return true
	case strings.HasPrefix(command, "/search"):
		if args != "" {
			handleSearchCommand(userID, args, update.Message.Chat.ID)
		}
		return true
	}

	return false
}

// handleGroupNLPIntent handles NLP intents in group chat - simplified
func handleGroupNLPIntent(userID int64, chatID int64, intent Intent, params *SearchParams) {
	switch intent {
	case IntentSearch, IntentRequest, IntentMovie, IntentTV:
		if params.Query != "" {
			handleSearchCommand(userID, params.Query, chatID)
		} else {
			sendGroupMessage(chatID, "🔍 请输入搜索关键词")
		}
	case IntentStatus:
		handleMyRequestsCommand(userID, chatID)
	case IntentHelp:
		sendMentionHelp(userID, chatID)
	default:
		sendMentionHelp(userID, chatID)
	}
}

// getDisplayName gets user display name
func getDisplayName(user struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}) string {
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	if name == "" {
		name = user.Username
	}
	if name == "" {
		name = fmt.Sprintf("User_%d", user.ID)
	}
	return name
}

// handleSearchCommand handles media search
func handleSearchCommand(userID int64, query string, chatID int64) {
	// Use basic search
	handleBasicSearchCommand(userID, query, chatID)
}

// handleBasicSearchCommand handles basic search without smart search
func handleBasicSearchCommand(userID int64, query string, chatID int64) {
	if jellyseerrClient == nil {
		sendGroupMessage(chatID, "❌ 搜索功能暂不可用，请联系管理员")
		return
	}

	// Search media
	results, err := jellyseerrClient.SearchMedia(query)
	if err != nil {
		log.Printf("Error searching media: %v", err)
		sendGroupMessage(chatID, "❌ 搜索失败，请稍后再试")
		return
	}

	if len(results) == 0 {
		sendGroupMessage(chatID, fmt.Sprintf("🔍 未找到 \"%s\" 相关内容", query))
		return
	}

	// Store results for quick request
	userRequestMgr.searchMutex.Lock()
	userRequestMgr.pendingSearches[userID] = &PendingSearch{
		UserID:    userID,
		Query:     query,
		Results:   results,
		CreatedAt: time.Now(),
	}
	userRequestMgr.searchMutex.Unlock()

	// Send results with quick request buttons
	msg := fmt.Sprintf("🔍 *搜索结果: %s*\n\n", query)
	msg += fmt.Sprintf("找到 %d 个结果\n\n", len(results))

	// Show top 3 results
	for i, result := range results {
		if i >= 3 {
			msg += fmt.Sprintf("\n... 还有 %d 个结果，使用 /request 查看更多", len(results)-3)
			break
		}

		emoji := "🎬"
		typeName := "电影"
		if result.MediaType == "tv" {
			emoji = "📺"
			typeName = "剧集"
		}

		title := result.Title
		if title == "" {
			title = result.Name
		}

		year := ""
		if len(result.ReleaseDate) >= 4 {
			year = result.ReleaseDate[:4]
		}

		msg += fmt.Sprintf("%d. %s *%s*", i+1, emoji, title)
		if year != "" {
			msg += fmt.Sprintf(" (%s)", year)
		}
		msg += fmt.Sprintf(" - %s\n", typeName)
	}

	msg += "\n💡 点击下方按钮快速请求，或使用:"
	msg += fmt.Sprintf("\n`/request %d <类型>`", results[0].TmdbID)

	// Create inline keyboard with quick request buttons
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	for i, result := range results {
		if i >= 3 {
			break
		}
		title := result.Title
		if title == "" {
			title = result.Name
		}
		if len(title) > 20 {
			title = title[:20] + "..."
		}

		btnText := fmt.Sprintf("🎬 请求 %s", title)
		callbackData := fmt.Sprintf("request_%d_%s", result.TmdbID, result.MediaType)

		row := []map[string]string{
			{"text": btnText, "callback_data": callbackData},
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	sendGroupMessageWithKeyboard(chatID, msg, keyboard)
}

// sendGroupMessage sends a message to a group
func sendGroupMessage(chatID int64, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	msg := TelegramMessage{
		ChatID:    fmt.Sprintf("%d", chatID),
		Text:      text,
		ParseMode: "Markdown",
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Telegram API error: %s", string(bodyBytes))
		return fmt.Errorf("telegram API error: %d", resp.StatusCode)
	}

	return nil
}

// sendGroupMessageWithKeyboard sends a message with inline keyboard to group
func sendGroupMessageWithKeyboard(chatID int64, text string, keyboard *TelegramInlineKeyboard) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	msg := TelegramMessage{
		ChatID:      fmt.Sprintf("%d", chatID),
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Telegram API error: %s", string(bodyBytes))
		return fmt.Errorf("telegram API error: %d", resp.StatusCode)
	}

	return nil
}

// HandleQuickRequest handles quick request from inline button
func HandleQuickRequest(userID int64, tmdbID int, mediaType string) string {
	if jellyseerrClient == nil {
		return "❌ 请求功能暂不可用，请联系管理员"
	}

	// Get Jellyseerr user ID from Telegram user ID
	// This requires mapping or admin setup
	// For now, return instructions

	msg := fmt.Sprintf("📝 *快速请求*\n\n")
	msg += fmt.Sprintf("媒体ID: %d\n", tmdbID)
	msg += fmt.Sprintf("类型: %s\n\n", map[string]string{"movie": "电影", "tv": "剧集"}[mediaType])
	msg += "⚠️ 首次使用需要绑定 Jellyseerr 账号\n\n"
	msg += "请私聊机器人发送:"
	msg += fmt.Sprintf("\n`/link %d`", tmdbID)

	return msg
}

// sendMentionHelp sends help for mention commands
func sendMentionHelp(userID int64, chatID int64) {
	msg := "🤖 *云海看板娘 - 群组命令*\n\n"
	msg += "*使用方式:*\n"
	msg += "@yunhaisese_bot <命令> [参数]\n\n"
	msg += "*可用命令:*\n"
	msg += "• `@yunhaisese_bot search <关键词>` - 搜索媒体\n"
	msg += "• `@yunhaisese_bot 我的` - 查看我的请求\n"
	msg += "• `@yunhaisese_bot 状态` - 查看系统状态\n"
	msg += "• `@yunhaisese_bot help` - 显示此帮助\n\n"
	msg += "*快捷搜索:*\n"
	msg += "• `/search <关键词>` - 快速搜索并请求"

	sendGroupMessage(chatID, msg)
}

// handleStatusCommand shows system status
func handleStatusCommand(userID int64, chatID int64) {
	msg := "📊 *系统状态*\n\n"

	// Check services
	if jellyseerrClient != nil {
		msg += "✅ Jellyseerr API: 已连接\n"
	} else {
		msg += "❌ Jellyseerr API: 未配置\n"
	}

	// Admin count
	adminsMutex.RLock()
	adminCount := len(admins)
	adminsMutex.RUnlock()
	msg += fmt.Sprintf("👨‍💼 管理员: %d 人\n", adminCount)

	// Today's stats
	statsMutex.Lock()
	todayRequests := stats.RequestCount
	statsMutex.Unlock()
	msg += fmt.Sprintf("🎬 今日请求: %d\n", todayRequests)

	// Active users
	activeUsers := GetActiveUserCount()
	msg += fmt.Sprintf("👥 活跃用户: %d 人\n", activeUsers)

	msg += fmt.Sprintf("\n🕐 更新时间: %s", time.Now().Format("15:04:05"))

	sendGroupMessage(chatID, msg)
}

// handleMyRequestsCommand shows user's requests
func handleMyRequestsCommand(userID int64, chatID int64) {
	// Get user's requests from analytics
	analytics.mutex.RLock()
	defer analytics.mutex.RUnlock()

	userIDStr := fmt.Sprintf("%d", userID)
	var userRequests []RequestRecord
	for _, req := range analytics.Requests {
		if req.UserID == userIDStr {
			userRequests = append(userRequests, req)
		}
	}

	if len(userRequests) == 0 {
		sendGroupMessage(chatID, "📋 *我的请求*\n\n暂无请求记录")
		return
	}

	msg := "📋 *我的请求*\n\n"

	// Group by status
	pending := 0
	approved := 0
	available := 0
	declined := 0

	for _, req := range userRequests {
		switch req.Status {
		case "pending":
			pending++
		case "approved":
			approved++
		case "available":
			available++
		case "declined":
			declined++
		}
	}

	msg += fmt.Sprintf("⏳ 待处理: %d\n", pending)
	msg += fmt.Sprintf("✅ 已批准: %d\n", approved)
	msg += fmt.Sprintf("🎉 已可用: %d\n", available)
	msg += fmt.Sprintf("❌ 已拒绝: %d\n", declined)
	msg += fmt.Sprintf("\n📊 总计: %d 个请求", len(userRequests))

	sendGroupMessage(chatID, msg)
}
