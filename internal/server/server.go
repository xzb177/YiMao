package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/xzb177/yimao/internal/api"
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	botHandlers "github.com/xzb177/yimao/internal/bot"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

// Dependencies holds all service dependencies for the server
type Dependencies struct {
	Telegram          *services.TelegramClient
	MoviePilot        *services.MoviePilotClient
	SessionMgr        *session.Manager
	UserMapping       *services.UserMappingService
	Preferences       *services.PreferencesService
	IssueService      *services.IssueService
	AdminService      *services.AdminService
	QuotaService      *services.QuotaService
	WebhookService    *services.WebhookService
	BindingRequest    *services.BindingRequestService
	MediaNotification *services.MediaNotificationService
	FeedbackHandler   *handlers.FeedbackHandler
}

// New creates a new HTTP server
func New(
	cfg *config.Config,
	registry *callback.Registry,
	deps *Dependencies,
	securityService *services.SecurityService,
) *http.Server {
	mux := http.NewServeMux()

	// Health check (public, no auth required)
	mux.HandleFunc("/health", securityService.PublicMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}))

	// Debug endpoint (protected with API auth if enabled)
	var debugHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		stats := deps.SessionMgr.Stats()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sessions": %d, "total_size": %d}`,
			stats["total_sessions"], stats["total_size"])
	}
	if cfg.EnableAPIAuth {
		mux.HandleFunc("/debug", securityService.Middleware(debugHandler))
	} else {
		mux.HandleFunc("/debug", securityService.PublicMiddleware(debugHandler))
	}

	// Webhook endpoints (for Telegram bot updates) - public with rate limiting
	webhookHandler := func(w http.ResponseWriter, r *http.Request) {
		botHandlers.HandleWebhook(w, r, registry, toBotDeps(deps), cfg)
	}
	mux.HandleFunc("/webhook", securityService.PublicMiddleware(webhookHandler))
	mux.HandleFunc("/telegram-webhook", securityService.PublicMiddleware(webhookHandler))

	// Initialize API router for Emby/Jellyseerr webhooks
	apiRouter := api.NewRouter(
		cfg,
		deps.Telegram,
		nil, // jellyseerr - not needed for current setup
		deps.AdminService,
		deps.QuotaService,
		deps.UserMapping,
		deps.Preferences,
		deps.IssueService,
		deps.SessionMgr,
		registry,
		deps.WebhookService,
	)

	// Register webhook endpoint for external services (Emby, Jellyseerr, MoviePilot)
	mux.HandleFunc("/api/summary", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/emby", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/jellyseerr", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/moviepilot", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/mp", securityService.PublicMiddleware(apiRouter.HandleWebhook))

	// Register additional API routes (protected with API auth if enabled)
	var apiHandler http.HandlerFunc = apiRouter.HandleWebhook
	if cfg.EnableAPIAuth {
		mux.HandleFunc("/api/stats", securityService.Middleware(apiHandler))
		mux.HandleFunc("/api/admins", securityService.Middleware(apiHandler))
		mux.HandleFunc("/api/admins/", securityService.Middleware(apiHandler))
	} else {
		mux.HandleFunc("/api/stats", securityService.PublicMiddleware(apiHandler))
		mux.HandleFunc("/api/admins", securityService.PublicMiddleware(apiHandler))
		mux.HandleFunc("/api/admins/", securityService.PublicMiddleware(apiHandler))
	}

	return &http.Server{
		Addr:         cfg.ServerHost + ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

// toBotDeps converts server Dependencies to bot Dependencies
func toBotDeps(deps *Dependencies) *botHandlers.Dependencies {
	return &botHandlers.Dependencies{
		Telegram:       deps.Telegram,
		MoviePilot:     deps.MoviePilot,
		SessionMgr:     deps.SessionMgr,
		UserMapping:    deps.UserMapping,
		BindingRequest: deps.BindingRequest,
		AdminService:   deps.AdminService,
		QuotaService:   deps.QuotaService,
		IssueService:   deps.IssueService,
		FeedbackHandler: deps.FeedbackHandler,
	}
}
