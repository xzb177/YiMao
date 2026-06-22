package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/xzb177/yimao/internal/bot"
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/middleware"
	"github.com/xzb177/yimao/internal/server"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

func main() {
	// 强制初始化时区为 Asia/Shanghai，不依赖宿主机 Alpine 镜像的时区配置
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		time.Local = loc
	}
	log.Println("🚀 Starting Emby Telegram Bot...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger with configuration
	logLevel := logger.ParseLevel(cfg.LogLevel)
	logger.InitLogger(logLevel, cfg.LogPrefix, cfg.LogColor)
	logger.Info("✅ Logger initialized: level=%s, prefix=%s, color=%v", cfg.LogLevel, cfg.LogPrefix, cfg.LogColor)

	logger.Info("✅ Configuration loaded")
	logger.Info("   MoviePilot: %s", cfg.MoviePilotURL)
	logger.Info("   Data directory: %s", cfg.DataDir)

	// Parse chat ID (optional - empty means no group notifications)
	logger.Info("🔍 Parsing chat ID...")
	chatID := int64(0)
	if cfg.TelegramChatID != "" {
		var err error
		chatID, err = strconv.ParseInt(cfg.TelegramChatID, 10, 64)
		if err != nil {
			log.Fatalf("❌ Invalid Telegram Chat ID '%s': %v", cfg.TelegramChatID, err)
		}
		logger.Info("✅ Chat ID parsed: %d", chatID)
	} else {
		logger.Info("ℹ️  No group chat ID configured (group notifications disabled)")
	}

	// Initialize services
	logger.Info("🔧 Initializing services...")
	deps := initServices(cfg, chatID)
	logger.Info("✅ Services initialized")

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
	logger.Info("✅ Security service initialized: rate_limit=%v, ip_blocking=%v",
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
		FeedbackHandler:   depsWithHandlers.FeedbackHandler,
		WishHandler:       depsWithHandlers.WishHandler,
	}, securityService)

	// Start server in background
	go func() {
		logger.Info("🌐 Server listening on %s:%s", cfg.ServerHost, cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("🛑 Shutting down server...")

	// 第一步：阻断流量（先关 HTTP，等已有请求处理完毕）
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Info("❌ Server shutdown error: %v", err)
	}
	logger.Info("✅ HTTP server stopped")

	// 第二步：停止后台任务
	securityService.Stop()

	// 第三步：关闭底层资源（DB 等）
	// 必须在 HTTP 关闭之后，否则正在处理的请求会 panic（database is closed）
	if depsWithHandlers.WishScheduler != nil {
		depsWithHandlers.WishScheduler.Stop()
	}
	if depsWithHandlers.WishService != nil {
		if err := depsWithHandlers.WishService.Close(); err != nil {
			logger.Info("[wish] 关闭数据库出错: %v", err)
		}
	}
	// 关闭 UserMappingDB
	if umdb, ok := depsWithHandlers.UserMapping.(*services.UserMappingDB); ok {
		if err := umdb.Close(); err != nil {
			logger.Info("[UserMappingDB] 关闭数据库出错: %v", err)
		}
	}

	logger.Info("✅ All services stopped")
}

// Dependencies holds all service dependencies
type Dependencies struct {
	Cfg               *config.Config
	Telegram          *services.TelegramClient
	MoviePilot        *services.MoviePilotClient
	SessionMgr        *session.Manager
	UserMapping       services.UserMappingStore
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
	WeeklyReportSvc   *services.WeeklyReportService
	CarpoolService    *services.CarpoolService    // #3 拼车 +1 服务
	WishService       *services.WishService       // #6 许愿池存储
	WishScheduler     *services.WishScheduler     // #6 许愿池 DailyRescan task
	WishHandler       *handlers.WishHandler       // #6 许愿池命令/回调处理器
	MyRequestsHandler *handlers.MyRequestsHandler // 「我的请求」聚合视图（/requests 命令复用）
}

// initServices initializes all services
func initServices(cfg *config.Config, chatID int64) *Dependencies {
	logger.Info("  [1/11] Creating basic clients and services...")
	logger.Info("    - TelegramClient...")
	telegramClient := services.NewTelegramClient(cfg.TelegramBotToken)
	logger.Info("    - ImageCache...")
	imageCache := services.NewImageCache(cfg.DataDir, 7*24*time.Hour) // 7天缓存
	telegramClient.SetImageCache(imageCache)
	logger.Info("    - MoviePilotClient...")
	moviepilotClient := services.NewMoviePilotClient(cfg.MoviePilotURL, cfg.MoviePilotAPIKey, cfg.DownloadSavePath)
	if cfg.DownloadSavePath != "" {
		logger.Info("    - Download save path configured: %s", cfg.DownloadSavePath)
	}
	// Set Emby config for checking media availability
	if cfg.EmbyURL != "" && cfg.EmbyAPIKey != "" {
		moviepilotClient.SetEmbyConfig(cfg.EmbyURL, cfg.EmbyAPIKey)
		if cfg.EmbyUserID != "" {
			moviepilotClient.SetEmbyUserID(cfg.EmbyUserID)
			logger.Info("    - Emby user ID configured: %s", cfg.EmbyUserID)
		}
		logger.Info("    - Emby integration enabled for completion status")
	}
	logger.Info("    - SessionManager...")
	sessMgr := session.NewManager(time.Duration(cfg.MaxSessionAge)*time.Hour, cfg.MaxSessions)
	logger.Info("    - UserMappingDB (SQLite)...")
	userMappingDB, err := services.NewUserMappingDB(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize UserMappingDB: %v", err)
	}
	// NOTE: 不要在这里 defer Close()，DB 需要保持打开到程序结束
	userMappingService := userMappingDB
	logger.Info("    - BindingRequestService...")
	bindingRequestService := services.NewBindingRequestService(cfg.DataDir)
	logger.Info("    - PreferencesService...")
	preferencesService := services.NewPreferencesService(cfg.DataDir)
	logger.Info("    - IssueService...")
	issueService := services.NewIssueService(cfg.DataDir)
	logger.Info("    - AdminService...")
	adminService := services.NewAdminService(cfg.DataDir)
	logger.Info("    - QuotaService...")
	quotaService := services.NewQuotaService(cfg.DataDir, moviepilotClient)
	logger.Info("    - ReviewService...")
	reviewService := services.NewReviewService(cfg.DataDir, cfg.EnableAutoResubscribe)
	logger.Info("    - Setting MoviePilotClient...")
	reviewService.SetMoviePilotClient(moviepilotClient)
	reviewService.SetUserMapping(userMappingService) // Issue #1: 全量检测需要 MP 用户名→TG ID 反查

	// A4: 数据自动备份（每 24h 备份一次，保留 7 天，启动时立即执行一次）
	backupService := services.NewDataBackupService(cfg.DataDir, 24*time.Hour, 7*24*time.Hour)
	backupService.Start()
	logger.Info("    - BackupService started (interval=24h, retention=7d)")

	// B4: 系统告警（stuck 未处理 / MP API 连续失败 时通知管理员）
	// 用第一个管理员作为告警接收人（adminService 已加载完管理员列表）
	var alertService *services.AlertService
	if adminIDs := adminService.GetAdminIDs(); len(adminIDs) > 0 {
		alertService = services.NewAlertService(telegramClient, adminIDs[0], 30*time.Minute)
		logger.Info("    - AlertService initialized (admin=%d)", adminIDs[0])
	}
	// P1：MP 轮询订阅完成时通知用户（替代 Emby webhook，Emby 不可用时仍能通知）
	reviewService.OnSubscriptionComplete = func(telegramID int64, title string, year int, mediaType string) {
		// 检查用户是否开启了入库通知
		if !preferencesService.IsNotifyEnabled(telegramID, services.NotifyDownload) {
			logger.Info("[ReviewService] 用户 %d 关闭了入库通知，跳过", telegramID)
			return
		}

		mediaEmoji := "🎬"
		if mediaType == "tv" {
			mediaEmoji = "📺"
		}
		yearStr := ""
		if year > 0 {
			yearStr = fmt.Sprintf(" (%d)", year)
		}

		msg := services.NewMessageBuilder()
		msg.Bold("🎉 求片已完成！").Newline()
		msg.Newline()
		msg.Textf("%s 《%s》%s", mediaEmoji, title, yearStr).Newline()
		msg.Newline()
		msg.Text("🍿 快去 Emby 观看吧～")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("📋 我的求片", "my_requests")
		telegramClient.SendMessage(telegramID, msg.Build(), "HTML", kb.Build())
		logger.Info("[ReviewService] 已通知用户 %d: %s%s 订阅完成", telegramID, title, yearStr)
	}
	// B4: stuck 告警 — 审核通过但 MP 提交失败时通知管理员
	reviewService.Alert = func(requestID, title string, retryCount int, lastError string) {
		if lastError == "自动重试成功" {
			// 自动重试成功：用户会通过 OnSubscriptionComplete 收到通知，这里仅记日志
			logger.Info("[Alert] 求片自动重试成功: %s, 请求 %s, 第 %d 次", title, requestID, retryCount)
			return
		}
		alertService.Warn("review_stuck",
			fmt.Sprintf("求片提交失败: %s", title),
			fmt.Sprintf("请求 %s，已重试 %d 次\n最后错误: %s\n请检查 MoviePilot 状态", requestID, retryCount, lastError),
		)
	}
	// 每日汇总回调
	reviewService.OnDailySummary = func(telegramID int64, message string) {
		if !preferencesService.IsNotifyEnabled(telegramID, services.NotifyDownload) {
			return
		}
		telegramClient.SendMessage(telegramID, message, "", nil)
	}
	logger.Info("    - CarpoolService...")
	carpoolService := services.NewCarpoolService(cfg.DataDir) // #3 拼车 +1 持久化服务
	logger.Info("  [2/11] Basic services created")

	// Initialize Media Notification Service
	mediaNotificationSvc := services.NewMediaNotificationService(cfg.DataDir, telegramClient, adminService, chatID)
	logger.Info("  [3/11] Media notification service initialized")

	// Set admin IDs for quota service (admins have unlimited quota)
	adminsMap := adminService.GetAllAdmins()
	var adminIDs []int64
	for idStr := range adminsMap {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			adminIDs = append(adminIDs, id)
		}
	}
	quotaService.SetAdminIDs(adminIDs)
	logger.Info("  [4/11] Admin IDs configured")

	logger.Info("  [5/11] Creating webhook service...")
	webhookService := services.NewWebhookService(
		telegramClient,
		moviepilotClient,
		userMappingService,
		adminService,
		preferencesService,
		chatID,
		cfg.EmbyURL,
		cfg.EmbyAPIKey,
		cfg.EmbyUserID,
		cfg.EmbySkipTLSVerify,
		mediaNotificationSvc,
		cfg.NotificationFormat,
		cfg.TMDBAPIKey,
	)
	logger.Info("  [6/11] Webhook service created")

	// #3 拼车 +1：把拼车服务注入 webhook，用于入库时 @ 拼车用户（setter 注入，不改构造函数签名）
	webhookService.SetCarpoolService(carpoolService)
	webhookService.SetReviewService(reviewService)

	// Initialize TMDB client
	tmdbClient := services.NewTMDBClientWithDefaultKey(cfg.TMDBAPIKey)
	logger.Info("  [7/11] TMDB client created")

	// Initialize Notification Service
	logger.Info("  [8/11] Creating notification service...")
	notificationService := services.NewNotificationService(telegramClient, moviepilotClient, userMappingService, reviewService, cfg.DataDir)
	notificationService.NotifyEnabled = func(userID int64, key string) bool {
		return preferencesService.IsNotifyEnabled(userID, key)
	}
	logger.Info("  [9/11] Notification service created")

	// Initialize Scheduler for daily recommendations
	logger.Info("  [10/11] Creating scheduler...")
	scheduler := services.NewScheduler(notificationService, moviepilotClient, tmdbClient, adminService, userMappingService, telegramClient, chatID)
	scheduler.SetDailyTime(9, 0) // 9 AM daily
	scheduler.Start()
	logger.Info("  [11/11] Scheduler started")

	// Initialize Search History Service (legacy, for backward compatibility)
	searchHistory := services.NewSearchHistoryService(cfg.DataDir)

	// Initialize Search History DB with cache (new, advanced features)
	logger.Info("    - SearchHistoryDB...")
	searchHistoryDB, err := services.NewSearchHistoryDB(cfg.DataDir)
	if err != nil {
		logger.Info("⚠️  Failed to create SearchHistoryDB: %v", err)
		searchHistoryDB = nil
	} else {
		logger.Info("    - SearchHistoryDB initialized")
	}

	// Initialize Weekly Report Service
	logger.Info("    - WeeklyReportService...")
	var weeklyReportSvc *services.WeeklyReportService
	if searchHistoryDB != nil {
		weeklyReportSvc = services.NewWeeklyReportService(cfg.DataDir, searchHistoryDB, quotaService, reviewService, userMappingService, telegramClient, tmdbClient)
		weeklyReportSvc.NotifyEnabled = func(userID int64, key string) bool {
			return preferencesService.IsNotifyEnabled(userID, key)
		}
	}

	// #6 许愿池：初始化 SQLite 存储 + 单个 DailyRescan 调度 task。
	// 任一环节失败则降级（WishService=nil），命令/回调不接入半成品。
	logger.Info("    - WishService (#6 许愿池)...")
	var wishService *services.WishService
	var wishScheduler *services.WishScheduler
	if ws, werr := services.NewWishService(cfg.DataDir); werr != nil {
		logger.Info("⚠️  Failed to create WishService: %v (许愿池功能禁用)", werr)
	} else {
		wishService = ws
		wishScheduler = services.NewWishScheduler(
			wishService, moviepilotClient, telegramClient, chatID,
			cfg.WishResearchIntervalHours, cfg.WishExpireDays, cfg.WishMinSeeders, cfg.WishSearchLockTTLMinutes,
		)
		// 注入「立即求片」按钮构造（出源喜报用），按钮回调走 wish_request。
		wishScheduler.SetRequestButtonBuilder(handlers.BuildWishRequestButton)
		wishScheduler.Start()
		logger.Info("    - WishScheduler started (#6 DailyRescan)")
	}

	// Start cleanup routines
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Info("[Cleanup] Panic recovered in cleanup routine: %v", r)
			}
		}()
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			bindingRequestService.CleanupExpiredRequests()
		}
	}()

	// Start image cache cleanup routine (每天清理一次过期缓存)
	imageCache.StartCleanupRoutine(24 * time.Hour)

	// 预热 MoviePilot 订阅缓存（后台执行，避免首次「我的请求」点击超时）
	go moviepilotClient.WarmupSubscriptionCache()

	logger.Info("✅ Services initialized")

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
		WeeklyReportSvc:   weeklyReportSvc,
		CarpoolService:    carpoolService,
		WishService:       wishService,
		WishScheduler:     wishScheduler,
	}
}

// initRegistry initializes the callback registry and handlers
func initRegistry(deps *Dependencies) (*callback.Registry, *Dependencies) {
	registry := callback.NewRegistry()

	// Parse group chat ID for notifications
	groupChatID := int64(0)
	if deps.Cfg.TelegramChatID != "" {
		if id, err := strconv.ParseInt(deps.Cfg.TelegramChatID, 10, 64); err == nil {
			groupChatID = id
		}
	}

	// Apply middleware
	registry.Use(middleware.Recovery)
	registry.Use(middleware.Logger)
	registry.Use(middleware.Validator)

	// Create handlers
	startHandler := handlers.NewStartHandler(nil, deps.SessionMgr, deps.Telegram, deps.MoviePilot)
	detailHandler := handlers.NewDetailHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient)
	detailHandler.SetCarpool(deps.CarpoolService)
	backHandler := handlers.NewBackHandler(deps.SessionMgr)
	cancelHandler := handlers.NewCancelHandler()
	requestHandler := handlers.NewRequestHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient, deps.AdminService, deps.WebhookService, deps.UserMapping, deps.QuotaService, deps.ReviewService)
	requestHandler.SetCarpoolService(deps.CarpoolService)
	searchHandler := handlers.NewSearchHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient)
	if deps.SearchHistoryDB != nil {
		searchHandler.SetSearchHistoryDB(deps.SearchHistoryDB)
	}
	myRequestsHandler := handlers.NewMyRequestsHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot)
	linkHandler := handlers.NewLinkHandler(deps.Cfg, deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.UserMapping, deps.BindingRequest)
	helpHandler := handlers.NewHelpHandler()
	adminHandler := handlers.NewAdminHandler(deps.Cfg, deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.AdminService, deps.QuotaService)
	reviewHandler := handlers.NewReviewHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.AdminService, deps.ReviewService, deps.QuotaService, deps.WebhookService, groupChatID)
	feedbackHandler := handlers.NewFeedbackHandler(deps.SessionMgr, deps.Telegram, deps.AdminService)

	// 拼车取消/拒绝通知
	carpoolNotifyFunc := func(tmdbID int, mediaType, title, reason string) {
		userIDs := deps.CarpoolService.GetAndClear(tmdbID, mediaType)
		for _, uid := range userIDs {
			deps.Telegram.SendMessage(uid, fmt.Sprintf("📢 拼车通知\n\n《%s》%s，本次拼车已取消", title, reason), "", nil)
		}
	}
	reviewHandler.OnCarpoolNotify = carpoolNotifyFunc
	myRequestsHandler.OnCarpoolNotify = carpoolNotifyFunc

	// #3 拼车 +1 回调处理器
	carpoolHandler := handlers.NewCarpoolHandler(deps.CarpoolService)
	// #1 注入 Telegram，用于拼车 +1 时尝试打通私聊通道（打招呼）。
	carpoolHandler.SetTelegram(deps.Telegram)

	// #6 许愿池处理器：复用 requestHandler 走现有求片流程（仅在 WishService 就绪时接入）。
	var wishHandler *handlers.WishHandler
	if deps.WishService != nil {
		wishHandler = handlers.NewWishHandler(
			deps.WishService, deps.TMDBClient, deps.MoviePilot, deps.Telegram, deps.SessionMgr, requestHandler,
		)
	}

	// Initialize site adapter registry for resource candidates
	siteRegistry := services.NewSiteRegistry()
	// Register site adapters with passkeys from config
	hdskyAdapter := services.NewSkyIslandAdapter(siteRegistry.HTTPClient())
	zhuqueAdapter := services.NewZhuQueAdapter(siteRegistry.HTTPClient())
	mteamAdapter := services.NewMTeamAdapter(siteRegistry.HTTPClient())

	// Set passkeys from config
	if deps.Cfg.HDSkyPasskey != "" {
		hdskyAdapter.SetCredentials(map[string]string{"passkey": deps.Cfg.HDSkyPasskey})
		logger.Info("    - HD-Sky passkey configured")
	}
	// ZhuQue: support both legacy passkey and new RSS key format
	if deps.Cfg.ZhuQueRSSKey1 != "" && deps.Cfg.ZhuQueRSSKey2 != "" {
		zhuqueAdapter.SetCredentials(map[string]string{
			"rss_key1": deps.Cfg.ZhuQueRSSKey1,
			"rss_key2": deps.Cfg.ZhuQueRSSKey2,
		})
		logger.Info("    - ZhuQue RSS new format configured")
	} else if deps.Cfg.ZhuQuePasskey != "" {
		zhuqueAdapter.SetCredentials(map[string]string{"passkey": deps.Cfg.ZhuQuePasskey})
		logger.Info("    - ZhuQue passkey configured (legacy)")
	}
	// M-Team: support both legacy passkey and new RSS format
	if deps.Cfg.MTeamRSSUID != "" && deps.Cfg.MTeamRSSSign != "" {
		mteamAdapter.SetCredentials(map[string]string{
			"rss_uid":  deps.Cfg.MTeamRSSUID,
			"rss_sign": deps.Cfg.MTeamRSSSign,
		})
		logger.Info("    - M-Team RSS new format configured")
	} else if deps.Cfg.MTeamPasskey != "" {
		mteamAdapter.SetCredentials(map[string]string{"passkey": deps.Cfg.MTeamPasskey})
		logger.Info("    - M-Team passkey configured (legacy)")
	}

	siteRegistry.Register(hdskyAdapter)
	siteRegistry.Register(zhuqueAdapter)
	siteRegistry.Register(mteamAdapter)
	logger.Info("    - SiteRegistry initialized with 3 adapters (HD-Sky, ZhuQue, M-Team)")

	resourceHandler := handlers.NewResourceHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient, siteRegistry)

	// Search History Handler (if DB is available)
	var searchHistoryHandler *handlers.SearchHistoryHandler
	if deps.SearchHistoryDB != nil {
		searchHistoryHandler = handlers.NewSearchHistoryHandler(deps.Telegram, deps.SearchHistoryDB)
		logger.Info("    - SearchHistoryHandler created")
	} else {
		logger.Info("    - SearchHistoryHandler skipped (DB not available)")
	}

	// Inject dependencies
	startHandler.SetAdminService(deps.AdminService)
	startHandler.SetUserMapping(deps.UserMapping)
	startHandler.SetWeeklyReportService(deps.WeeklyReportSvc)

	// 灵魂画像服务
	if deps.Cfg.EmbyURL != "" && deps.Cfg.EmbyAPIKey != "" {
		startHandler.SetPortraitService(services.NewPortraitService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey))
	}
	backHandler.SetAdminService(deps.AdminService)
	adminHandler.SetMediaNotificationService(deps.MediaNotification)
	adminHandler.SetIssueService(deps.IssueService)
	adminHandler.SetReviewService(deps.ReviewService)
	myRequestsHandler.SetUserMapping(deps.UserMapping)
	myRequestsHandler.SetReviewService(deps.ReviewService)
	myRequestsHandler.SetQuotaService(deps.QuotaService)
	myRequestsHandler.SetWishService(deps.WishService)
	myRequestsHandler.SetAdminService(deps.AdminService)
	searchHandler.SetSearchHistory(deps.SearchHistory)
	feedbackHandler.SetIssueService(deps.IssueService)
	feedbackHandler.SetTMDBClient(deps.TMDBClient)
	feedbackHandler.SetQuotaService(deps.QuotaService)

	// Register callbacks
	registry.RegisterFunc(callback.ActionStart, startHandler.Handle)
	registry.RegisterFunc(callback.ActionSearch, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionSettings, startHandler.Handle)
	registry.RegisterFunc(callback.ActionHelpTopic, startHandler.Handle)
	registry.RegisterFunc("start_settings", startHandler.Handle)
	if wishHandler != nil {
		registry.RegisterFunc("wish", wishHandler.HandleEntry)
		registry.RegisterFunc("wish_cancel", wishHandler.HandleCancel)
	}
	registry.RegisterFunc("myreq_cancel", myRequestsHandler.HandleCancelReview) // 用户撤回 pending 求片申请
	registry.RegisterFunc(callback.ActionDetail, detailHandler.Handle)
	registry.RegisterFunc(callback.ActionDetailSeasons, detailHandler.Handle)
	registry.RegisterFunc(callback.ActionRequest, requestHandler.Handle)
	registry.RegisterFunc(callback.ActionPage, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionSelect, searchHandler.Handle)
	registry.RegisterFunc(callback.ActionBack, backHandler.Handle)
	registry.RegisterFunc(callback.ActionCancel, cancelHandler.Handle)
	registry.RegisterFunc(callback.ActionRequests, myRequestsHandler.Handle)
	registry.RegisterFunc(callback.ActionLink, linkHandler.Handle)
	registry.RegisterFunc("resetpw", linkHandler.HandleResetPW)
	registry.RegisterFunc(callback.ActionHelp, helpHandler.Handle)
	logger.Info("[initRegistry] Registering FeedbackHandler: feedbackHandler=%v", feedbackHandler != nil)
	registry.RegisterFunc(callback.ActionFeedback, feedbackHandler.Handle)
	registry.RegisterFunc("my_feedback", feedbackHandler.Handle)
	registry.RegisterFunc("portrait", startHandler.Handle)
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
	registry.RegisterFunc("admin_dashboard", adminHandler.Handle)
	registry.RegisterFunc("admin_todo", adminHandler.Handle)
	registry.RegisterFunc("admin_request_stats", adminHandler.Handle)

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

	// #3 拼车 +1 回调
	registry.RegisterFunc("carpool", carpoolHandler.Handle)

	// #6 许愿池：出源喜报「立即求片」回调（仅在处理器就绪时注册）。
	if wishHandler != nil {
		registry.RegisterFunc("wish_request", wishHandler.Handle)
		// #1 搜索无结果「🌟 加入许愿池」回调（片名从 session 取，回调串无超长参数）。
		registry.RegisterFunc("wish_add", wishHandler.HandleAddFromSearch)
	}

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
		logger.Info("    - Search History callbacks registered")
	}

	// Weekly Report callbacks
	registry.RegisterFunc("weekly_report", startHandler.Handle)
	registry.RegisterFunc("weekly_report_send", startHandler.Handle)

	// Admin Feedback Panel callbacks
	registry.RegisterFunc("admin_feedback", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_stats", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_list", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_filter", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_detail", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_reply", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_priority", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_template", adminHandler.Handle)
	registry.RegisterFunc("admin_feedback_priority_menu", adminHandler.Handle)

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

	// 通知设置回调
	notifySettingsHandler := handlers.NewNotificationSettingsHandler(deps.Preferences)
	registry.RegisterFunc("notify_settings", notifySettingsHandler.Handle)
	registry.RegisterFunc("notify_toggle", notifySettingsHandler.HandleToggle)

	logger.Info("✅ Callback handlers registered")

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
		CarpoolService:    deps.CarpoolService,
		WishService:       deps.WishService,
		WishScheduler:     deps.WishScheduler,
		WishHandler:       wishHandler,
		MyRequestsHandler: myRequestsHandler,
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
		{Command: "wish", Description: "🌟 许愿求片（无源片众筹）"},
		{Command: "link", Description: "🔗 绑定账号"},
		{Command: "quota", Description: "💎 查看配额"},
		{Command: "help", Description: "❓ 帮助中心"},
	}
	if err := telegram.SetMyCommands(commands, ""); err != nil {
		logger.Info("⚠️  Failed to set bot commands: %v", err)
	} else {
		logger.Info("✅ Bot command menu set")
	}
}

// setupWebhook configures the Telegram webhook
func setupWebhook(telegram *services.TelegramClient, cfg *config.Config) {
	if cfg.WebhookURL != "" {
		if err := telegram.SetWebhook(cfg.WebhookURL); err != nil {
			logger.Info("⚠️  Failed to set webhook: %v", err)
		} else {
			logger.Info("✅ Webhook set: %s", cfg.WebhookURL)
		}
	} else {
		logger.Info("⚠️  No webhook URL configured, deleting webhook to enable polling")
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
		WishHandler:     deps.WishHandler,
		MyRequests:      deps.MyRequestsHandler,
	}
}
