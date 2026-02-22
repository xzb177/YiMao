package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/handlers"
	"emby-telegram-bot/internal/middleware"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
	"emby-telegram-bot/pkg/types"
)

func main() {
	log.Println("🚀 Starting Emby Telegram Bot (Enterprise Edition)...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("✅ Configuration loaded")
	log.Printf("   MoviePilot: %s", cfg.MoviePilotURL)
	log.Printf("   Data directory: %s", cfg.DataDir)

	// Parse chat ID
	chatID, _ := strconv.ParseInt(cfg.TelegramChatID, 10, 64)

	// Initialize services
	telegramClient := services.NewTelegramClient(cfg.TelegramBotToken)
	moviepilotClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey)
	sessMgr := session.NewManager(time.Duration(cfg.MaxSessionAge)*time.Hour, cfg.MaxSessions)
	userMappingService := services.NewUserMappingService(cfg.DataDir)
	bindingRequestService := services.NewBindingRequestService(cfg.DataDir)
	preferencesService := services.NewPreferencesService(cfg.DataDir)
	issueService := services.NewIssueService(cfg.DataDir)
	adminService := services.NewAdminService(cfg.DataDir)
	quotaService := services.NewQuotaService(cfg.DataDir, moviepilotClient)
	reviewService := services.NewReviewService(cfg.DataDir)

	// Initialize Media Notification Service
	mediaNotificationSvc := services.NewMediaNotificationService(cfg.DataDir, telegramClient, adminService)
	log.Println("✅ Media notification service initialized")

	// Initialize AI Chat Service
	chatService := services.NewChatService(cfg.ZhipuAPIKey)
	// Get admin IDs from admin service
	adminsMap := adminService.GetAllAdmins()
	var adminIDs []int64
	for idStr := range adminsMap {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			adminIDs = append(adminIDs, id)
		}
	}
	chatService.SetAdminIDs(adminIDs)
	log.Printf("[ChatService] AI Chat initialized: enabled=%v", chatService.IsAIEnabled())

	// Set admin IDs for quota service (admins have unlimited quota)
	quotaService.SetAdminIDs(adminIDs)

	webhookService := services.NewWebhookService(
		telegramClient,
		moviepilotClient,
		userMappingService,
		adminService,
		preferencesService,
		chatID,
		cfg.EmbyURL,
		cfg.EmbyAPIKey,
		mediaNotificationSvc,
	)

	// Initialize TMDB client (with default read-access key if not provided)
	tmdbClient := services.NewTMDBClientWithDefaultKey(cfg.TMDBAPIKey)

	// Start cleanup routines
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			bindingRequestService.CleanupExpiredRequests()
		}
	}()

	log.Println("✅ Services initialized")

	// Initialize callback registry
	registry := callback.NewRegistry()

	// Apply middleware
	registry.Use(middleware.Recovery)
	registry.Use(middleware.Logger)
	registry.Use(middleware.Validator)

	// Register handlers
	startHandler := handlers.NewStartHandler(cfg, sessMgr, telegramClient, moviepilotClient)
	detailHandler := handlers.NewDetailHandler(sessMgr, telegramClient, moviepilotClient, tmdbClient)
	backHandler := handlers.NewBackHandler(sessMgr)
	cancelHandler := handlers.NewCancelHandler()
	requestHandler := handlers.NewRequestHandler(sessMgr, telegramClient, moviepilotClient, adminService, webhookService, userMappingService, quotaService, reviewService)
	searchHandler := handlers.NewSearchHandler(sessMgr, telegramClient, moviepilotClient, tmdbClient)
	myRequestsHandler := handlers.NewMyRequestsHandler(sessMgr, telegramClient, moviepilotClient)
	linkHandler := handlers.NewLinkHandler(cfg, sessMgr, telegramClient, moviepilotClient, userMappingService, bindingRequestService)
	helpHandler := handlers.NewHelpHandler()
	aiHandler := handlers.NewAIHandler(cfg, sessMgr, telegramClient, moviepilotClient)
	adminHandler := handlers.NewAdminHandler(cfg, sessMgr, telegramClient, moviepilotClient, adminService, quotaService)
	reviewHandler := handlers.NewReviewHandler(sessMgr, telegramClient, moviepilotClient, adminService, reviewService)

	// Inject dependencies
	startHandler.SetAdminService(adminService)
	backHandler.SetAdminService(adminService)
	adminHandler.SetMediaNotificationService(mediaNotificationSvc)

	registry.RegisterFunc(callback.ActionStart, startHandler.Handle)
	registry.RegisterFunc(callback.ActionSearch, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionAI, aiHandler.Handle)
	registry.RegisterFunc(callback.ActionTrending, aiHandler.HandleTrending)
	registry.RegisterFunc(callback.ActionHot, aiHandler.HandleHot)
	registry.RegisterFunc(callback.ActionNew, aiHandler.HandleNew)
	registry.RegisterFunc(callback.ActionDetail, detailHandler.Handle)
	registry.RegisterFunc(callback.ActionRequest, requestHandler.Handle)
	registry.RegisterFunc(callback.ActionPage, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionSelect, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionBack, backHandler.Handle)
	registry.RegisterFunc(callback.ActionCancel, cancelHandler.Handle)
	registry.RegisterFunc(callback.ActionRequests, myRequestsHandler.Handle)
	registry.RegisterFunc(callback.ActionLink, linkHandler.Handle)
	registry.RegisterFunc(callback.ActionHelp, helpHandler.Handle)
	registry.RegisterFunc("admin_approve", adminHandler.Handle)
	registry.RegisterFunc("admin_decline", adminHandler.Handle)
	registry.RegisterFunc("admin_pending", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_reply", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_fixed", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_processing", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_close", adminHandler.Handle)
	registry.RegisterFunc("admin_menu", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_settings", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_mode_instant", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_mode_daily", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_toggle", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_settime", adminHandler.Handle)

	// Review system callbacks
	registry.RegisterFunc("review_approve", reviewHandler.Handle)
	registry.RegisterFunc("review_reject", reviewHandler.Handle)
	registry.RegisterFunc("review_cancel", reviewHandler.Handle)
	registry.RegisterFunc("my_reviews", reviewHandler.Handle)
	registry.RegisterFunc("review_list", reviewHandler.Handle)

	log.Println("✅ Callback handlers registered")

	// Setup webhook (if configured)
	if cfg.WebhookURL != "" {
		if err := telegramClient.SetWebhook(cfg.WebhookURL); err != nil {
			log.Printf("⚠️  Failed to set webhook: %v", err)
		} else {
			log.Printf("✅ Webhook set: %s", cfg.WebhookURL)
		}
	} else {
		// No webhook configured, delete any existing webhook to enable polling
		log.Println("⚠️  No webhook URL configured, deleting webhook to enable polling")
		telegramClient.DeleteWebhook()
	}

	// Start polling for updates if no webhook is configured
	go pollForUpdates(telegramClient, sessMgr, cfg, registry, moviepilotClient, linkHandler, chatService, adminService)

	// Create HTTP server
	server := createServer(cfg, registry, telegramClient, sessMgr, adminService, quotaService, userMappingService, preferencesService, issueService, webhookService, moviepilotClient, chatService, linkHandler)

	// Start server in background
	go func() {
		log.Printf("🌐 Server listening on %s:%s", cfg.ServerHost, cfg.ServerPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("❌ Server shutdown error: %v", err)
	}

	log.Println("✅ Server stopped")
}

// pollForUpdates polls Telegram for updates using getUpdates
func pollForUpdates(
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	cfg *config.Config,
	registry *callback.Registry,
	moviepilot *services.MoviePilotClient,
	linkHandler *handlers.LinkHandler,
	chatService *services.ChatService,
	adminService *services.AdminService,
) {
	if cfg.WebhookURL != "" {
		return // Don't poll if webhook is configured
	}

	log.Println("🔄 Starting Telegram updates polling...")

	offset := 0
	pollInterval := 1 * time.Second

	for {
		time.Sleep(pollInterval)

		updates, err := telegram.GetUpdates(offset, 100)
		if err != nil {
			log.Printf("[Poll] Failed to get updates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(updates) == 0 {
			continue
		}

		log.Printf("[Poll] Received %d updates", len(updates))

		for _, update := range updates {
			// Update offset
			if update.UpdateID > 0 {
				offset = int(update.UpdateID + 1)
			}

			// Process update
			if update.Message != nil {
				handlePollMessage(update.Message, telegram, sessMgr, cfg, moviepilot, linkHandler, chatService, adminService)
			} else if update.CallbackQuery != nil {
				handleCallbackQuery(update.CallbackQuery, registry, telegram)
			}
		}
	}
}

// handlePollMessage processes a message update (for polling)
func handlePollMessage(msg *types.TelegramMessage,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	cfg *config.Config,
	moviepilot *services.MoviePilotClient,
	linkHandler *handlers.LinkHandler,
	chatService *services.ChatService,
	adminService *services.AdminService,
) {
	log.Printf("[Poll] Message from %d: %s", msg.From.ID, msg.Text)

	// Handle commands
	if strings.HasPrefix(msg.Text, "/") {
		parts := strings.Fields(msg.Text)
		if len(parts) > 0 {
			switch parts[0] {
			case "/start":
				// Check if user is admin
				isAdmin := adminService != nil && adminService.IsAdmin(msg.From.ID)
				telegram.SendMessage(msg.Chat.ID, buildStartMenu(), "Markdown", services.BuildStartKeyboard(isAdmin))
			case "/help":
				telegram.SendMessage(msg.Chat.ID, buildHelpMessage(), "Markdown", nil)
			case "/link":
				if len(parts) >= 3 {
					username := parts[1]
					password := strings.Join(parts[2:], " ")
					if err := linkHandler.HandleWithCredentials(msg.From.ID, username, password); err != nil {
						telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 绑定失败: %v", err), "Markdown", nil)
					} else {
						telegram.SendMessage(msg.Chat.ID, "✅ 账号绑定成功！", "Markdown", nil)
					}
				} else {
					telegram.SendMessage(msg.Chat.ID, "🔗 绑定账号\n\n格式: /link 用户名 密码", "Markdown", nil)
				}
			default:
				telegram.SendMessage(msg.Chat.ID, "❓ 未知命令，使用 /help 查看帮助", "Markdown", nil)
			}
		}
		return
	}

	// Group chat: Only AI chat is allowed
	if msg.Chat.Type != "private" {
		query := msg.Text
		isReplyToBot := msg.ReplyToMessage != nil && msg.ReplyToMessage.From.IsBot
		isMention := strings.Contains(strings.ToLower(query), "@oceancloudying_bot") ||
			strings.Contains(strings.ToLower(query), "@云海看板娘")

		chatType := services.ChatTypeGroup
		if msg.Chat.Type == "supergroup" {
			chatType = services.ChatTypeSupergroup
		}

		userName := msg.From.FirstName
		if msg.From.Username != "" {
			userName = msg.From.Username
		}

		chatMsg := &services.ChatMessage{
			UserID:    msg.From.ID,
			UserName:  userName,
			Content:   query,
			IsReply:   isReplyToBot,
			IsMention: isMention,
			ChatType:  chatType,
			Timestamp: time.Now(),
		}

		log.Printf("[PollGroupChat] Message from %s: isMention=%v, isReply=%v",
			userName, isMention, isReplyToBot)

		// Only respond to mentions or replies
		if chatService.ShouldReply(chatMsg) {
			log.Printf("[PollGroupChat] ShouldReply=true, getting response...")
			response := chatService.GetResponse(chatMsg)
			log.Printf("[PollGroupChat] Got response: ShouldReply=%v, Text=%s",
				response.ShouldReply, response.Text)
			if response.ShouldReply && response.Text != "" {
				telegram.SendMessage(msg.Chat.ID, response.Text, "", nil)
			}
		}
		return
	}

	// Private chat: Handle search query
	if msg.Text != "" && len(msg.Text) > 1 {
		handlePollSearchQuery(msg, telegram, moviepilot, sessMgr)
	}
}

// handlePollSearchQuery handles search queries (for polling)
func handlePollSearchQuery(msg *types.TelegramMessage, telegram *services.TelegramClient, moviepilot *services.MoviePilotClient, sessMgr *session.Manager) {
	query := msg.Text

	// Search in MoviePilot
	results, err := moviepilot.SearchMedia(query, 1)
	if err != nil {
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 搜索失败: %v", err), "Markdown", nil)
		return
	}

	if len(results.Results) == 0 {
		telegram.SendMessage(msg.Chat.ID, "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索", "Markdown", nil)
		return
	}

	// Store search results in session
	sess := sessMgr.GetOrCreate(msg.From.ID)
	searchItems := make([]session.SearchItem, len(results.Results))
	for i, item := range results.Results {
		mediaType := "movie"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "tv"
		}
		searchItems[i] = session.SearchItem{
			ID:       fmt.Sprintf("%d", item.ID),
			Title:    item.Title,
			Year:     item.Year.Int(),
			Type:     mediaType,
			Poster:   item.Poster,
			Rating:   item.Rating,
			Overview: item.Overview,
		}
	}
	sess.SetSearchResults(searchItems, 1, query)
	log.Printf("[PollSearch] Stored %d search results in session for user %d", len(searchItems), msg.From.ID)

	// Build search results message
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 { // Limit to 8 results per page
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		// Rating
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		// Add item info to message text
		mediaType := "🎬 电影"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)

		// Button with number only
		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, item.Type),
		})

		// Add row every 4 buttons (4 columns for mobile)
		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	// Add remaining items
	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	// Add navigation buttons
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回", CallbackData: "start"},
	}
	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}
	keyboardRows = append(keyboardRows, navRow)

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}

	telegram.SendMessage(msg.Chat.ID, text, "", keyboard)
}

// handleCallbackQuery processes a callback query (for polling)
func handleCallbackQuery(cb *types.TelegramCallbackQuery, registry *callback.Registry, telegram *services.TelegramClient) {
	log.Printf("[Poll] Callback from user %d: %s", cb.From.ID, cb.Data)

	// Parse callback
	parsed, err := registry.Parser().Parse(cb.Data)
	if err != nil {
		log.Printf("Failed to parse callback: %v", err)
		telegram.AnswerCallback(cb.ID, "无效的请求", true)
		return
	}

	// Build context
	ctx := &callback.Context{
		UserID:     cb.From.ID,
		ChatID:      cb.Message.Chat.ID,
		MessageID:   cb.Message.MessageID,
		CallbackID:  cb.ID,
		Callback:    parsed,
	}

	// Get handler
	handler, exists := registry.Get(parsed.Action)
	if !exists {
		log.Printf("No handler for action: %s", parsed.Action)
		telegram.AnswerCallback(cb.ID, "未知操作", true)
		return
	}

	// Handle callback
	resp, err := handler.Handle(ctx)

	// Answer callback query
	callbackMsg := ""
	showAlert := false
	if resp != nil {
		// Use Text as alert message if ShowAlert is true and Text is not empty
		// Otherwise use CallbackMsg (short confirmation)
		if resp.ShowAlert && resp.Text != "" {
			callbackMsg = resp.Text
		} else {
			callbackMsg = resp.CallbackMsg
		}
		showAlert = resp.ShowAlert
	}

	if err != nil {
		log.Printf("Handler error: %v", err)
		if callbackMsg == "" {
			callbackMsg = "操作失败"
		}
		showAlert = true
	}

	// Truncate callback message if too long (Telegram limit is 200 chars for alerts)
	if showAlert && len(callbackMsg) > 200 {
		callbackMsg = callbackMsg[:197] + "..."
	}

	if err := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); err != nil {
		log.Printf("[Callback] AnswerCallback error: %v", err)
	}

	// Edit message if needed
	if resp != nil && resp.Edit && resp.Text != "" {
		keyboard := convertToTelegramKeyboard(resp.Keyboard)
		telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, "Markdown", keyboard)
	}
}

func buildStartMenu() string {
	return `*🌟 欢迎使用 Emby Telegram Bot*

请选择操作：`
}

func buildHelpMessage() string {
	return `*📖 帮助*

/search <影片名> - 搜索影片
/link 用户名 密码 - 绑定账号
/start - 显示主菜单`
}

// handleSearchQuery handles search queries (for polling)
func handleSearchQuery(msg *types.TelegramMessage, telegram *services.TelegramClient, sessMgr *session.Manager, cfg *config.Config, moviepilot *services.MoviePilotClient) {
	query := msg.Text

	// Search in MoviePilot
	results, err := moviepilot.SearchMedia(query, 1)
	if err != nil {
		telegram.SendMessage(msg.Chat.ID, fmt.Sprintf("❌ 搜索失败: %v", err), "Markdown", nil)
		return
	}

	if len(results.Results) == 0 {
		telegram.SendMessage(msg.Chat.ID, "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索", "Markdown", nil)
		return
	}

	// Build search results message
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 { // Limit to 8 results per page
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		// Rating
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		// Add item info to message text
		mediaType := "🎬 电影"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)

		// Button with number only
		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, item.Type),
		})

		// Add row every 4 buttons (4 columns for mobile)
		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	// Add remaining items
	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	// Add navigation buttons
	navRow := []types.TelegramInlineKeyboardButton{
		{Text: "⬅️ 返回", CallbackData: "start"},
	}
	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}
	keyboardRows = append(keyboardRows, navRow)

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: keyboardRows,
	}

	telegram.SendMessage(msg.Chat.ID, text, "", keyboard)
}

// createServer creates the HTTP server
func createServer(
	cfg *config.Config,
	registry *callback.Registry,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	adminService *services.AdminService,
	quotaService *services.QuotaService,
	userMapping *services.UserMappingService,
	preferences *services.PreferencesService,
	issueService *services.IssueService,
	webhookService *services.WebhookService,
	moviepilot *services.MoviePilotClient,
	chatService *services.ChatService,
	linkHandler *handlers.LinkHandler,
) *http.Server {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	// Debug endpoint
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		stats := sessMgr.Stats()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sessions": %d, "total_size": %d}`,
			stats["total_sessions"], stats["total_size"])
	})

	// Webhook endpoint (for Telegram bot updates)
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		handleWebhook(w, r, registry, telegram, sessMgr, cfg, moviepilot, userMapping, quotaService, chatService, linkHandler, adminService)
	})

	// Telegram webhook endpoint (for compatibility)
	mux.HandleFunc("/telegram-webhook", func(w http.ResponseWriter, r *http.Request) {
		handleWebhook(w, r, registry, telegram, sessMgr, cfg, moviepilot, userMapping, quotaService, chatService, linkHandler, adminService)
	})

	return &http.Server{
		Addr:         cfg.ServerHost + ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
}

// handleWebhook handles incoming Telegram webhook
func handleWebhook(
	w http.ResponseWriter,
	r *http.Request,
	registry *callback.Registry,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	cfg *config.Config,
	moviepilot *services.MoviePilotClient,
	userMapping *services.UserMappingService,
	quotaService *services.QuotaService,
	chatService *services.ChatService,
	linkHandler *handlers.LinkHandler,
	adminService *services.AdminService,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse update
	var update types.TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Failed to decode update: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Route update
	if update.CallbackQuery != nil {
		handleCallback(w, &update, registry, telegram, sessMgr, cfg, adminService)
	} else if update.Message != nil {
		handleMessage(w, &update, telegram, sessMgr, cfg, registry, moviepilot, userMapping, quotaService, chatService, linkHandler, adminService)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}
}

// handleCallback handles callback queries
func handleCallback(
	w http.ResponseWriter,
	update *types.TelegramUpdate,
	registry *callback.Registry,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	cfg *config.Config,
	adminService *services.AdminService,
) {
	cb := update.CallbackQuery
	log.Printf("[Webhook] Callback from user %d: %s", cb.From.ID, cb.Data)

	// Parse callback
	parsed, err := registry.Parser().Parse(cb.Data)
	if err != nil {
		log.Printf("Failed to parse callback: %v", err)
		telegram.AnswerCallback(cb.ID, "无效的请求", true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Build context
	ctx := &callback.Context{
		UserID:     cb.From.ID,
		ChatID:     cb.Message.Chat.ID,
		MessageID:  cb.Message.MessageID,
		CallbackID: cb.ID,
		Callback:   parsed,
	}

	// Get handler
	handler, exists := registry.Get(parsed.Action)
	if !exists {
		log.Printf("No handler for action: %s", parsed.Action)
		telegram.AnswerCallback(cb.ID, "未知操作", true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Handle callback
	resp, err := handler.Handle(ctx)

	// Answer callback query
	callbackMsg := ""
	showAlert := false
	if resp != nil {
		// Use Text as alert message if ShowAlert is true and Text is not empty
		// Otherwise use CallbackMsg (short confirmation)
		if resp.ShowAlert && resp.Text != "" {
			callbackMsg = resp.Text
		} else {
			callbackMsg = resp.CallbackMsg
		}
		showAlert = resp.ShowAlert
	}

	if err != nil {
		log.Printf("Handler error: %v", err)
		if callbackMsg == "" {
			callbackMsg = "操作失败"
		}
		showAlert = true
	}

	// Truncate callback message if too long (Telegram limit is 200 chars for alerts)
	if showAlert && len(callbackMsg) > 200 {
		callbackMsg = callbackMsg[:197] + "..."
	}

	if err := telegram.AnswerCallback(cb.ID, callbackMsg, showAlert); err != nil {
		log.Printf("[Callback] AnswerCallback error: %v", err)
	}

	// Edit message if needed
	if resp != nil && resp.Edit && resp.Text != "" {
		keyboard := convertToTelegramKeyboard(resp.Keyboard)
		telegram.EditMessage(ctx.ChatID, ctx.MessageID, resp.Text, "Markdown", keyboard)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// handleMessage handles incoming messages
func handleMessage(
	w http.ResponseWriter,
	update *types.TelegramUpdate,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
	cfg *config.Config,
	registry *callback.Registry,
	moviepilot *services.MoviePilotClient,
	userMapping *services.UserMappingService,
	quotaService *services.QuotaService,
	chatService *services.ChatService,
	linkHandler *handlers.LinkHandler,
	adminService *services.AdminService,
) {
	msg := update.Message
	log.Printf("[Webhook] Message from user %d (chat: %s, type: %s): %s", msg.From.ID, msg.Chat.ID, msg.Chat.Type, msg.Text)

	// 群聊中只处理 AI 聊天（@机器人 或 回复机器人），其他所有功能都忽略
	if msg.Chat.Type != "private" {
		// 群聊中，只处理文本消息用于 AI 聊天
		if len(msg.Text) > 1 {
			handleTextQuery(telegram, msg, sessMgr, cfg, registry, moviepilot, userMapping, quotaService, chatService)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// 私聊中处理所有功能

	// Handle commands
	if strings.HasPrefix(msg.Text, "/") {
		handleCommand(telegram, msg, cfg, registry, linkHandler)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Handle search queries (non-command text)
	if len(msg.Text) > 1 {
		handleTextQuery(telegram, msg, sessMgr, cfg, registry, moviepilot, userMapping, quotaService, chatService)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

func handleCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, registry *callback.Registry, linkHandler *handlers.LinkHandler) {
	// Extract command and arguments
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	command := parts[0]

	switch command {
	case "/start":
		sendStartMenu(telegram, msg.Chat.ID)
	case "/search":
		text := "🔍 请输入影片名称进行搜索"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/ai":
		text := "🤖 请输入推荐关键词，如：推荐、热门、新片等"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/trending":
		text := "🔥 请使用 /start 菜单中的 AI 推荐功能"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/requests":
		text := "📋 请使用 /start 菜单中的 我的请求 功能"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/link":
		handleLinkCommand(telegram, msg, linkHandler)
	case "/quota":
		text := "📊 配额功能开发中...\n\n💡 请使用 /start 菜单查看请求状态"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/help":
		sendHelpMessage(telegram, msg.Chat.ID)
	default:
		text := "❓ 未知命令，请使用 /help 查看帮助"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	}
}

// handleLinkCommand handles /link command with optional username and password
func handleLinkCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, linkHandler *handlers.LinkHandler) {
	parts := strings.Fields(msg.Text)

	if len(parts) == 1 {
		// No arguments provided, show instructions
		text := "🔗 绑定 Jellyseerr 账号\n\n请使用以下命令绑定您的账号：\n\n/link 用户名 密码\n\n示例：\n/link johndoe mypassword123\n\n💡 您的凭据将直接发送到 Jellyseerr 服务器进行验证"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	if len(parts) < 3 {
		text := "❌ 参数不足\n\n格式: /link 用户名 密码\n\n示例: /link johndoe mypassword123"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	username := parts[1]
	password := strings.Join(parts[2:], " ") // Join remaining parts in case password has spaces

	// Attempt to link account
	if err := linkHandler.HandleWithCredentials(msg.From.ID, username, password); err != nil {
		log.Printf("[LinkCommand] Failed to link account: %v", err)
		text := fmt.Sprintf("❌ 绑定失败: %v", err)
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
		return
	}

	text := "✅ 账号绑定成功！\n\n您现在可以使用以下功能：\n• 🔍 搜索影片\n• 📋 请求影片\n• 🎬 查看请求状态"
	_, _ = telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
}

func handleTextQuery(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	sessMgr *session.Manager,
	cfg *config.Config,
	registry *callback.Registry,
	moviepilot *services.MoviePilotClient,
	userMapping *services.UserMappingService,
	quotaService *services.QuotaService,
	chatService *services.ChatService,
) {
	query := msg.Text

	// Check for reply_to_bot or mention
	isReplyToBot := msg.ReplyToMessage != nil && msg.ReplyToMessage.From.IsBot
	isMention := strings.Contains(strings.ToLower(query), "@oceancloudying_bot") ||
		strings.Contains(strings.ToLower(query), "@云海看板娘")

	// Determine chat type (private vs group)
	chatType := services.ChatTypeGroup // 默认群组
	if msg.Chat.Type == "private" {
		chatType = services.ChatTypePrivate
	}

	// Check if it's a chat-worthy message
	userName := msg.From.FirstName
	if msg.From.Username != "" {
		userName = msg.From.Username
	}

	chatMsg := &services.ChatMessage{
		UserID:    msg.From.ID,
		UserName:  userName,
		Content:   query,
		IsReply:   isReplyToBot,
		IsMention: isMention,
		ChatType:  chatType,
		Timestamp: time.Now(),
	}

	// Group chat: Only AI chat is allowed
	if chatType == services.ChatTypeGroup || chatType == services.ChatTypeSupergroup {
		log.Printf("[GroupChat] Message from %s: isMention=%v, isReply=%v, content=%s",
			userName, isMention, isReplyToBot, query)
		// Only respond to mentions or replies
		if chatService.ShouldReply(chatMsg) {
			log.Printf("[GroupChat] ShouldReply=true, getting response...")
			response := chatService.GetResponse(chatMsg)
			log.Printf("[GroupChat] Got response: ShouldReply=%v, Text=%s", response.ShouldReply, response.Text)
			if response.ShouldReply && response.Text != "" {
				_, err := telegram.SendMessage(msg.Chat.ID, response.Text, "", nil)
				log.Printf("[GroupChat] Sent message: err=%v", err)
			}
		} else {
			log.Printf("[GroupChat] ShouldReply=false, skipping")
		}
		// Group chat: no other features (search, AI query, etc.)
		return
	}

	// Private chat: AI chat check (currently disabled for private)
	if chatService.ShouldReply(chatMsg) {
		response := chatService.GetResponse(chatMsg)
		if response.ShouldReply && response.Text != "" {
			_, _ = telegram.SendMessage(msg.Chat.ID, response.Text, "", nil)
		}
		return
	}

	// Check if it's an AI recommendation query
	if isAIQuery(query) {
		handleAIQuery(telegram, msg, sessMgr, cfg)
		return
	}

	// Otherwise, treat as search query - trigger search via handler
	go performSearch(telegram, msg, sessMgr, moviepilot)
}

// performSearch performs the actual search in background
func performSearch(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	sessMgr *session.Manager,
	moviepilot *services.MoviePilotClient,
) {
	query := msg.Text

	// URL encode the query
	encodedQuery := strings.ReplaceAll(query, " ", "+")

	// Search in MoviePilot
	results, err := moviepilot.SearchMedia(encodedQuery, 1)
	if err != nil {
		log.Printf("[Search] Failed to search: %v", err)
		text := fmt.Sprintf("❌ 搜索失败\n\n错误: %v", err)
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	// Format results
	if len(results.Results) == 0 {
		text := "😕 未找到相关内容\n\n💡 建议：\n• 检查拼写是否正确\n• 尝试使用更简短的关键词\n• 尝试使用英文搜索"
		_, _ = telegram.SendMessage(msg.Chat.ID, text, "", nil)
		return
	}

	// Build search results message
	text := fmt.Sprintf("🔍 搜索结果「%s」\n\n找到 %d 条结果\n\n",
		query, len(results.Results))

	// Build keyboard with results
	var keyboardRows [][]types.TelegramInlineKeyboardButton
	var row []types.TelegramInlineKeyboardButton

	for i, item := range results.Results {
		if i >= 8 { // Limit to 8 results per page
			break
		}

		year := ""
		if item.Year > 0 {
			year = fmt.Sprintf("%d", item.Year)
		}

		// Rating
		rating := ""
		if item.Rating > 0 {
			rating = fmt.Sprintf(" ⭐%.1f", item.Rating)
		}

		// Add item info to message text
		mediaType := "🎬 电影"
		if item.Type == "tv" || item.Type == "电视剧" {
			mediaType = "📺 剧集"
		}
		text += fmt.Sprintf("%d. %s (%s) %s%s\n", i+1, item.Title, year, mediaType, rating)

		// Button with number only
		row = append(row, types.TelegramInlineKeyboardButton{
			Text:         fmt.Sprintf("%d", i+1),
			CallbackData: fmt.Sprintf("select:id:%d:type:%s", item.ID, item.Type),
		})

		// Add row every 4 buttons (4 columns for mobile)
		if len(row) == 4 {
			keyboardRows = append(keyboardRows, row)
			row = []types.TelegramInlineKeyboardButton{}
		}
	}

	// Add remaining items
	if len(row) > 0 {
		keyboardRows = append(keyboardRows, row)
	}

	// Save search results to session for later use in detail view
	sess := sessMgr.GetOrCreate(msg.Chat.ID)
	searchItems := make([]session.SearchItem, 0)
	for i, item := range results.Results {
		if i >= 8 {
			break
		}
		searchItems = append(searchItems, session.SearchItem{
			ID:     fmt.Sprintf("%d", item.ID),
			Title:  item.Title,
			Year:   item.Year.Int(),
			Type:   string(item.Type),
			Rating: item.Rating,
		})
	}
	sess.SetSearchResults(searchItems, 1, query)

	// Add navigation row
	navRow := []types.TelegramInlineKeyboardButton{}
	navRow = append(navRow, types.TelegramInlineKeyboardButton{
		Text:         "⬅️ 返回",
		CallbackData: "start",
	})

	// Check if there are more results
	if len(results.Results) >= 20 {
		navRow = append(navRow, types.TelegramInlineKeyboardButton{
			Text:         "➡️ 下一页",
			CallbackData: fmt.Sprintf("search:query:%s:page:2", query),
		})
	}

	keyboardRows = append(keyboardRows, navRow)

	// Send message
	keyboard := &types.TelegramInlineKeyboard{InlineKeyboard: keyboardRows}
	_, _ = telegram.SendMessage(msg.Chat.ID, text, "", keyboard)
}

// getTitle gets the title from MediaInfo
func getTitle(item *services.MediaInfo) string {
	if item.Title != "" {
		return item.Title
	}
	return "未知"
}

func isAIQuery(query string) bool {
	aiKeywords := []string{"推荐", "有什么", "好看的", "想看", "来点", "给我", "热门", "trending"}
	for _, keyword := range aiKeywords {
		if len(query) >= len(keyword) && (query == keyword || len(query) > len(keyword)) {
			// Simple contains check
			for i := 0; i <= len(query)-len(keyword); i++ {
				if query[i:i+len(keyword)] == keyword {
					return true
				}
			}
		}
	}
	return false
}

func handleAIQuery(
	telegram *services.TelegramClient,
	msg *types.TelegramMessage,
	sessMgr *session.Manager,
	cfg *config.Config,
) {
	// Send AI recommendation menu
	sendAIMenu(telegram, msg.Chat.ID)
}

// sendAIMenu sends AI recommendation menu
func sendAIMenu(telegram *services.TelegramClient, chatID int64) {
	text := "🤖 AI 智能推荐\n\n请选择推荐类型："

	keyboard := &types.TelegramInlineKeyboard{
		InlineKeyboard: [][]types.TelegramInlineKeyboardButton{
			{
				{Text: "🔥 热门推荐", CallbackData: "ai:trending"},
				{Text: "📺 热播剧集", CallbackData: "ai:hot_tv"},
			},
			{
				{Text: "🎬 最新电影", CallbackData: "ai:new_movies"},
				{Text: "🎲 随机推荐", CallbackData: "ai:random"},
			},
			{
				{Text: "⬅️ 返回主菜单", CallbackData: "start"},
			},
		},
	}

	_, _ = telegram.SendMessage(chatID, text, "", keyboard)
}

func sendStartMenu(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("🌟 欢迎使用 Emby Telegram Bot").Newline()
	msg.Newline()
	msg.Text("请选择操作：")

	keyboard := services.BuildStartKeyboard(false) // No admin check in polling mode
	_, _ = telegram.SendMessage(chatID, msg.Build(), "Markdown", keyboard)
}

func sendHelpMessage(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("❓ 帮助中心").Newline()
	msg.Newline()

	msg.Bold("🔍 搜索影片").Newline()
	msg.Text("  直接输入影片名称即可搜索").Newline()
	msg.Newline()

	msg.Bold("🤖 AI 推荐").Newline()
	msg.Text("  说「推荐」获取智能推荐").Newline()
	msg.Newline()

	msg.Bold("⌨️ 命令列表").Newline()
	msg.Text("  /start - 开始使用").Newline()
	msg.Text("  /search - 搜索影片").Newline()
	msg.Text("  /ai - AI 推荐").Newline()
	msg.Text("  /link - 绑定账号").Newline()
	msg.Text("  /help - 帮助信息").Newline()

	keyboard := services.BuildStartKeyboard(false) // No admin check in polling mode
	_, _ = telegram.SendMessage(chatID, msg.Build(), "Markdown", keyboard)
}

func convertToTelegramKeyboard(kb *callback.Keyboard) *types.TelegramInlineKeyboard {
	if kb == nil {
		return nil
	}

	result := &types.TelegramInlineKeyboard{
		InlineKeyboard: make([][]types.TelegramInlineKeyboardButton, len(kb.InlineKeyboard)),
	}

	for i, row := range kb.InlineKeyboard {
		result.InlineKeyboard[i] = make([]types.TelegramInlineKeyboardButton, len(row))
		for j, btn := range row {
			result.InlineKeyboard[i][j] = types.TelegramInlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
				URL:          btn.URL,
			}
		}
	}

	return result
}
