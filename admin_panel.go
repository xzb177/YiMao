package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AdminPanelManager handles admin panel operations
type AdminPanelManager struct {
	// Cache for admin panel data
	panelCache      map[int64]*AdminPanelData
	cacheMutex      sync.RWMutex
	cacheExpiry     time.Time

	// Quick action handlers
	actionHandlers map[string]AdminActionHandler
}

// AdminPanelData represents admin panel data
type AdminPanelData struct {
	PendingRequests []JellyseerrRequest
	Stats           Statistics
	LastUpdated     time.Time
	StuckRequests   []*TrackedRequest
}

// AdminActionHandler handles admin actions
type AdminActionHandler func(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error)

// AdminActionType represents types of admin actions
type AdminActionType string

const (
	AdminActionPending      AdminActionType = "pending"      // View pending requests
	AdminActionApprove      AdminActionType = "approve"      // Approve a request
	AdminActionDecline      AdminActionType = "decline"      // Decline a request
	AdminActionStats        AdminActionType = "stats"        // View statistics
	AdminActionStuck        AdminActionType = "stuck"        // View stuck requests
	AdminActionUsers        AdminActionType = "users"        // View user list
	AdminActionAdmins       AdminActionType = "admins"       // Manage admins
	AdminActionTrends       AdminActionType = "trends"       // View trends
	AdminActionMedia        AdminActionType = "media"        // Top media
	AdminActionApproveBatch AdminActionType = "approve_batch" // Batch approve
)

var adminPanelMgr *AdminPanelManager

// InitAdminPanelManager initializes the admin panel manager
func InitAdminPanelManager() {
	adminPanelMgr = &AdminPanelManager{
		panelCache:      make(map[int64]*AdminPanelData),
		cacheExpiry:     time.Now(),
		actionHandlers: make(map[string]AdminActionHandler),
	}

	// Register action handlers
	adminPanelMgr.registerHandlers()

	log.Println("Admin panel manager initialized")
}

// registerHandlers registers admin action handlers
func (m *AdminPanelManager) registerHandlers() {
	m.actionHandlers[string(AdminActionPending)] = m.handlePending
	m.actionHandlers[string(AdminActionApprove)] = m.handleApprove
	m.actionHandlers[string(AdminActionDecline)] = m.handleDecline
	m.actionHandlers[string(AdminActionStats)] = m.handleStats
	m.actionHandlers[string(AdminActionStuck)] = m.handleStuck
	m.actionHandlers[string(AdminActionUsers)] = m.handleUsers
	m.actionHandlers[string(AdminActionAdmins)] = m.handleAdmins
	m.actionHandlers[string(AdminActionTrends)] = m.handleTrends
	m.actionHandlers[string(AdminActionMedia)] = m.handleMedia
}

// SendAdminPanel sends admin panel to user
func SendAdminPanel(userID int64) (string, *TelegramInlineKeyboard, error) {
	if !IsAdmin(userID) {
		return "❌ 你不是管理员", nil, fmt.Errorf("user is not admin")
	}

	// Get panel data
	data, err := adminPanelMgr.getPanelData()
	if err != nil {
		return "❌ 获取数据失败", nil, err
	}

	// Format panel message
	msg := "👨‍💼 *管理员面板*\n\n"

	// Summary section
	pendingCount := len(data.PendingRequests)
	msg += fmt.Sprintf("⏳ 待处理请求: %d\n", pendingCount)

	if pendingCount > 0 {
		msg += fmt.Sprintf("_点击下方按钮查看详情_\n")
	}

	// Quick stats
	msg += fmt.Sprintf("\n📊 *今日统计*\n")
	msg += fmt.Sprintf("🎬 新请求: %d\n", data.Stats.RequestCount)
	msg += fmt.Sprintf("✅ 已批准: %d\n", data.Stats.ApprovedCount)
	msg += fmt.Sprintf("❌ 已拒绝: %d\n", data.Stats.DeclinedCount)
	msg += fmt.Sprintf("🎉 已可用: %d\n", data.Stats.AvailableCount)

	// System status
	msg += fmt.Sprintf("\n🔧 *系统状态*\n")
	if jellyseerrClient != nil {
		msg += "✅ Jellyseerr: 已连接\n"
	} else {
		msg += "❌ Jellyseerr: 未配置\n"
	}

	adminCount := GetAdminCount()
	msg += fmt.Sprintf("👨‍💼 管理员: %d 人\n", adminCount)

	activeUsers := GetActiveUserCount()
	msg += fmt.Sprintf("👥 活跃用户: %d 人", activeUsers)

	// Create inline keyboard
	keyboard := adminPanelMgr.buildMainKeyboard(pendingCount > 0)

	return msg, keyboard, nil
}

// buildMainKeyboard builds the main admin panel keyboard
func (m *AdminPanelManager) buildMainKeyboard(hasPending bool) *TelegramInlineKeyboard {
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	// Pending requests button (highlighted if exists)
	pendingBtn := "📋 待处理请求"
	if hasPending {
		pendingBtn = "📋 待处理请求 (" + fmt.Sprint(getPendingCount()) + ")"
	}

	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []map[string]string{
		{"text": pendingBtn, "callback_data": "admin_pending"},
	})

	// Quick stats row
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []map[string]string{
		{"text": "📊 统计", "callback_data": "admin_stats"},
		{"text": "📈 趋势", "callback_data": "admin_trends"},
	})

	// More options row
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []map[string]string{
		{"text": "🔥 超时请求", "callback_data": "admin_stuck"},
		{"text": "🎬 热门媒体", "callback_data": "admin_media"},
	})

	// User management row
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []map[string]string{
		{"text": "👥 活跃用户", "callback_data": "admin_users"},
	})

	return keyboard
}

// HandleAdminCallback handles admin panel button callbacks
func HandleAdminCallback(userID int64, action string, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	if !IsAdmin(userID) {
		return "❌ 你不是管理员", nil, fmt.Errorf("user is not admin")
	}

	// Parse action
	parts := strings.Split(action, "_")
	if len(parts) < 2 {
		return "❌ 无效的操作", nil, fmt.Errorf("invalid action")
	}

	actionType := strings.Join(parts[1:], "_")

	// Find and execute handler
	handler, exists := adminPanelMgr.actionHandlers[actionType]
	if !exists {
		return SendAdminPanel(userID)
	}

	return handler(userID, params)
}

// getPanelData gets current panel data
func (m *AdminPanelManager) getPanelData() (*AdminPanelData, error) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	// Check if cache is still valid
	if time.Now().Before(m.cacheExpiry) && len(m.panelCache) > 0 {
		for _, data := range m.panelCache {
			return data, nil
		}
	}

	// Fetch fresh data
	data := &AdminPanelData{
		LastUpdated: time.Now(),
	}

	// Get pending requests
	if jellyseerrClient != nil {
		requests, err := jellyseerrClient.GetPendingRequests()
		if err == nil {
			data.PendingRequests = requests
		}
	}

	// Get stats
	statsMutex.Lock()
	data.Stats = stats
	statsMutex.Unlock()

	// Get stuck requests
	data.StuckRequests = GetStuckRequests()

	// Update cache
	m.panelCache[0] = data // Use 0 as shared key
	m.cacheExpiry = time.Now().Add(5 * time.Minute)

	return data, nil
}

// Handler implementations

func (m *AdminPanelManager) handlePending(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	if jellyseerrClient == nil {
		return "❌ Jellyseerr API 未配置", nil, nil
	}

	requests, err := jellyseerrClient.GetPendingRequests()
	if err != nil {
		return "❌ 获取请求失败: " + err.Error(), nil, nil
	}

	if len(requests) == 0 {
		msg := "✅ *没有待处理的请求*\n\n"
		msg += "所有请求都已处理完毕！"
		return msg, m.buildBackKeyboard(), nil
	}

	msg := fmt.Sprintf("📋 *待处理请求* (%d)\n\n", len(requests))

	// Show up to 5 requests with inline buttons
	displayCount := 5
	if len(requests) < displayCount {
		displayCount = len(requests)
	}

	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	for i, req := range requests {
		if i >= displayCount {
			msg += fmt.Sprintf("\n... 还有 %d 个请求", len(requests)-displayCount)
			break
		}

		emoji := "🎬"
		if req.Type == "tv" {
			emoji = "📺"
		}

		// Safely get title from Media (which can be nil)
		title := "未知标题"
		if req.Media != nil {
			title = req.Media.Title
			if title == "" {
				title = req.Media.Name
			}
		}

		msg += fmt.Sprintf("%d. %s *%s*\n", i+1, emoji, title)

		if req.RequestedBy != nil {
			name := req.RequestedBy.DisplayName
			if name == "" {
				name = req.RequestedBy.Username
			}
			if name == "" {
				name = req.RequestedBy.Email
			}
			msg += fmt.Sprintf("   👤 %s", name)
		}

		msg += fmt.Sprintf("\n   🕐 %s\n\n", req.CreatedAt.Format("01-02 15:04"))

		// Add action buttons for each request
		row := []map[string]string{
			{"text": "✅", "callback_data": fmt.Sprintf("admin_approve_%d", req.ID)},
			{"text": "❌", "callback_data": fmt.Sprintf("admin_decline_%d", req.ID)},
			{"text": "🔗", "url": fmt.Sprintf("%s/requests/%d", jellyseerrURL, req.ID)},
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	// Add back button
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []map[string]string{
		{"text": "🔙 返回", "callback_data": "admin_main"},
	})

	return msg, keyboard, nil
}

func (m *AdminPanelManager) handleApprove(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	requestIDStr := params["id"]
	if requestIDStr == "" {
		return "❌ 缺少请求ID", nil, nil
	}

	requestID := 0
	fmt.Sscanf(requestIDStr, "%d", &requestID)

	if requestID == 0 {
		return "❌ 无效的请求ID", nil, nil
	}

	if jellyseerrClient == nil {
		return "❌ Jellyseerr API 未配置", nil, nil
	}

	if err := jellyseerrClient.ApproveRequest(requestID); err != nil {
		return "❌ 批准失败: " + err.Error(), nil, nil
	}

	// Get request details for message
	request, err := jellyseerrClient.GetRequest(requestID)
	if err == nil && request.Media != nil {
		title := request.Media.Title
		msg := fmt.Sprintf("✅ *已批准*\n\n")
		msg += fmt.Sprintf("📦 %s\n", title)
		msg += "\nJellyseerr 正在处理..."
		return msg, m.buildBackKeyboard(), nil
	}

	return "✅ 请求已批准", m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleDecline(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	requestIDStr := params["id"]
	if requestIDStr == "" {
		return "❌ 缺少请求ID", nil, nil
	}

	requestID := 0
	fmt.Sscanf(requestIDStr, "%d", &requestID)

	if requestID == 0 {
		return "❌ 无效的请求ID", nil, nil
	}

	if jellyseerrClient == nil {
		return "❌ Jellyseerr API 未配置", nil, nil
	}

	if err := jellyseerrClient.DeclineRequest(requestID); err != nil {
		return "❌ 拒绝失败: " + err.Error(), nil, nil
	}

	return "❌ 请求已拒绝", m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleStats(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	msg := "📊 *统计数据*\n\n"
	msg += fmt.Sprintf("📅 日期: %s\n\n", stats.Date)

	msg += "*今日请求:*\n"
	msg += fmt.Sprintf("🎬 新请求: %d\n", stats.RequestCount)
	msg += fmt.Sprintf("✅ 已批准: %d\n", stats.ApprovedCount)
	msg += fmt.Sprintf("❌ 已拒绝: %d\n", stats.DeclinedCount)
	msg += fmt.Sprintf("🎉 已可用: %d\n\n", stats.AvailableCount)

	msg += "*其他:*\n"
	msg += fmt.Sprintf("🐛 问题报告: %d\n", stats.IssueCount)
	msg += fmt.Sprintf("📀 新增媒体: %d\n", stats.MediaAdded)

	return msg, m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleStuck(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	stuck := GetStuckRequests()

	if len(stuck) == 0 {
		msg := "✅ *没有超时的请求*\n\n"
		msg += "所有请求都在正常处理中"
		return msg, m.buildBackKeyboard(), nil
	}

	msg := fmt.Sprintf("⚠️ *超时未处理请求* (%d)\n\n", len(stuck))

	for i, req := range stuck {
		if i >= 10 {
			msg += fmt.Sprintf("\n... 还有 %d 个请求", len(stuck)-10)
			break
		}

		emoji := "🎬"
		if req.MediaType == "tv" {
			emoji = "📺"
		}

		msg += fmt.Sprintf("%d. %s %s\n", i+1, emoji, req.MediaTitle)
		msg += fmt.Sprintf("   👤 %s | ⏱️ %s\n\n", req.RequesterName, formatDuration(time.Since(req.RequestedAt)))
	}

	return msg, m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleUsers(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	topUsers := GetTopUsers(10)

	if len(topUsers) == 0 {
		return "📊 *暂无用户数据*", m.buildBackKeyboard(), nil
	}

	msg := "👥 *活跃用户排行*\n\n"

	for i, user := range topUsers {
		if i >= 10 {
			break
		}
		msg += fmt.Sprintf("%d. %s - %d 个请求\n", i+1, user.Username, user.RequestCount)
	}

	return msg, m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleAdmins(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	if len(admins) == 0 {
		return "📋 *暂无管理员*", m.buildBackKeyboard(), nil
	}

	msg := "👨‍💼 *管理员列表*\n\n"

	for uid, name := range admins {
		msg += fmt.Sprintf("• %s (`%s`)\n", name, uid)
	}

	msg += fmt.Sprintf("\n共 %d 位管理员", len(admins))

	return msg, m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleTrends(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	trends := GetTrends(7)

	msg := "📈 *7天请求趋势*\n\n"

	for _, trend := range trends {
		msg += fmt.Sprintf("• %s: %d 个请求\n", trend.Date, trend.RequestCount)
	}

	return msg, m.buildBackKeyboard(), nil
}

func (m *AdminPanelManager) handleMedia(userID int64, params map[string]string) (string, *TelegramInlineKeyboard, error) {
	topMedia := GetTopMedia(10)

	if len(topMedia) == 0 {
		return "🎬 *暂无媒体数据*", m.buildBackKeyboard(), nil
	}

	msg := "🔥 *热门媒体排行*\n\n"

	for i, media := range topMedia {
		if i >= 10 {
			break
		}

		emoji := "🎬"
		if media.MediaType == "tv" {
			emoji = "📺"
		}

		msg += fmt.Sprintf("%d. %s *%s*\n", i+1, emoji, media.MediaTitle)
		msg += fmt.Sprintf("   👥 %d 次请求\n", media.RequestCount)
	}

	return msg, m.buildBackKeyboard(), nil
}

// buildBackKeyboard builds a keyboard with back button
func (m *AdminPanelManager) buildBackKeyboard() *TelegramInlineKeyboard {
	return &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{{"text": "🔙 返回主面板", "callback_data": "admin_main"}},
			{{"text": "🔄 刷新", "callback_data": "admin_pending"}},
		},
	}
}

// Helper functions

// IsAdmin checks if user is admin
func IsAdmin(userID int64) bool {
	userIDStr := fmt.Sprintf("%d", userID)
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()
	_, exists := admins[userIDStr]
	return exists
}

// GetAdminCount returns the number of admins
func GetAdminCount() int {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()
	return len(admins)
}

// getPendingCount returns the number of pending requests
func getPendingCount() int {
	if jellyseerrClient == nil {
		return 0
	}
	requests, err := jellyseerrClient.GetPendingRequests()
	if err != nil {
		return 0
	}
	return len(requests)
}

// SendAdminUpdateNotification sends notification to admins about panel updates
func SendAdminUpdateNotification(message string) {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	for userIDStr := range admins {
		userID, _ := strconv.ParseInt(userIDStr, 10, 64)
		sendPrivateMessage(userID, message, nil)
	}
}
