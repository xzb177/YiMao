package main

import (
	"context"
	"fmt"
	"html"
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

	// Load configuration. --check-config optionally accepts a dotenv path and
	// exits before any service, network client, or background worker starts.
	checkConfig := len(os.Args) > 1 && os.Args[1] == "--check-config"
	if checkConfig && len(os.Args) > 2 {
		if err := config.LoadEnvFile(os.Args[2]); err != nil {
			log.Fatalf("Failed to load env file: %v", err)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if checkConfig {
		log.Println("✅ Configuration is valid")
		return
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
	if deps.Notification != nil {
		deps.Notification.StartNotificationWorker()
	}
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
	securityService.SetAPIKeys(cfg.APIKeys)
	securityService.EnableAPIAuth(cfg.EnableAPIAuth)
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
		RequestSubmission: depsWithHandlers.RequestSubmission,
		TMDB:              depsWithHandlers.TMDBClient,
		Reviews:           depsWithHandlers.ReviewService,
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
	if depsWithHandlers.Notification != nil {
		depsWithHandlers.Notification.StopNotificationWorker()
	}
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
	// 关闭 GameHandler (SocialDB)
	if depsWithHandlers.GameHandler != nil {
		depsWithHandlers.GameHandler.Close()
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
	CarpoolService    *services.CarpoolService          // #3 拼车 +1 服务
	WishService       *services.WishService             // #6 许愿池存储
	WishScheduler     *services.WishScheduler           // #6 许愿池 DailyRescan task
	FulfillmentStats  *services.FulfillmentStatsService // 履约统计（ETA + 入库回访）
	SeasonRadar       *services.SeasonRadarService      // 剧集续季雷达
	WishHandler       *handlers.WishHandler             // #6 许愿池命令/回调处理器
	RequestSubmission *services.RequestSubmissionService
	MyRequestsHandler *handlers.MyRequestsHandler // 求片进度聚合视图（/requests 命令复用）
	GameHandler       *handlers.GameHandler       // 游戏化功能处理器
	AdventureHandler  *handlers.AdventureHandler  // 电影冒险
	RankHandler       *handlers.RankHandler       // 冒险排行
	StatsHandler      *handlers.StatsHandler      // 个人冒险战绩
	DreamHandler      *handlers.DreamHandler      // 本周挑战
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
	logger.Info("    - FulfillmentStatsService...")
	fulfillmentStats := services.NewFulfillmentStatsService(cfg.DataDir)
	reviewService.Fulfillment = fulfillmentStats
	reviewService.OnFulfillmentComplete = func(requestID string, telegramID int64, title string, year int, mediaType string, completedAt time.Time) {
		fulfillmentStats.AddCompletion(services.CompletionRecord{
			RequestID: requestID, TelegramID: telegramID, Title: title,
			Year: year, MediaType: mediaType, CompletedAt: completedAt,
		})
	}
	logger.Info("    - SeasonRadarService...")
	seasonRadar := services.NewSeasonRadarService(cfg.DataDir, nil)
	seasonRadar.SetEnabled(func(userID int64) bool {
		return preferencesService.IsNotifyEnabled(userID, services.NotifySeason)
	})
	seasonRadar.SetNotifier(func(userID int64, tmdbID int, title string, season services.TVSeason) bool {
		if !preferencesService.IsNotifyEnabled(userID, services.NotifySeason) {
			return true
		}
		msg := services.NewMessageBuilder()
		msg.Bold("📺 追更提醒").Newline()
		msg.Newline()
		msg.Textf("《%s》出了第 %d 季", html.EscapeString(title), season.SeasonNumber).Newline()
		if season.AirDate != "" {
			msg.Textf("开播日期：%s", season.AirDate).Newline()
		}
		msg.Newline()
		msg.Text("想继续追的话，可以直接提交这一季的求片申请。")
		kb := services.NewKeyboardBuilder()
		kb.AddButton("📥 求第 "+strconv.Itoa(season.SeasonNumber)+" 季", fmt.Sprintf("request:id:%d:type:tv:season:%d", tmdbID, season.SeasonNumber))
		kb.NewRow()
		kb.AddButton("📊 求片进度", "requests")
		if _, err := telegramClient.SendMessage(userID, msg.Build(), "HTML", kb.Build()); err != nil {
			logger.Info("[SeasonRadar] 续季通知发送失败: user=%d tmdb=%d err=%v", userID, tmdbID, err)
			return false
		}
		return true
	})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[SeasonRadar] routine panic: %v", r)
			}
		}()
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		<-timer.C
		seasonRadar.Scan()
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			seasonRadar.Scan()
		}
	}()

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
		msg.Textf("%s 《%s》%s", mediaEmoji, html.EscapeString(title), yearStr).Newline()
		msg.Newline()
		msg.Text("🍿 快去 Emby 观看吧～")

		kb := services.NewKeyboardBuilder()
		kb.AddButton("📊 求片进度", "my_requests")
		if _, err := telegramClient.SendMessage(telegramID, msg.Build(), "HTML", kb.Build()); err != nil {
			logger.Info("[ReviewService] 完成通知发送失败: user=%d err=%v", telegramID, err)
		}
		logger.Info("[ReviewService] 已通知用户 %d: %s%s 订阅完成", telegramID, title, yearStr)
	}
	// P1 中间态：开始下载（用户可关，走同一 NotifyDownload 偏好）
	reviewService.OnDownloadStart = func(telegramID int64, title string, year int, mediaType string) {
		if !preferencesService.IsNotifyEnabled(telegramID, services.NotifyDownload) {
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
		msg.Bold("⬇️ 开始下载").Newline()
		msg.Newline()
		msg.Textf("%s 《%s》%s", mediaEmoji, html.EscapeString(title), yearStr).Newline()
		msg.Newline()
		msg.Text("找到资源了，正在下载，入库后会再通知你")
		kb := services.NewKeyboardBuilder()
		kb.AddButton("📊 求片进度", "my_requests")
		if _, err := telegramClient.SendMessage(telegramID, msg.Build(), "HTML", kb.Build()); err != nil {
			logger.Info("[ReviewService] 开始下载通知发送失败: user=%d err=%v", telegramID, err)
		}
		logger.Info("[ReviewService] 已通知用户 %d: %s%s 开始下载", telegramID, title, yearStr)
	}
	// P1 中间态：暂未找到资源，转入持续搜索（预期管理，用户可关）
	reviewService.OnSearchStall = func(telegramID int64, title string, year int, mediaType string) {
		if !preferencesService.IsNotifyEnabled(telegramID, services.NotifyDownload) {
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
		msg.Bold("🔍 暂时没找到资源").Newline()
		msg.Newline()
		msg.Textf("%s 《%s》%s", mediaEmoji, html.EscapeString(title), yearStr).Newline()
		msg.Newline()
		msg.Text("已转入持续搜索，出了资源会自动下载，不用重新求片")
		if _, err := telegramClient.SendMessage(telegramID, msg.Build(), "HTML", nil); err != nil {
			logger.Info("[ReviewService] 持续搜索通知发送失败: user=%d err=%v", telegramID, err)
		}
		logger.Info("[ReviewService] 已通知用户 %d: %s%s 暂未找到资源", telegramID, title, yearStr)
	}
	// 入库回访：完成 3 天后询问是否看过，按钮回答写入本地统计。
	reviewService.OnWatchFollowup = func(telegramID int64, requestID, title string) bool {
		if !preferencesService.IsNotifyEnabled(telegramID, services.NotifyDownload) {
			// 用户明确关闭此类通知，视为已处理，避免每小时反复扫描。
			return true
		}
		msg := services.NewMessageBuilder()
		msg.Bold("🍿 片子已经到库几天啦").Newline()
		msg.Newline()
		msg.Textf("《%s》看完了吗？你的反馈会帮助我以后推荐得更准。", html.EscapeString(title)).Newline()
		kb := services.NewKeyboardBuilder()
		kb.AddButton("🎉 看完了", fmt.Sprintf("watch_fb:id:%s:a:w", requestID))
		kb.AddButton("🍿 还没看", fmt.Sprintf("watch_fb:id:%s:a:l", requestID))
		kb.NewRow()
		kb.AddButton("👌 不想看了", fmt.Sprintf("watch_fb:id:%s:a:d", requestID))
		if _, err := telegramClient.SendMessage(telegramID, msg.Build(), "HTML", kb.Build()); err != nil {
			logger.Info("[ReviewService] 入库回访发送失败: user=%d request=%s err=%v", telegramID, requestID, err)
			return false
		}
		logger.Info("[ReviewService] 已发送入库回访: user=%d request=%s", telegramID, requestID)
		return true
	}
	// B4: stuck 告警 — 审核通过但 MP 提交失败时通知管理员
	reviewService.Alert = func(requestID, title string, retryCount int, lastError string) {
		if lastError == "自动重试成功" {
			// 自动重试成功：用户会通过 OnSubscriptionComplete 收到通知，这里仅记日志
			logger.Info("[Alert] 求片自动重试成功: %s, 请求 %s, 第 %d 次", title, requestID, retryCount)
			return
		}
		if alertService == nil {
			logger.Info("[Alert] review_stuck 未发送：没有可用管理员告警接收人，request=%s", requestID)
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
		if _, err := telegramClient.SendMessage(telegramID, message, "", nil); err != nil {
			logger.Info("[ReviewService] 每日汇总发送失败: user=%d err=%v", telegramID, err)
		}
	}
	logger.Info("    - CarpoolService...")
	carpoolService := services.NewCarpoolService(cfg.DataDir) // #3 拼车 +1 持久化服务
	logger.Info("  [2/11] Basic services created")

	// Initialize Media Notification Service
	mediaNotificationSvc := services.NewMediaNotificationService(cfg.DataDir, telegramClient, adminService, chatID, moviepilotClient)
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
	seasonRadar.SetTMDB(tmdbClient)
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

	// 预热 MoviePilot 订阅缓存（后台执行，避免首次打开「求片进度」超时）
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
		FulfillmentStats:  fulfillmentStats,
		SeasonRadar:       seasonRadar,
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
	cancelHandler := handlers.NewCancelHandler(deps.SessionMgr)
	requestHandler := handlers.NewRequestHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient, deps.AdminService, deps.WebhookService, deps.UserMapping, deps.QuotaService, deps.ReviewService)
	requestHandler.SetFulfillmentStats(deps.FulfillmentStats)
	requestHandler.SetSeasonRadar(deps.SeasonRadar)
	submissionService := services.NewRequestSubmissionService(deps.UserMapping, deps.ReviewService, deps.QuotaService, requestHandler.NotifyAdminsForReview)
	requestHandler.SetRequestSubmissionService(submissionService)
	requestHandler.SetCarpoolService(deps.CarpoolService)
	searchHandler := handlers.NewSearchHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.TMDBClient)
	requestHeatHandler := handlers.NewRequestHeatHandler(services.NewRequestHeatService(deps.ReviewService, deps.CarpoolService))
	if deps.SearchHistoryDB != nil {
		searchHandler.SetSearchHistoryDB(deps.SearchHistoryDB)
	}
	myRequestsHandler := handlers.NewMyRequestsHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot)
	linkHandler := handlers.NewLinkHandler(deps.Cfg, deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.UserMapping, deps.BindingRequest)
	helpHandler := handlers.NewHelpHandler()
	adminHandler := handlers.NewAdminHandler(deps.Cfg, deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.AdminService, deps.QuotaService)
	adminHandler.SetFulfillmentStats(deps.FulfillmentStats)
	seriesHandler := handlers.NewSeriesHandler(deps.TMDBClient, deps.MoviePilot, deps.WebhookService)
	requestHandler.SetSeriesHandler(seriesHandler)
	watchFollowupHandler := handlers.NewWatchFollowupHandler(deps.FulfillmentStats, deps.ReviewService)
	reviewHandler := handlers.NewReviewHandler(deps.SessionMgr, deps.Telegram, deps.MoviePilot, deps.AdminService, deps.ReviewService, deps.QuotaService, deps.WebhookService, groupChatID)
	feedbackHandler := handlers.NewFeedbackHandler(deps.SessionMgr, deps.Telegram, deps.AdminService)
	washHandler := handlers.NewWashHandler(deps.ReviewService, deps.TMDBClient, deps.WebhookService, requestHandler.NotifyAdminsForReview, deps.SessionMgr)

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
		wishHandler.SetRequestSubmissionService(submissionService)
		wishHandler.SetCarpoolService(deps.CarpoolService)
		wishHandler.SetSearchHistoryService(deps.SearchHistory)
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

	// 观影画像服务
	if deps.Cfg.EmbyURL != "" && deps.Cfg.EmbyAPIKey != "" {
		startHandler.SetPortraitService(services.NewPortraitService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey))
	}

	// ====== 游戏化服务 ======
	rankSvc := services.NewRankService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey)
	personalitySvc := services.NewPersonalityService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey)
	narratorSvc := services.NewNarratorService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey, deps.Cfg.OpenAIAPIKey, deps.Cfg.OpenAIBaseURL, deps.Cfg.OpenAIModel)
	blindBoxSvc := services.NewBlindBoxService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey, deps.Cfg.TMDBAPIKey)
	rouletteSvc := services.NewRouletteService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey, deps.Cfg.TMDBAPIKey)
	socialDB, socialErr := services.NewSocialDB(deps.Cfg.DataDir)
	if socialErr != nil {
		logger.Info("[initRegistry] ⚠️ SocialDB init failed: %v", socialErr)
	}
	emotionSvc := services.NewEmotionTimelineService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey, deps.Cfg.OpenAIAPIKey, deps.Cfg.OpenAIBaseURL, deps.Cfg.OpenAIModel)
	// 电影冒险服务
	adventureSvc := services.NewAdventureService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey, deps.Cfg.TMDBAPIKey, deps.Cfg.OpenAIAPIKey, deps.Cfg.OpenAIBaseURL, deps.Cfg.OpenAIModel)
	adventureHandler := handlers.NewAdventureHandler(adventureSvc, deps.TMDBClient, deps.SessionMgr, deps.Telegram, deps.UserMapping, groupChatID)
	if socialDB != nil {
		adventureHandler.SetSocialDB(socialDB)
		// 自动生成本周挑战（如果没有的话）
		if deps.Cfg.TMDBAPIKey != "" {
			go socialDB.AutoGenerateWeeklyBoss(deps.Cfg.TMDBAPIKey)
		}
		// 定期清理过期的冒险会话（每30分钟）
		go func() {
			ticker := time.NewTicker(30 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				if cleaned, err := socialDB.CleanStaleAdventureSessions(); err == nil && cleaned > 0 {
					logger.Info("[Adventure] 清理过期冒险会话: %d 条", cleaned)
				}
			}
		}()
		// 回归钩子：每 6 小时检查超 48h 未冒险的用户，带 per-user 24h 冷却
		if deps.Telegram != nil {
			go func() {
				ticker := time.NewTicker(6 * time.Hour)
				defer ticker.Stop()
				lastReminded := make(map[int64]time.Time) // per-user 冷却
				for range ticker.C {
					inactiveUsers, err := socialDB.GetInactiveUsersWithNemesis()
					if err != nil || len(inactiveUsers) == 0 {
						continue
					}
					var reminded int
					for _, uid := range inactiveUsers {
						// 24h 冷却：同用户不重复推送
						if last, ok := lastReminded[uid]; ok && time.Since(last) < 24*time.Hour {
							continue
						}
						userName := "冒险者"
						if deps.UserMapping != nil {
							if name, err2 := deps.UserMapping.GetMoviePilotUsername(uid); err2 == nil && name != "" {
								userName = name
							}
						}
						// 文案变化：根据冷却状态调整语气
						var msg string
						if _, remindedBefore := lastReminded[uid]; remindedBefore {
							msg = fmt.Sprintf("⚔️ %s，你的冒险记录仍在周榜上。\n\n有空时，欢迎回来继续挑战。\n\n/go 开始冒险", userName)
						} else {
							msg = fmt.Sprintf("⚔️ %s，好久不见。\n\n上次的冒险记录还为你保留着；想继续时，随时回来。\n\n/go 开始冒险", userName)
						}
						deps.Telegram.SendMessage(uid, msg, "", nil)
						lastReminded[uid] = time.Now()
						reminded++
					}
					if reminded > 0 {
						logger.Info("[ReturnHook] 推送召回 %d 位冒险者 (跳过 %d 位冷却中)", reminded, len(inactiveUsers)-reminded)
					}
				}
			}()
		}
	}
	// 注入盲盒服务（用于通关奖励）
	if blindBoxSvc != nil {
		adventureHandler.SetBlindBoxService(blindBoxSvc)
	}
	// 注入观影历史服务（用于个性化推荐）
	viewingSvc := services.NewViewingHistoryService(deps.Cfg.EmbyURL, deps.Cfg.EmbyAPIKey)
	adventureHandler.SetViewingHistoryService(viewingSvc)
	// 冒险通关 → 自动提交求片请求（不消耗配额，冒险本身就是代价）
	adventureHandler.SetOnAdventureSuccess(func(userID int64, chatID int64, movieName string, movieYear int, tmdbID int, mediaType string, genres []string, score int, grade string) {
		if deps.ReviewService == nil {
			return
		}
		userName := ""
		if deps.UserMapping != nil {
			if name, err := deps.UserMapping.GetMoviePilotUsername(userID); err == nil && name != "" {
				userName = name
			}
		}
		if userName == "" {
			userName = fmt.Sprintf("用户%d", userID)
		}
		requestMediaType := services.MediaTypeMovie
		if mediaType == "tv" {
			requestMediaType = services.MediaTypeTV
		}
		_, err := submissionService.Submit(services.RequestSubmission{
			TelegramID: userID, TelegramName: userName, TmdbID: tmdbID, MediaTitle: movieName,
			MediaYear: movieYear, MediaType: requestMediaType, Priority: "high",
			Origin: "adventure", AdventureScore: score, AdventureGrade: grade, UseQuota: false,
		})
		if err != nil {
			logger.Info("[Adventure] 求片提交失败: %v", err)
			deps.Telegram.SendMessage(chatID, "❌ 求片自动提交失败，请返回主菜单选择「搜索求片」重试", "", nil)
			return
		}
		logger.Info("[Adventure] 冒险通关自动提交求片: %s (%d), 用户 %d, 评级 %s", movieName, movieYear, userID, grade)
	})
	gameHandler := handlers.NewGameHandler(rankSvc, personalitySvc, narratorSvc, blindBoxSvc, socialDB, rouletteSvc, deps.UserMapping, deps.Telegram, deps.SessionMgr, emotionSvc, adventureHandler, groupChatID)
	gameHandler.SetViewingHistoryService(viewingSvc)
	logger.Info("[initRegistry] Game services initialized")

	// 新冒险功能 handlers
	rankHandler := handlers.NewRankHandler(socialDB, deps.Telegram, deps.UserMapping, groupChatID)
	statsHandler := handlers.NewStatsHandler(socialDB, deps.Telegram, deps.UserMapping)
	dreamHandler := handlers.NewDreamHandler(socialDB, deps.Telegram, adventureHandler, deps.UserMapping)
	backHandler.SetAdminService(deps.AdminService)
	adminHandler.SetMediaNotificationService(deps.MediaNotification)
	adminHandler.SetIssueService(deps.IssueService)
	adminHandler.SetReviewService(deps.ReviewService)
	myRequestsHandler.SetUserMapping(deps.UserMapping)
	myRequestsHandler.SetReviewService(deps.ReviewService)
	myRequestsHandler.SetIssueService(deps.IssueService)
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
	registry.RegisterFunc(callback.ActionRequestHeat, requestHeatHandler.Handle)
	// Legacy start_ai buttons now land on the request-focused search entry.
	registry.RegisterFunc(callback.ActionAI, searchHandler.Handle)
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
	registry.RegisterFunc("my_requests", myRequestsHandler.Handle)
	registry.RegisterFunc("series_view", seriesHandler.Handle)
	registry.RegisterFunc("watch_fb", watchFollowupHandler.Handle)
	registry.RegisterFunc(callback.ActionLink, linkHandler.Handle)
	registry.RegisterFunc("unlink_confirm", linkHandler.HandleUnlinkConfirm)
	registry.RegisterFunc("resetpw", linkHandler.HandleResetPW)
	registry.RegisterFunc(callback.ActionHelp, helpHandler.Handle)
	logger.Info("[initRegistry] Registering FeedbackHandler: feedbackHandler=%v", feedbackHandler != nil)
	registry.RegisterFunc(callback.ActionFeedback, feedbackHandler.Handle)
	registry.RegisterFunc("issue", feedbackHandler.Handle)
	registry.RegisterFunc("wash", washHandler.Handle)
	registry.RegisterFunc("my_feedback", feedbackHandler.Handle)
	registry.RegisterFunc("portrait", startHandler.Handle)
	logger.Info("[initRegistry] portrait callback registered → startHandler.Handle")

	// 游戏化功能回调
	registry.RegisterFunc("game_menu", gameHandler.Handle)
	registry.RegisterFunc("game_rank", gameHandler.Handle)
	registry.RegisterFunc("game_personality", gameHandler.Handle)
	registry.RegisterFunc("game_narrator", gameHandler.Handle)
	registry.RegisterFunc("game_narrate", gameHandler.Handle)
	registry.RegisterFunc("game_blindbox", gameHandler.Handle)
	registry.RegisterFunc("game_blindbox_open", gameHandler.Handle)
	registry.RegisterFunc("game_social", gameHandler.Handle)
	registry.RegisterFunc("game_review", gameHandler.Handle)
	registry.RegisterFunc("game_review_rate", gameHandler.Handle)
	registry.RegisterFunc("game_roulette", gameHandler.Handle)
	registry.RegisterFunc("game_roulette_spin", gameHandler.Handle)
	registry.RegisterFunc("game_emotion", gameHandler.Handle)
	registry.RegisterFunc("game_time_machine", gameHandler.Handle)
	registry.RegisterFunc("game_prescription", gameHandler.Handle)
	registry.RegisterFunc("game_contract", gameHandler.Handle)
	registry.RegisterFunc("game_blindbox_horror", gameHandler.Handle)
	registry.RegisterFunc("game_blindbox_personality", gameHandler.Handle)
	registry.RegisterFunc("game_contract_complete", gameHandler.Handle)
	registry.RegisterFunc("game_compare", gameHandler.Handle)
	registry.RegisterFunc("game_daily_challenge", gameHandler.Handle)
	registry.RegisterFunc("game_daily_complete", gameHandler.Handle)
	registry.RegisterFunc("game_achievements", gameHandler.Handle)
	// 电影冒险回调
	registry.RegisterFunc("adventure_start", adventureHandler.Handle)
	registry.RegisterFunc("adventure_choice", adventureHandler.Handle)
	registry.RegisterFunc("adventure_hint", adventureHandler.Handle) // 🎬 问导演
	registry.RegisterFunc("adventure_retry", adventureHandler.Handle)
	registry.RegisterFunc("adventure_quit", adventureHandler.Handle)
	registry.RegisterFunc("adventure_share", adventureHandler.Handle)         // 📢 分享战绩
	registry.RegisterFunc("adventure_revive", adventureHandler.Handle)        // 🩸 每日免费复活
	registry.RegisterFunc("adventure_gamble", adventureHandler.Handle)        // 🎰 双倍或归零
	registry.RegisterFunc("adventure_gamble_safe", adventureHandler.Handle)   // 📦 安全领取
	registry.RegisterFunc("adventure_gamble_triple", adventureHandler.Handle) // 尝试三倍奖励
	registry.RegisterFunc("game_adventure_stats", gameHandler.Handle)
	registry.RegisterFunc("game_adventure_rank", gameHandler.Handle)
	logger.Info("[initRegistry] Game callbacks registered (27+5 actions)")
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
	registry.RegisterFunc("review_complete_wash", reviewHandler.Handle)
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
		FulfillmentStats:  deps.FulfillmentStats,
		SeasonRadar:       deps.SeasonRadar,
		WishHandler:       wishHandler,
		RequestSubmission: submissionService,
		MyRequestsHandler: myRequestsHandler,
		GameHandler:       gameHandler,
		AdventureHandler:  adventureHandler,
		RankHandler:       rankHandler,
		StatsHandler:      statsHandler,
		DreamHandler:      dreamHandler,
	}

	return registry, resultDeps
}

// setupBotCommands sets up the bot command menu
func setupBotCommands(telegram *services.TelegramClient) {
	privateCommands := []services.BotCommand{
		{Command: "start", Description: "🌟 打开主菜单"},
		{Command: "search", Description: "🔍 搜索求片"},
		{Command: "adventure", Description: "⚔️ 电影冒险"},
		{Command: "requests", Description: "📊 求片进度"},
		{Command: "wish", Description: "✨ 许愿求片（无源片众筹）"},
		{Command: "link", Description: "🔗 绑定账号"},
		{Command: "quota", Description: "💎 查看配额"},
		{Command: "portrait", Description: "🧠 观影画像"},
		{Command: "game", Description: "🎮 游戏中心"},
		{Command: "narrate", Description: "🎬 AI 电影解说"},
		{Command: "review", Description: "✍️ 写影评"},
		{Command: "rank", Description: "📊 冒险排行"},
		{Command: "mystats", Description: "📈 我的战绩"},
		{Command: "go", Description: "⚔️ 开始冒险"},
		{Command: "dream", Description: "🎯 本周挑战"},
		{Command: "help", Description: "❓ 帮助中心"},
	}
	if err := telegram.SetMyCommandsForScope(privateCommands, "", map[string]interface{}{"type": "all_private_chats"}); err != nil {
		logger.Info("⚠️  Failed to set private bot commands: %v", err)
	} else {
		logger.Info("✅ Private bot command menu set")
	}

	// Group and Community chats expose only privacy-safe entries. Every command
	// in this scope is ephemeral; credential-bearing and free-form commands such
	// as /link, /review and /narrate remain private-chat only.
	groupCommands := []services.BotCommand{
		{Command: "start", Description: "🌟 私密主菜单", IsEphemeral: true},
		{Command: "search", Description: "🔍 私密搜索求片", IsEphemeral: true},
		{Command: "adventure", Description: "⚔️ 私密电影冒险", IsEphemeral: true},
		{Command: "requests", Description: "📊 私密求片进度", IsEphemeral: true},
		{Command: "wish", Description: "✨ 私密许愿", IsEphemeral: true},
		{Command: "quota", Description: "💎 私密查看配额", IsEphemeral: true},
		{Command: "portrait", Description: "🧠 私密观影画像", IsEphemeral: true},
		{Command: "game", Description: "🎮 私密游戏中心", IsEphemeral: true},
		{Command: "go", Description: "⚔️ 私密开始冒险", IsEphemeral: true},
	}
	if err := telegram.SetMyCommandsForScope(groupCommands, "", map[string]interface{}{"type": "all_group_chats"}); err != nil {
		logger.Info("⚠️  Failed to set group/community bot commands: %v", err)
	} else {
		logger.Info("✅ Group/community ephemeral command menu set")
	}
}

// setupWebhook configures the Telegram webhook
func setupWebhook(telegram *services.TelegramClient, cfg *config.Config) {
	if cfg.WebhookURL != "" {
		if cfg.TelegramWebhookSecret == "" {
			logger.Warn("⚠️  TELEGRAM_WEBHOOK_SECRET is empty; Telegram webhook disabled (polling unaffected)")
			return
		}
		if err := telegram.SetWebhookWithSecret(cfg.WebhookURL, cfg.TelegramWebhookSecret); err != nil {
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
		Telegram:         deps.Telegram,
		MoviePilot:       deps.MoviePilot,
		SessionMgr:       deps.SessionMgr,
		UserMapping:      deps.UserMapping,
		BindingRequest:   deps.BindingRequest,
		AdminService:     deps.AdminService,
		AdminHandler:     deps.AdminHandler,
		QuotaService:     deps.QuotaService,
		SearchHistory:    deps.SearchHistory,
		SearchHistoryDB:  deps.SearchHistoryDB,
		TMDB:             deps.TMDBClient,
		IssueService:     deps.IssueService,
		FeedbackHandler:  deps.FeedbackHandler,
		WishHandler:      deps.WishHandler,
		MyRequests:       deps.MyRequestsHandler,
		GameHandler:      deps.GameHandler,
		AdventureHandler: deps.AdventureHandler,
		RankHandler:      deps.RankHandler,
		StatsHandler:     deps.StatsHandler,
		DreamHandler:     deps.DreamHandler,
	}
}
