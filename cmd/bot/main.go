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
	SearchHistoryDB   *services.SearchHistoryDB
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
	reviewService := services.NewReviewService(cfg.DataDir, cfg.EnableAutoResubscribe)
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

	// Initialize Search History Service (legacy, for backward compatibility)
	searchHistory := services.NewSearchHistoryService(cfg.DataDir)

	// Initialize Search History DB with cache (new, advanced features)
	log.Println("    - SearchHistoryDB...")
	searchHistoryDB, err := services.NewSearchHistoryDB(cfg.DataDir)
	if err != nil {
		log.Printf("⚠️  Failed to create SearchHistoryDB: %v", err)
		searchHistoryDB = nil
	} else {
		log.Println("    - SearchHistoryDB initialized")
	}

	// Start cleanup routines
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Cleanup] Panic recovered in cleanup routine: %v", r)
			}
		}()
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
		SearchHistoryDB:   searchHistoryDB,
	}
}

// initRegistry initializes the callback registry and handlers
func initRegistry(deps *Dependencies) (*callback.Registry, *Dependencies) {
	registry := callback.NewRegistry()

	// Apply middleware
	registry.Use(middleware.Recovery)
	registry.Use(middleware.Logger)
	registry.Use(middleware.Validator)

	// Create handlers
	startHandler := handlers.NewStartHandler(nil, deps.SessionMgr, deps.Telegram, deps.MoviePilot)
	detailHandler := handlers.NewDetailHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient)
	backHandler := handlers.NewBackHandler(deps.SessionMgr)
	cancelHandler := handlers.NewCancelHandler()
	requestHandler := handlers.NewRequestHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient, deps.AdminService, deps.WebhookService, deps.UserMapping, deps.QuotaService, deps.ReviewService)
	searchHandler := handlers.NewSearchHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient)
	if deps.SearchHistoryDB != nil {
		searchHandler.SetSearchHistoryDB(deps.SearchHistoryDB)
	}
	myRequestsHandler := handlers.NewMyRequestsHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot)
	linkHandler := handlers.NewLinkHandler(nil, deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.UserMapping, deps.BindingRequest)
	helpHandler := handlers.NewHelpHandler()
	adminHandler := handlers.NewAdminHandler(deps.Cfg, deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.AdminService, deps.QuotaService)
	reviewHandler := handlers.NewReviewHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.AdminService, deps.ReviewService, deps.QuotaService, deps.WebhookService)
	feedbackHandler := handlers.NewFeedbackHandler(deps.SessionMgr, deps.Telegram, deps.AdminService)

	// Initialize site adapter registry for resource candidates
	siteRegistry := services.NewSiteRegistry()
	// Register site adapters with passkeys from config
	hdskyAdapter := services.NewSkyIslandAdapter(siteRegistry.HTTPClient())
	zhuqueAdapter := services.NewZhuQueAdapter(siteRegistry.HTTPClient())
	mteamAdapter := services.NewMTeamAdapter(siteRegistry.HTTPClient())

	// Set passkeys from config
	if deps.Cfg.HDSkyPasskey != "" {
		hdskyAdapter.SetCredentials(map[string]string{"passkey": deps.Cfg.HDSkyPasskey})
		log.Println("    - HD-Sky passkey configured")
	}
	// ZhuQue: support both legacy passkey and new RSS key format
	if deps.Cfg.ZhuQueRSSKey1 != "" && deps.Cfg.ZhuQueRSSKey2 != "" {
		zhuqueAdapter.SetCredentials(map[string]string{
			"rss_key1": deps.Cfg.ZhuQueRSSKey1,
			"rss_key2": deps.Cfg.ZhuQueRSSKey2,
		})
		log.Println("    - ZhuQue RSS new format configured")
	} else if deps.Cfg.ZhuQuePasskey != "" {
		zhuqueAdapter.SetCredentials(map[string]string{"passkey": deps.Cfg.ZhuQuePasskey})
		log.Println("    - ZhuQue passkey configured (legacy)")
	}
	// M-Team: support both legacy passkey and new RSS format
	if deps.Cfg.MTeamRSSUID != "" && deps.Cfg.MTeamRSSSign != "" {
		mteamAdapter.SetCredentials(map[string]string{
			"rss_uid":  deps.Cfg.MTeamRSSUID,
			"rss_sign": deps.Cfg.MTeamRSSSign,
		})
		log.Println("    - M-Team RSS new format configured")
	} else if deps.Cfg.MTeamPasskey != "" {
		mteamAdapter.SetCredentials(map[string]string{"passkey": deps.Cfg.MTeamPasskey})
		log.Println("    - M-Team passkey configured (legacy)")
	}

	siteRegistry.Register(hdskyAdapter)
	siteRegistry.Register(zhuqueAdapter)
	siteRegistry.Register(mteamAdapter)
	log.Println("    - SiteRegistry initialized with 3 adapters (HD-Sky, ZhuQue, M-Team)")

	resourceHandler := handlers.NewResourceHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient, siteRegistry)

	// Search History Handler (if DB is available)
	var searchHistoryHandler *handlers.SearchHistoryHandler
	if deps.SearchHistoryDB != nil {
		searchHistoryHandler = handlers.NewSearchHistoryHandler(deps.Telegram, deps.SearchHistoryDB)
		log.Println("    - SearchHistoryHandler created")
	} else {
		log.Println("    - SearchHistoryHandler skipped (DB not available)")
	}

	// Inject dependencies
	startHandler.SetAdminService(deps.AdminService)
	backHandler.SetAdminService(deps.AdminService)
	adminHandler.SetMediaNotificationService(deps.MediaNotification)
	adminHandler.SetIssueService(deps.IssueService)
	myRequestsHandler.SetUserMapping(deps.UserMapping)
	searchHandler.SetSearchHistory(deps.SearchHistory)
	feedbackHandler.SetIssueService(deps.IssueService)
	feedbackHandler.SetTMDBClient(deps.TMDBClient)

	// Register callbacks
	registry.RegisterFunc(callback.ActionStart, startHandler.Handle)
	registry.RegisterFunc(callback.ActionSearch, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionAI, startHandler.Handle)
	registry.RegisterFunc(callback.ActionMood, startHandler.Handle)
	registry.RegisterFunc(callback.ActionMoodPick, startHandler.Handle)
	registry.RegisterFunc(callback.ActionQuickPick, startHandler.Handle)
	registry.RegisterFunc(callback.ActionHot, startHandler.Handle)
	registry.RegisterFunc(callback.ActionNew, startHandler.Handle)
	registry.RegisterFunc(callback.ActionSettings, startHandler.Handle)
	registry.RegisterFunc(callback.ActionHelpTopic, startHandler.Handle)
	registry.RegisterFunc("start_settings", startHandler.Handle)
	registry.RegisterFunc("start_ai", startHandler.Handle)
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

	// Search History callbacks (if handler is available)
	if searchHistoryHandler != nil {
		registry.RegisterFunc("search_history_menu", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_stats", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_popular", searchHistoryHandler.Handle)
		registry.RegisterFunc("popular_week", searchHistoryHandler.Handle)
		registry.RegisterFunc("popular_all", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_trends", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_manage", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_delete", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_clear_all", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_popular_refresh", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_trends_refresh", searchHistoryHandler.Handle)
		registry.RegisterFunc("search_input", searchHandler.Handle)
		log.Println("    - Search History callbacks registered")
	}

	// Admin Feedback Panel callbacks
	registry.RegisterFunc("admin_feedback", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_stats", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_list", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_filter", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_detail", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_reply", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_priority", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_template", adminHandler.Handle)

	// User Feedback interaction callbacks
	registry.RegisterFunc("feedback_follow_up", feedbackHandler.Handle)
	registry.RegisterFunc("feedback_close", feedbackHandler.Handle)
	registry.RegisterFunc("feedback_rate_1", feedbackHandler.Handle)
	registry.RegisterFunc("feedback_rate_2", feedbackHandler.Handle)
	registry.RegisterFunc("feedback_rate_3", feedbackHandler.Handle)
	registry.RegisterFunc("feedback_rate_4", feedbackHandler.Handle)
	registry.RegisterFunc("feedback_rate_5", feedbackHandler.Handle)

	// Resource candidate callbacks
	registry.RegisterFunc(callback.ActionResourceList, resourceHandler.Handle)
	registry.RegisterFunc(callback.ActionResourcePick, resourceHandler.Handle)
	registry.RegisterFunc(callback.ActionResourceSort, resourceHandler.Handle)
	registry.RegisterFunc(callback.ActionResourcePrev, resourceHandler.Handle)
	registry.RegisterFunc(callback.ActionResourceNext, resourceHandler.Handle)
	// Short format callback for resource pick (to stay under 64 bytes)
	registry.RegisterFunc("rp", resourceHandler.Handle)

	log.Println("✅ Callback handlers registered")

	// Build full dependencies with handlers included
	resultDeps := &Dependencies{
		Cfg:               deps.Cfg,
		Telegram:          deps.Telegram,
		MoviePilot:        deps.MoviePilot,
		SessionMgr:        deps.SessionMgr,
		UserMapping:       deps.UserMapping,
		BindingRequest:    deps.BindingRequest,
		Preferences:       deps.Preferences,
		IssueService:      deps.IssueService,
		AdminService:      deps.AdminService,
		AdminHandler:      adminHandler,
		QuotaService:      deps.QuotaService,
		ReviewService:     deps.ReviewService,
		MediaNotification: deps.MediaNotification,
		WebhookService:    deps.WebhookService,
		TMDBClient:        deps.TMDBClient,
		Notification:      deps.Notification,
		Scheduler:         deps.Scheduler,
		SearchHistory:     deps.SearchHistory,
		SearchHistoryDB:   deps.SearchHistoryDB,
		FeedbackHandler:   feedbackHandler,
	}

	return registry, resultDeps
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
		SearchHistoryDB: deps.SearchHistoryDB,
		TMDB:            deps.TMDBClient,
		IssueService:    deps.IssueService,
		FeedbackHandler: deps.FeedbackHandler,
	}
}
