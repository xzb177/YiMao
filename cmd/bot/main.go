package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"emby-telegram-bot/internal/bot"
	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/handlers"
	"emby-telegram-bot/internal/middleware"
	"emby-telegram-bot/internal/server"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

func main() {
	log.Println("🚀 Starting Emby Telegram Bot...")

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
	deps := initServices(cfg, chatID)

	// Initialize Security Service
	securityService := services.NewSecurityService()
	// Configure security limits from config
	securityService.SetConfig(
		cfg.RateLimitRequests,
		cfg.RateLimitWindow,
		cfg.MaxFailedAttempts,
		cfg.BlockDuration,
	)
	if cfg.EnableAPIAuth && len(cfg.APIKeys) > 0 {
		securityService.SetAPIKeys(cfg.APIKeys)
		securityService.EnableAPIAuth(true)
	}
	securityService.Start()
	log.Printf("✅ Security service initialized: rate_limit=%v, ip_blocking=%v",
		cfg.EnableRateLimit, cfg.EnableIPBlocking)

	// Initialize callback registry (creates handlers including FeedbackHandler)
	registry, depsWithHandlers := initRegistry(deps)

	// Setup bot command menu
	setupBotCommands(depsWithHandlers.Telegram)

	// Setup webhook (if configured)
	setupWebhook(depsWithHandlers.Telegram, cfg)

	// Start polling for updates if no webhook is configured
	go bot.StartPolling(toBotDeps(depsWithHandlers), cfg, registry)

	// Create HTTP server
	srv := server.New(cfg, registry, &server.Dependencies{
		Telegram:          depsWithHandlers.Telegram,
		MoviePilot:        depsWithHandlers.MoviePilot,
		SessionMgr:        depsWithHandlers.SessionMgr,
		UserMapping:       depsWithHandlers.UserMapping,
		Preferences:       depsWithHandlers.Preferences,
		IssueService:      depsWithHandlers.IssueService,
		AdminService:      depsWithHandlers.AdminService,
		QuotaService:      depsWithHandlers.QuotaService,
		ChatService:       depsWithHandlers.ChatService,
		WebhookService:    depsWithHandlers.WebhookService,
		BindingRequest:    depsWithHandlers.BindingRequest,
		MediaNotification: depsWithHandlers.MediaNotification,
		FeedbackHandler:   depsWithHandlers.FeedbackHandler,
	}, securityService)

	// Start server in background
	go func() {
		log.Printf("🌐 Server listening on %s:%s", cfg.ServerHost, cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server...")

	// Stop security service
	securityService.Stop()

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("❌ Server shutdown error: %v", err)
	}

	log.Println("✅ Server stopped")
}

// Dependencies holds all service dependencies
type Dependencies struct {
	Telegram          *services.TelegramClient
	MoviePilot        *services.MoviePilotClient
	SessionMgr        *session.Manager
	UserMapping       *services.UserMappingService
	BindingRequest    *services.BindingRequestService
	Preferences       *services.PreferencesService
	IssueService      *services.IssueService
	AdminService      *services.AdminService
	QuotaService      *services.QuotaService
	ReviewService     *services.ReviewService
	MediaNotification *services.MediaNotificationService
	ChatService       *services.ChatService
	WebhookService    *services.WebhookService
	TMDBClient        *services.TMDBClient
	Notification      *services.NotificationService
	Scheduler         *services.Scheduler
	SearchHistory     *services.SearchHistoryService
	FeedbackHandler   *handlers.FeedbackHandler
}

// initServices initializes all services
func initServices(cfg *config.Config, chatID int64) *Dependencies {
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
	reviewService.SetMoviePilotClient(moviepilotClient)

	// Initialize Media Notification Service
	mediaNotificationSvc := services.NewMediaNotificationService(cfg.DataDir, telegramClient, adminService)
	log.Println("✅ Media notification service initialized")

	// Initialize AI Chat Service
	chatService := services.NewChatService(cfg.ZhipuAPIKey)
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

	// Initialize TMDB client
	tmdbClient := services.NewTMDBClientWithDefaultKey(cfg.TMDBAPIKey)

	// Initialize Notification Service
	notificationService := services.NewNotificationService(telegramClient, moviepilotClient, userMappingService, cfg.DataDir)
	log.Println("✅ Notification service initialized")

	// Initialize Scheduler for daily recommendations
	scheduler := services.NewScheduler(notificationService, moviepilotClient, adminService, userMappingService)
	scheduler.SetDailyTime(9, 0) // 9 AM daily
	scheduler.Start()
	log.Println("✅ Scheduler started")

	// Initialize Search History Service
	searchHistory := services.NewSearchHistoryService(cfg.DataDir)
	log.Println("✅ Search history service initialized")

	// Start cleanup routines
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			bindingRequestService.CleanupExpiredRequests()
		}
	}()

	log.Println("✅ Services initialized")

	return &Dependencies{
		Telegram:          telegramClient,
		MoviePilot:        moviepilotClient,
		SessionMgr:        sessMgr,
		UserMapping:       userMappingService,
		BindingRequest:    bindingRequestService,
		Preferences:       preferencesService,
		IssueService:      issueService,
		AdminService:      adminService,
		QuotaService:      quotaService,
		ReviewService:     reviewService,
		MediaNotification: mediaNotificationSvc,
		ChatService:       chatService,
		WebhookService:    webhookService,
		TMDBClient:        tmdbClient,
		Notification:      notificationService,
		Scheduler:         scheduler,
		SearchHistory:     searchHistory,
	}
}

// initRegistry initializes the callback registry and handlers
func initRegistry(services *Dependencies) (*callback.Registry, *Dependencies) {
	registry := callback.NewRegistry()

	// Apply middleware
	registry.Use(middleware.Recovery)
	registry.Use(middleware.Logger)
	registry.Use(middleware.Validator)

	// Create handlers
	startHandler := handlers.NewStartHandler(nil, services.SessionMgr, services.Telegram, services.MoviePilot)
	detailHandler := handlers.NewDetailHandler(services.SessionMgr, services.Telegram, services.MoviePilot, services.TMDBClient)
	backHandler := handlers.NewBackHandler(services.SessionMgr)
	cancelHandler := handlers.NewCancelHandler()
	requestHandler := handlers.NewRequestHandler(services.SessionMgr, services.Telegram, services.MoviePilot, services.AdminService, services.WebhookService, services.UserMapping, services.QuotaService, services.ReviewService)
	searchHandler := handlers.NewSearchHandler(services.SessionMgr, services.Telegram, services.MoviePilot, services.TMDBClient)
	myRequestsHandler := handlers.NewMyRequestsHandler(services.SessionMgr, services.Telegram, services.MoviePilot)
	linkHandler := handlers.NewLinkHandler(nil, services.SessionMgr, services.Telegram, services.MoviePilot, services.UserMapping, services.BindingRequest)
	helpHandler := handlers.NewHelpHandler()
	aiHandler := handlers.NewAIHandler(nil, services.SessionMgr, services.Telegram, services.MoviePilot)
	adminHandler := handlers.NewAdminHandler(nil, services.SessionMgr, services.Telegram, services.MoviePilot, services.AdminService, services.QuotaService)
	reviewHandler := handlers.NewReviewHandler(services.SessionMgr, services.Telegram, services.MoviePilot, services.AdminService, services.ReviewService)
	feedbackHandler := handlers.NewFeedbackHandler(services.SessionMgr, services.Telegram, services.AdminService)

	// Inject dependencies
	startHandler.SetAdminService(services.AdminService)
	backHandler.SetAdminService(services.AdminService)
	adminHandler.SetMediaNotificationService(services.MediaNotification)
	myRequestsHandler.SetUserMapping(services.UserMapping)
	aiHandler.SetTMDBClient(services.TMDBClient)
	searchHandler.SetSearchHistory(services.SearchHistory)
	feedbackHandler.SetIssueService(services.IssueService)

	// Register callbacks
	registry.RegisterFunc(callback.ActionStart, startHandler.Handle)
	registry.RegisterFunc(callback.ActionSearch, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionAI, aiHandler.Handle)
	registry.RegisterFunc(callback.ActionHot, aiHandler.HandleHot)
	registry.RegisterFunc(callback.ActionNew, aiHandler.HandleNew)
	registry.RegisterFunc(callback.ActionDetail, detailHandler.Handle)
	registry.RegisterFunc(callback.ActionDetailSeasons, detailHandler.Handle)
	registry.RegisterFunc(callback.ActionRequest, requestHandler.Handle)
	registry.RegisterFunc(callback.ActionPage, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionSelect, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionBack, backHandler.Handle)
	registry.RegisterFunc(callback.ActionCancel, cancelHandler.Handle)
	registry.RegisterFunc(callback.ActionRequests, myRequestsHandler.Handle)
	registry.RegisterFunc(callback.ActionLink, linkHandler.Handle)
	registry.RegisterFunc(callback.ActionHelp, helpHandler.Handle)
	registry.RegisterFunc(callback.ActionFeedback, feedbackHandler.Handle)
	registry.RegisterFunc("admin_approve", adminHandler.Handle)
	registry.RegisterFunc("admin_decline", adminHandler.Handle)
	registry.RegisterFunc("admin_pending", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_reply", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_fixed", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_processing", adminHandler.Handle)
	registry.RegisterFunc("admin_issue_close", adminHandler.Handle)
	registry.RegisterFunc("admin_menu", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_settings", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_toggle_instant", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_toggle_daily", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_toggle", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_settime", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_format_simple", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_format_detailed", adminHandler.Handle)

	// Review system callbacks
	registry.RegisterFunc("review_approve", reviewHandler.Handle)
	registry.RegisterFunc("review_reject", reviewHandler.Handle)
	registry.RegisterFunc("review_cancel", reviewHandler.Handle)
	registry.RegisterFunc("my_reviews", reviewHandler.Handle)
	registry.RegisterFunc("review_list", reviewHandler.Handle)

	// Emby library check callbacks
	registry.RegisterFunc("force_subscribe", requestHandler.HandleForceSubscribe)
	registry.RegisterFunc("cancel_request", requestHandler.HandleCancelRequest)

	log.Println("✅ Callback handlers registered")

	// Build full dependencies
	deps := &Dependencies{
		Telegram:          services.Telegram,
		MoviePilot:        services.MoviePilot,
		SessionMgr:        services.SessionMgr,
		UserMapping:       services.UserMapping,
		BindingRequest:    services.BindingRequest,
		Preferences:       services.Preferences,
		IssueService:      services.IssueService,
		AdminService:      services.AdminService,
		QuotaService:      services.QuotaService,
		ReviewService:     services.ReviewService,
		MediaNotification: services.MediaNotification,
		ChatService:       services.ChatService,
		WebhookService:    services.WebhookService,
		TMDBClient:        services.TMDBClient,
		Notification:      services.Notification,
		Scheduler:         services.Scheduler,
		SearchHistory:     services.SearchHistory,
		FeedbackHandler:   feedbackHandler,
	}

	return registry, deps
}

// setupBotCommands sets up the bot command menu
func setupBotCommands(telegram *services.TelegramClient) {
	commands := []services.BotCommand{
		{Command: "start", Description: "🌟 打开主菜单"},
		{Command: "search", Description: "🔍 搜索影片"},
		{Command: "ai", Description: "🤖 AI 推荐菜单"},
		{Command: "requests", Description: "📋 我的请求"},
		{Command: "link", Description: "🔗 绑定账号"},
		{Command: "quota", Description: "💎 查看配额"},
		{Command: "help", Description: "❓ 帮助中心"},
	}
	if err := telegram.SetMyCommands(commands, ""); err != nil {
		log.Printf("⚠️  Failed to set bot commands: %v", err)
	} else {
		log.Println("✅ Bot command menu set")
	}
}

// setupWebhook configures the Telegram webhook
func setupWebhook(telegram *services.TelegramClient, cfg *config.Config) {
	if cfg.WebhookURL != "" {
		if err := telegram.SetWebhook(cfg.WebhookURL); err != nil {
			log.Printf("⚠️  Failed to set webhook: %v", err)
		} else {
			log.Printf("✅ Webhook set: %s", cfg.WebhookURL)
		}
	} else {
		log.Println("⚠️  No webhook URL configured, deleting webhook to enable polling")
		telegram.DeleteWebhook()
	}
}

// toBotDeps converts main Dependencies to bot Dependencies
func toBotDeps(deps *Dependencies) *bot.Dependencies {
	return &bot.Dependencies{
		Telegram:       deps.Telegram,
		MoviePilot:     deps.MoviePilot,
		SessionMgr:     deps.SessionMgr,
		UserMapping:    deps.UserMapping,
		BindingRequest: deps.BindingRequest,
		AdminService:   deps.AdminService,
		QuotaService:   deps.QuotaService,
		ChatService:    deps.ChatService,
		SearchHistory:  deps.SearchHistory,
		TMDB:           deps.TMDBClient,
	}
}

// toServerDeps converts main Dependencies to server Dependencies
func toServerDeps(deps *Dependencies) *server.Dependencies {
	return &server.Dependencies{
		Telegram:          deps.Telegram,
		MoviePilot:        deps.MoviePilot,
		SessionMgr:        deps.SessionMgr,
		UserMapping:       deps.UserMapping,
		Preferences:       deps.Preferences,
		IssueService:      deps.IssueService,
		AdminService:      deps.AdminService,
		QuotaService:      deps.QuotaService,
		ChatService:       deps.ChatService,
		WebhookService:    deps.WebhookService,
		BindingRequest:    deps.BindingRequest,
		MediaNotification: deps.MediaNotification,
	}
}

// createServer creates the HTTP server
func createServer(cfg *config.Config, registry *callback.Registry, deps *Dependencies, securityService *services.SecurityService) *http.Server {
	return server.New(cfg, registry, toServerDeps(deps), securityService)
}
