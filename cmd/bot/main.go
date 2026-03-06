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

	// Parse chat ID (optional - empty means no group notifications)
	log.Println("🔍 Parsing chat ID...")
	chatID := int64(0)
	if cfg.TelegramChatID != "" {
		var err error
		chatID, err = strconv.ParseInt(cfg.TelegramChatID, 10, 64)
		if err != nil {
			log.Fatalf("❌ Invalid Telegram Chat ID '%s': %v", cfg.TelegramChatID, err)
		}
		log.Printf("✅ Chat ID parsed: %d", chatID)
	} else {
		log.Println("ℹ️  No group chat ID configured (group notifications disabled)")
	}

	// Initialize services
	log.Println("🔧 Initializing services...")
	deps := initServices(cfg, chatID)
	log.Println("✅ Services initialized")

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
		WebhookService:    depsWithHandlers.WebhookService,
		BindingRequest:    depsWithHandlers.BindingRequest,
		MediaNotification: depsWithHandlers.MediaNotification,
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
	Cfg               *config.Config
	Telegram          *services.TelegramClient
	MoviePilot        *services.MoviePilotClient
	SessionMgr        *session.Manager
	UserMapping       *services.UserMappingService
	BindingRequest    *services.BindingRequestService
	Preferences       *services.PreferencesService
	IssueService      *services.IssueService
	AdminService      *services.AdminService
	AdminHandler      *handlers.AdminHandler
	QuotaService      *services.QuotaService
	ReviewService     *services.ReviewService
	MediaNotification *services.MediaNotificationService
	WebhookService    *services.WebhookService
	TMDBClient        *services.TMDBClient
	Notification      *services.NotificationService
	Scheduler         *services.Scheduler
	SearchHistory     *services.SearchHistoryService
	FeedbackHandler   *handlers.FeedbackHandler
}

// initServices initializes all services
func initServices(cfg *config.Config, chatID int64) *Dependencies {
	log.Println("  [1/11] Creating basic clients and services...")
	log.Println("    - TelegramClient...")
	telegramClient := services.NewTelegramClient(cfg.TelegramBotToken)
	log.Println("    - ImageCache...")
	imageCache := services.NewImageCache(cfg.DataDir, 7*24*time.Hour) // 7天缓存
	telegramClient.SetImageCache(imageCache)
	log.Println("    - MoviePilotClient...")
	moviepilotClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey)
	log.Println("    - SessionManager...")
	sessMgr := session.NewManager(time.Duration(cfg.MaxSessionAge)*time.Hour, cfg.MaxSessions)
	log.Println("    - UserMappingService...")
	userMappingService := services.NewUserMappingService(cfg.DataDir)
	log.Println("    - BindingRequestService...")
	bindingRequestService := services.NewBindingRequestService(cfg.DataDir)
	log.Println("    - PreferencesService...")
	preferencesService := services.NewPreferencesService(cfg.DataDir)
	log.Println("    - IssueService...")
	issueService := services.NewIssueService(cfg.DataDir)
	log.Println("    - AdminService...")
	adminService := services.NewAdminService(cfg.DataDir)
	log.Println("    - QuotaService...")
	quotaService := services.NewQuotaService(cfg.DataDir, moviepilotClient)
	log.Println("    - ReviewService...")
	reviewService := services.NewReviewService(cfg.DataDir)
	log.Println("    - Setting MoviePilotClient...")
	reviewService.SetMoviePilotClient(moviepilotClient)
	log.Println("  [2/11] Basic services created")

	// Initialize Media Notification Service
	mediaNotificationSvc := services.NewMediaNotificationService(cfg.DataDir, telegramClient, adminService, chatID)
	log.Println("  [3/11] Media notification service initialized")

	// Set admin IDs for quota service (admins have unlimited quota)
	adminsMap := adminService.GetAllAdmins()
	var adminIDs []int64
	for idStr := range adminsMap {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			adminIDs = append(adminIDs, id)
		}
	}
	quotaService.SetAdminIDs(adminIDs)
	log.Println("  [4/11] Admin IDs configured")

	log.Println("  [5/11] Creating webhook service...")
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
		cfg.NotificationFormat,
		cfg.TMDBAPIKey,
	)
	log.Println("  [6/11] Webhook service created")

	// Initialize TMDB client
	tmdbClient := services.NewTMDBClientWithDefaultKey(cfg.TMDBAPIKey)
	log.Println("  [7/11] TMDB client created")

	// Initialize Notification Service
	log.Println("  [8/11] Creating notification service...")
	notificationService := services.NewNotificationService(telegramClient, moviepilotClient, userMappingService, cfg.DataDir)
	log.Println("  [9/11] Notification service created")

	// Initialize Scheduler for daily recommendations
	log.Println("  [10/11] Creating scheduler...")
	scheduler := services.NewScheduler(notificationService, moviepilotClient, adminService, userMappingService)
	scheduler.SetDailyTime(9, 0) // 9 AM daily
	scheduler.Start()
	log.Println("  [11/11] Scheduler started")

	// Initialize Search History Service
	searchHistory := services.NewSearchHistoryService(cfg.DataDir)

	// Start cleanup routines
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			bindingRequestService.CleanupExpiredRequests()
		}
	}()

	// Start image cache cleanup routine (每天清理一次过期缓存)
	imageCache.StartCleanupRoutine(24 * time.Hour)

	log.Println("✅ Services initialized")

	return &Dependencies{
		Cfg:               cfg,
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
	adminHandler := handlers.NewAdminHandler(services.Cfg, services.SessionMgr, services.Telegram, services.MoviePilot, services.AdminService, services.QuotaService)
	reviewHandler := handlers.NewReviewHandler(services.SessionMgr, services.Telegram, services.MoviePilot, services.AdminService, services.ReviewService, services.QuotaService, services.WebhookService)
	feedbackHandler := handlers.NewFeedbackHandler(services.SessionMgr, services.Telegram, services.AdminService)

	// Inject dependencies
	startHandler.SetAdminService(services.AdminService)
	backHandler.SetAdminService(services.AdminService)
	adminHandler.SetMediaNotificationService(services.MediaNotification)
	adminHandler.SetIssueService(services.IssueService)
	myRequestsHandler.SetUserMapping(services.UserMapping)
	searchHandler.SetSearchHistory(services.SearchHistory)
	feedbackHandler.SetIssueService(services.IssueService)

	// Register callbacks
	registry.RegisterFunc(callback.ActionStart, startHandler.Handle)
	registry.RegisterFunc(callback.ActionSearch, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionAI, startHandler.Handle)
	registry.RegisterFunc(callback.ActionHot, startHandler.Handle)
	registry.RegisterFunc(callback.ActionNew, startHandler.Handle)
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
	log.Printf("[initRegistry] Registering FeedbackHandler: feedbackHandler=%v", feedbackHandler != nil)
	registry.RegisterFunc(callback.ActionFeedback, feedbackHandler.Handle)
	registry.RegisterFunc("my_feedback", feedbackHandler.Handle)
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
	// V2 通知设置回调 - 状态融合按钮
	registry.RegisterFunc("admin_notif_toggle_single_v2", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_toggle_daily_v2", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_toggle_format", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_disable_all", adminHandler.Handle)
	registry.RegisterFunc("admin_notif_custom_time", adminHandler.Handle)
	// 管理员管理回调
	registry.RegisterFunc("admin_mgmt", adminHandler.Handle)
	registry.RegisterFunc("admin_list", adminHandler.Handle)
	registry.RegisterFunc("admin_add_start", adminHandler.Handle)
	registry.RegisterFunc("admin_remove_list", adminHandler.Handle)
	registry.RegisterFunc("admin_remove_confirm", adminHandler.Handle)

	// Review system callbacks
	registry.RegisterFunc("review_approve", reviewHandler.Handle)
	registry.RegisterFunc("review_reject", reviewHandler.Handle)
	registry.RegisterFunc("review_cancel", reviewHandler.Handle)
	registry.RegisterFunc("my_reviews", reviewHandler.Handle)
	registry.RegisterFunc("review_list", reviewHandler.Handle)
	// Short format callbacks (to keep CallbackData under 64 bytes)
	registry.RegisterFunc("rv_a", reviewHandler.Handle)
	registry.RegisterFunc("rv_r", reviewHandler.Handle)

	// Emby library check callbacks
	registry.RegisterFunc("force_subscribe", requestHandler.HandleForceSubscribe)
	registry.RegisterFunc("cancel_request", requestHandler.HandleCancelRequest)

	// My Requests pagination callbacks
	registry.RegisterFunc(callback.ActionMyReqsPage, myRequestsHandler.HandlePage)
	registry.RegisterFunc(callback.ActionMyReqsItem, myRequestsHandler.HandleItemAction)

	log.Println("✅ Callback handlers registered")

	// Build full dependencies
	deps := &Dependencies{
		Cfg:               services.Cfg,
		Telegram:          services.Telegram,
		MoviePilot:        services.MoviePilot,
		SessionMgr:        services.SessionMgr,
		UserMapping:       services.UserMapping,
		BindingRequest:    services.BindingRequest,
		Preferences:       services.Preferences,
		IssueService:      services.IssueService,
		AdminService:      services.AdminService,
		AdminHandler:      adminHandler,
		QuotaService:      services.QuotaService,
		ReviewService:     services.ReviewService,
		MediaNotification: services.MediaNotification,
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
		{Command: "ai", Description: "🎬 精选推荐"},
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
		Telegram:        deps.Telegram,
		MoviePilot:      deps.MoviePilot,
		SessionMgr:      deps.SessionMgr,
		UserMapping:     deps.UserMapping,
		BindingRequest:  deps.BindingRequest,
		AdminService:    deps.AdminService,
		AdminHandler:    deps.AdminHandler,
		QuotaService:    deps.QuotaService,
		SearchHistory:   deps.SearchHistory,
		TMDB:            deps.TMDBClient,
		IssueService:    deps.IssueService,
		FeedbackHandler: deps.FeedbackHandler,
	}
}
