package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	log.Printf("   Jellyseerr: %s", cfg.JellyseerrURL)
	log.Printf("   Data directory: %s", cfg.DataDir)

	// Initialize services
	telegramClient := services.NewTelegramClient(cfg.TelegramBotToken)
	jellyseerrClient := services.NewJellyseerrClient(cfg.JellyseerrURL, cfg.JellyseerrAPIKey)
	sessMgr := session.NewManager(time.Duration(cfg.MaxSessionAge)*time.Hour, cfg.MaxSessions)

	log.Println("✅ Services initialized")

	// Initialize callback registry
	registry := callback.NewRegistry()

	// Apply middleware
	registry.Use(middleware.Recovery)
	registry.Use(middleware.Logger)
	registry.Use(middleware.Validator)

	// Register handlers
	startHandler := handlers.NewStartHandler(cfg, sessMgr, telegramClient, jellyseerrClient)
	detailHandler := handlers.NewDetailHandler(sessMgr, telegramClient, jellyseerrClient)
	backHandler := handlers.NewBackHandler(sessMgr)
	cancelHandler := handlers.NewCancelHandler()
	requestHandler := handlers.NewRequestHandler(sessMgr, telegramClient, jellyseerrClient)
	searchHandler := handlers.NewSearchHandler(sessMgr, telegramClient, jellyseerrClient)
	myRequestsHandler := handlers.NewMyRequestsHandler(sessMgr, telegramClient, jellyseerrClient)
	linkHandler := handlers.NewLinkHandler(cfg, sessMgr, telegramClient)
	helpHandler := handlers.NewHelpHandler()
	aiHandler := handlers.NewAIHandler(cfg, sessMgr, telegramClient, jellyseerrClient)

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

	log.Println("✅ Callback handlers registered")

	// Setup webhook (if configured)
	if cfg.WebhookURL != "" {
		if err := telegramClient.SetWebhook(cfg.WebhookURL); err != nil {
			log.Printf("⚠️  Failed to set webhook: %v", err)
		} else {
			log.Printf("✅ Webhook set: %s", cfg.WebhookURL)
		}
	}

	// Create HTTP server
	server := createServer(cfg, registry, telegramClient, sessMgr)

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

// createServer creates the HTTP server
func createServer(
	cfg *config.Config,
	registry *callback.Registry,
	telegram *services.TelegramClient,
	sessMgr *session.Manager,
) *http.Server {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	// Webhook endpoint
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		handleWebhook(w, r, registry, telegram, sessMgr, cfg)
	})

	// Telegram webhook endpoint (for compatibility with existing setup)
	mux.HandleFunc("/telegram-webhook", func(w http.ResponseWriter, r *http.Request) {
		handleWebhook(w, r, registry, telegram, sessMgr, cfg)
	})

	// Debug endpoint (remove in production)
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		stats := sessMgr.Stats()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sessions": %d, "total_size": %d}`,
			stats["total_sessions"], stats["total_size"])
	})

	return &http.Server{
		Addr:         cfg.ServerHost + ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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
		handleCallback(w, &update, registry, telegram, sessMgr, cfg)
	} else if update.Message != nil {
		handleMessage(w, &update, telegram, sessMgr, cfg, registry)
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
		callbackMsg = resp.CallbackMsg
		showAlert = resp.ShowAlert
	}

	if err != nil {
		log.Printf("Handler error: %v", err)
		if callbackMsg == "" {
			callbackMsg = "操作失败"
		}
		showAlert = true
	}

	telegram.AnswerCallback(cb.ID, callbackMsg, showAlert)

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
) {
	msg := update.Message
	log.Printf("[Webhook] Message from user %d: %s", msg.From.ID, msg.Text)

	// Handle commands
	if strings.HasPrefix(msg.Text, "/") {
		handleCommand(telegram, msg, cfg, registry)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
		return
	}

	// Handle search queries (non-command text)
	if len(msg.Text) > 1 {
		handleTextQuery(telegram, msg, sessMgr, cfg, registry)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

func handleCommand(telegram *services.TelegramClient, msg *types.TelegramMessage, cfg *config.Config, registry *callback.Registry) {
	switch msg.Text {
	case "/start":
		sendStartMenu(telegram, msg.Chat.ID)
	case "/search":
		text := "🔍 请输入影片名称进行搜索"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/ai":
		text := "🤖 请输入推荐关键词，如：推荐、热门、新片等"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/trending":
		text := "🔥 请使用 /start 菜单中的 AI 推荐功能"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/requests":
		text := "📋 请使用 /start 菜单中的 我的请求 功能"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/link":
		text := "🔗 请发送您的 Jellyseerr 用户名进行绑定"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	case "/help":
		sendHelpMessage(telegram, msg.Chat.ID)
	default:
		text := "❓ 未知命令，请使用 /help 查看帮助"
		telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
	}
}

func handleTextQuery(telegram *services.TelegramClient, msg *types.TelegramMessage, sessMgr *session.Manager, cfg *config.Config, registry *callback.Registry) {
	query := msg.Text

	// Check if it's an AI recommendation query
	if isAIQuery(query) {
		handleAIQuery(telegram, msg, sessMgr, cfg)
		return
	}

	// Otherwise, treat as search query
	handleSearchQuery(telegram, msg, sessMgr, cfg)
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

func handleAIQuery(telegram *services.TelegramClient, msg *types.TelegramMessage, sessMgr *session.Manager, cfg *config.Config) {
	// Send loading message
	text := "🤖 正在为您推荐..."
	telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)

	// For now, send trending results
	// TODO: Implement proper AI service integration
	text = "💡 请使用 /start 菜单中的 AI 推荐功能获取更精准的推荐"
	telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)
}

func handleSearchQuery(telegram *services.TelegramClient, msg *types.TelegramMessage, sessMgr *session.Manager, cfg *config.Config) {
	text := fmt.Sprintf("🔍 搜索: %s\n\n正在搜索...", msg.Text)
	telegram.SendMessage(msg.Chat.ID, text, "Markdown", nil)

	// TODO: Implement actual search
	// This will be handled by the SearchHandler in callback system
}

func sendStartMenu(telegram *services.TelegramClient, chatID int64) {
	msg := services.NewMessageBuilder()
	msg.Bold("🌟 欢迎使用 Emby Telegram Bot").Newline()
	msg.Newline()
	msg.Text("请选择操作：")

	keyboard := services.BuildStartKeyboard()
	telegram.SendMessage(chatID, msg.Build(), "Markdown", keyboard)
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

	keyboard := services.BuildStartKeyboard()
	telegram.SendMessage(chatID, msg.Build(), "Markdown", keyboard)
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
