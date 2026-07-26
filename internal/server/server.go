package server

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/xzb177/yimao/internal/api"
	botHandlers "github.com/xzb177/yimao/internal/bot"
	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/handlers"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
)

// Dependencies holds all service dependencies for the server
type Dependencies struct {
	Telegram          *services.TelegramClient
	MoviePilot        *services.MoviePilotClient
	SessionMgr        *session.Manager
	UserMapping       services.UserMappingStore
	Preferences       *services.PreferencesService
	IssueService      *services.IssueService
	AdminService      *services.AdminService
	QuotaService      *services.QuotaService
	WebhookService    *services.WebhookService
	BindingRequest    *services.BindingRequestService
	MediaNotification *services.MediaNotificationService
	FeedbackHandler   *handlers.FeedbackHandler
	WishHandler       *handlers.WishHandler // #6 许愿池命令/回调处理器
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
		w.Header().Set("Content-Type", "application/json")
		status := "ok"
		checks := make(map[string]string)

		// Check MoviePilot connectivity
		if cfg.MoviePilotURL != "" {
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(cfg.MoviePilotURL + "/api/v1/system/setting/APP")
			if err != nil {
				checks["moviepilot"] = "unreachable"
				status = "degraded"
			} else {
				resp.Body.Close()
				if resp.StatusCode == 200 || resp.StatusCode == 401 || resp.StatusCode == 403 {
					checks["moviepilot"] = "ok"
				} else {
					checks["moviepilot"] = fmt.Sprintf("status_%d", resp.StatusCode)
					status = "degraded"
				}
			}
		}

		// Check Emby connectivity
		if cfg.EmbyURL != "" && cfg.EmbyAPIKey != "" {
			client := &http.Client{Timeout: 3 * time.Second}
			req, _ := http.NewRequest("GET", cfg.EmbyURL+"/System/Info", nil)
			req.Header.Set("X-Emby-Token", cfg.EmbyAPIKey)
			resp, err := client.Do(req)
			if err != nil {
				checks["emby"] = "unreachable"
				status = "degraded"
			} else {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					checks["emby"] = "ok"
				} else {
					checks["emby"] = fmt.Sprintf("status_%d", resp.StatusCode)
					status = "degraded"
				}
			}
		}

		code := http.StatusOK
		if status != "ok" {
			code = http.StatusServiceUnavailable
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"status":"%s","dependencies":{`, status)
		first := true
		for name, dependencyStatus := range checks {
			if !first {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `%q:%q`, name, dependencyStatus)
			first = false
		}
		fmt.Fprint(w, `}}`)
	}))

	// Debug endpoint — always requires API auth (no public fallback)
	var debugHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		stats := deps.SessionMgr.Stats()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sessions": %d, "total_size": %d}`,
			stats["total_sessions"], stats["total_size"])
	}
	if cfg.EnableAPIAuth {
		mux.HandleFunc("/debug", securityService.Middleware(debugHandler))
	} else {
		// When API auth is disabled, restrict to localhost only.
		mux.HandleFunc("/debug", securityService.PublicMiddleware(localOnlyHandler(debugHandler)))
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

	// Summary requires API authentication when configured; otherwise it is localhost-only.
	if cfg.EnableAPIAuth {
		mux.HandleFunc("/api/summary", securityService.Middleware(apiRouter.HandleSummary))
	} else {
		mux.HandleFunc("/api/summary", securityService.PublicMiddleware(localOnlyHandler(apiRouter.HandleSummary)))
	}
	mux.HandleFunc("/webhook/emby", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/jellyseerr", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/moviepilot", securityService.PublicMiddleware(apiRouter.HandleWebhook))
	mux.HandleFunc("/webhook/mp", securityService.PublicMiddleware(apiRouter.HandleWebhook))

	// Register additional API routes. These are management endpoints, NOT
	// webhooks: without API auth they must stay localhost-only (the previous
	// fallback exposed them publicly with only IP rate limiting).
	var apiHandler http.HandlerFunc = apiRouter.HandleWebhook
	if cfg.EnableAPIAuth {
		mux.HandleFunc("/api/stats", securityService.Middleware(apiHandler))
		mux.HandleFunc("/api/admins", securityService.Middleware(apiHandler))
		mux.HandleFunc("/api/admins/", securityService.Middleware(apiHandler))
	} else {
		mux.HandleFunc("/api/stats", securityService.PublicMiddleware(localOnlyHandler(apiHandler)))
		mux.HandleFunc("/api/admins", securityService.PublicMiddleware(localOnlyHandler(apiHandler)))
		mux.HandleFunc("/api/admins/", securityService.PublicMiddleware(localOnlyHandler(apiHandler)))
	}

	return &http.Server{
		Addr:              cfg.ServerHost + ":" + cfg.ServerPort,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func localOnlyHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// toBotDeps converts server Dependencies to bot Dependencies
func toBotDeps(deps *Dependencies) *botHandlers.Dependencies {
	return &botHandlers.Dependencies{
		Telegram:        deps.Telegram,
		MoviePilot:      deps.MoviePilot,
		SessionMgr:      deps.SessionMgr,
		UserMapping:     deps.UserMapping,
		BindingRequest:  deps.BindingRequest,
		AdminService:    deps.AdminService,
		QuotaService:    deps.QuotaService,
		IssueService:    deps.IssueService,
		FeedbackHandler: deps.FeedbackHandler,
		WishHandler:     deps.WishHandler,
	}
}
