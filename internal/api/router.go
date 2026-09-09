package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

// Router handles HTTP API requests
type Router struct {
	cfg            *config.Config
	adminService   *services.AdminService
	quotaService   *services.QuotaService
	sessMgr        *session.Manager
	webhookService *services.WebhookService
}

// NewRouter creates a new API router
func NewRouter(
	cfg *config.Config,
	_ *services.TelegramClient,
	_ interface{}, // jellyseerr - deprecated, kept for compatibility
	adminService *services.AdminService,
	quotaService *services.QuotaService,
	_ services.UserMappingStore,
	_ *services.PreferencesService,
	_ *services.IssueService,
	sessMgr *session.Manager,
	_ *callback.Registry,
	webhookService *services.WebhookService,
) *Router {
	return &Router{
		cfg:            cfg,
		adminService:   adminService,
		quotaService:   quotaService,
		sessMgr:        sessMgr,
		webhookService: webhookService,
	}
}

// HandleStats serves the authenticated statistics endpoint.
func (r *Router) HandleStats(w http.ResponseWriter, req *http.Request) {
	r.handleStats(w, req)
}

// handleStats handles stats requests
func (r *Router) handleStats(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Get session stats
	sessionStats := r.sessMgr.Stats()

	// Get admin count
	adminCount := r.adminService.GetAdminCount()

	// Count users with a persisted quota record, not fields in session stats.
	totalQuotas := 0
	if r.quotaService != nil {
		totalQuotas = r.quotaService.GetUserCount()
	}

	stats := map[string]interface{}{
		"status": "ok",
		"stats": map[string]interface{}{
			"sessions":    sessionStats["total_sessions"],
			"total_size":  sessionStats["total_size"],
			"admin_count": adminCount,
			"quota_count": totalQuotas,
			"uptime":      "active",
		},
	}

	json.NewEncoder(w).Encode(stats)
}

// HandleAdmins serves the authenticated administrator collection endpoint.
func (r *Router) HandleAdmins(w http.ResponseWriter, req *http.Request) {
	r.handleAdmins(w, req)
}

// handleAdmins handles admin management requests. Until the management API has
// a cryptographically verified principal, this endpoint is deliberately
// read-only: a shared API key authenticates a client, but does not identify a
// Telegram administrator and must never be combined with a caller-supplied ID.
func (r *Router) handleAdmins(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch req.Method {
	case http.MethodGet:
		r.getAdmins(w, req)
	case http.MethodPost:
		http.Error(w, "Administrator writes are disabled: no verifiable management principal", http.StatusForbidden)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getAdmins returns all admins
func (r *Router) getAdmins(w http.ResponseWriter, req *http.Request) {
	admins := r.adminService.GetAllAdmins()

	var adminList []map[string]string
	for userID, name := range admins {
		adminList = append(adminList, map[string]string{
			"user_id": userID,
			"name":    name,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"admins": adminList,
		"count":  len(adminList),
	})
}

// HandleAdminsByID serves the administrator member endpoint. It intentionally
// exposes no mutation until the HTTP API can authenticate a Telegram principal.
func (r *Router) HandleAdminsByID(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodDelete {
		http.Error(w, "Administrator writes are disabled: use the Telegram root workflow", http.StatusForbidden)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// HandleSummary rejects the legacy HTTP management action. A shared API key
// cannot identify the Telegram administrator on whose behalf it would run.
func (r *Router) HandleSummary(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Error(w, "Management writes are disabled: use the Telegram root workflow", http.StatusForbidden)
}

// HandleWebhook handles webhook POST requests (for external services)
func (r *Router) HandleWebhook(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.cfg.WebhookSecret == "" {
		http.Error(w, "External webhook disabled", http.StatusServiceUnavailable)
		return
	}
	if req.Body == nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)

	// Webhook authentication is mandatory for external webhooks. Send either an
	// HMAC-SHA256 signature over the raw body in X-Webhook-Signature or, during
	// migration, the shared secret in X-Webhook-Token. URL query tokens are never
	// accepted because URLs are routinely logged and forwarded.
	if secret := r.cfg.WebhookSecret; secret != "" {
		body, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			http.Error(w, "Failed to read request", http.StatusBadRequest)
			return
		}
		if !verifyWebhookAuth(req, body, secret) {
			logger.Warn("[API] Rejected webhook: invalid signature/token from %s", strings.Split(req.RemoteAddr, ":")[0])
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Restore the body so downstream handlers can read it again.
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	w.Header().Set("Content-Type", "application/json")

	// Check webhook type from path or header
	webhookType := req.URL.Query().Get("type")
	if webhookType == "" {
		webhookType = req.Header.Get("X-Webhook-Type")
	}

	switch webhookType {
	case "emby":
		r.handleEmbyWebhook(w, req)
	case "jellyseerr":
		r.handleJellyseerrWebhook(w, req)
	case "moviepilot", "mp":
		r.handleMoviePilotWebhook(w, req)
	default:
		// Auto-detect based on request body
		r.handleAutoDetectWebhook(w, req)
	}
}

// verifyWebhookAuth validates an inbound webhook against the shared secret.
// HMAC-SHA256 is preferred; X-Webhook-Token is retained as a header-only
// migration path. Query-string credentials are deliberately ignored.
func verifyWebhookAuth(req *http.Request, body []byte, secret string) bool {
	sig := req.Header.Get("X-Webhook-Signature")
	if sig != "" {
		sig = strings.TrimPrefix(sig, "sha256=")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}
	token := req.Header.Get("X-Webhook-Token")
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
}

// handleEmbyWebhook handles Emby webhook
func (r *Router) handleEmbyWebhook(w http.ResponseWriter, req *http.Request) {
	// Log request for debugging
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		logger.Error("[API] Failed to read Emby webhook body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	logger.Info("[API] Emby webhook received - Content-Type: %s, Body length: %d bytes", req.Header.Get("Content-Type"), len(body))

	var payload services.EmbyWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		logger.Info("[API] Failed to decode Emby webhook: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if err := r.webhookService.HandleEmbyWebhook(payload); err != nil {
		logger.Info("[API] Failed to handle Emby webhook: %v", err)
		http.Error(w, "Failed to process", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleJellyseerrWebhook handles Jellyseerr webhook
func (r *Router) handleJellyseerrWebhook(w http.ResponseWriter, req *http.Request) {
	var payload services.JellyseerrWebhookPayload
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		logger.Info("[API] Failed to decode Jellyseerr webhook: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if err := r.webhookService.HandleJellyseerrWebhook(payload); err != nil {
		logger.Info("[API] Failed to handle Jellyseerr webhook: %v", err)
		http.Error(w, "Failed to process", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleMoviePilotWebhook handles MoviePilot webhook
func (r *Router) handleMoviePilotWebhook(w http.ResponseWriter, req *http.Request) {
	var payload services.MoviePilotWebhookPayload
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		logger.Info("[API] Failed to decode MoviePilot webhook: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if err := r.webhookService.HandleMoviePilotWebhook(payload); err != nil {
		logger.Info("[API] Failed to handle MoviePilot webhook: %v", err)
		http.Error(w, "Failed to process", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAutoDetectWebhook auto-detects webhook type
func (r *Router) handleAutoDetectWebhook(w http.ResponseWriter, req *http.Request) {
	// Read body first
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		logger.Error("[API] Failed to read request body: %v", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	if len(body) > 200 {
		logger.Debug("[API] Auto-detect webhook - Content-Type: %s, Size: %d bytes", req.Header.Get("Content-Type"), len(body))
	} else {
		logger.Debug("[API] Auto-detect webhook - Content-Type: %s, Size: %d bytes", req.Header.Get("Content-Type"), len(body))
	}

	// Try to decode as Emby first
	var embyPayload services.EmbyWebhookPayload
	if err := json.Unmarshal(body, &embyPayload); err == nil {
		// Check for Emby event (NotificationType or Event field)
		event := embyPayload.Event
		if event == "" {
			event = embyPayload.EventField
		}
		if event != "" {
			logger.Info("[API] Detected Emby webhook: %s", event)
			if err := r.webhookService.HandleEmbyWebhook(embyPayload); err != nil {
				logger.Info("[API] Failed to handle Emby webhook: %v", err)
				http.Error(w, "Failed to process", http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}
			return
		}
	}

	// Try Jellyseerr
	var jellyseerrPayload services.JellyseerrWebhookPayload
	if err := json.Unmarshal(body, &jellyseerrPayload); err == nil && jellyseerrPayload.Event != "" {
		logger.Info("[API] Detected Jellyseerr webhook: %s", jellyseerrPayload.Event)
		if err := r.webhookService.HandleJellyseerrWebhook(jellyseerrPayload); err != nil {
			logger.Info("[API] Failed to handle Jellyseerr webhook: %v", err)
			http.Error(w, "Failed to process", http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
		return
	}

	logger.Debug("[API] Unknown webhook type, Size: %d bytes", len(body))
	http.Error(w, "Unknown webhook type", http.StatusBadRequest)
}

// getRequestBody reads and returns request body
func getRequestBody(req *http.Request) ([]byte, error) {
	// Try GetBody first for re-readability
	body := req.Body
	if body == nil {
		return nil, fmt.Errorf("request body is nil")
	}

	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	body.Close()
	return data, err
}
