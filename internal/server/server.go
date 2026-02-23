package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"emby-telegram-bot/internal/api"
	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	botHandlers "emby-telegram-bot/internal/bot"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
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
	ChatService       *services.ChatService
	WebhookService    *services.WebhookService
	BindingRequest    *services.BindingRequestService
	MediaNotification *services.MediaNotificationService
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

	// Test summary handler
	testSummaryHandler := func(d *Dependencies) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			// Get admin ID from query param
			adminIDStr := r.URL.Query().Get("admin_id")
			if adminIDStr == "" {
				// Use first admin if not specified
				adminIDs := d.AdminService.GetAdminIDs()
				if len(adminIDs) == 0 {
					http.Error(w, "No admins configured", http.StatusBadRequest)
					return
				}
				adminIDStr = fmt.Sprintf("%d", adminIDs[0])
			}

			var adminID int64
			_, err := fmt.Sscanf(adminIDStr, "%d", &adminID)
			if err != nil || adminID == 0 {
				http.Error(w, "Invalid admin_id", http.StatusBadRequest)
				return
			}

			// Trigger manual summary
			err = d.MediaNotification.SendManualSummary(adminID, time.Now())
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"status": "error", "message": "%s"}`, err.Error())
				return
			}

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status": "success", "admin_id": %d}`, adminID)
		}
	}

	// Test add item handler (for testing only)
	testAddItemHandler := func(d *Dependencies) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			// Parse MediaItem from request body
			var item services.MediaItem
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}

			// Add to notification service
			d.MediaNotification.AddItem(&item)

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status": "success", "title": "%s"}`, item.Title)
		}
	}

	// Register test routes
	if cfg.EnableAPIAuth {
		mux.HandleFunc("/api/test-summary", securityService.Middleware(testSummaryHandler(deps)))
		mux.HandleFunc("/api/test-add-item", securityService.Middleware(testAddItemHandler(deps)))
	} else {
		mux.HandleFunc("/api/test-summary", securityService.PublicMiddleware(testSummaryHandler(deps)))
		mux.HandleFunc("/api/test-add-item", securityService.PublicMiddleware(testAddItemHandler(deps)))
	}

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
		ChatService:    deps.ChatService,
	}
}
