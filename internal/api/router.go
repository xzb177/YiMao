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
	"strconv"
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
	telegram       *services.TelegramClient
	adminService   *services.AdminService
	quotaService   *services.QuotaService
	userMapping    services.UserMappingStore
	preferences    *services.PreferencesService
	issueService   *services.IssueService
	sessMgr        *session.Manager
	registry       *callback.Registry
	webhookService *services.WebhookService
}

// NewRouter creates a new API router
func NewRouter(
	cfg *config.Config,
	telegram *services.TelegramClient,
	_ interface{}, // jellyseerr - deprecated, kept for compatibility
	adminService *services.AdminService,
	quotaService *services.QuotaService,
	userMapping services.UserMappingStore,
	preferences *services.PreferencesService,
	issueService *services.IssueService,
	sessMgr *session.Manager,
	registry *callback.Registry,
	webhookService *services.WebhookService,
) *Router {
	return &Router{
		cfg:            cfg,
		telegram:       telegram,
		adminService:   adminService,
		quotaService:   quotaService,
		userMapping:    userMapping,
		preferences:    preferences,
		issueService:   issueService,
		sessMgr:        sessMgr,
		registry:       registry,
		webhookService: webhookService,
	}
}

// SetupRoutes configures all HTTP routes
func (r *Router) SetupRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("/health", r.handleHealth)
	mux.HandleFunc("/api/health", r.handleHealth)

	// Stats and analytics
	mux.HandleFunc("/api/stats", r.handleStats)

	// Admin management
	mux.HandleFunc("/api/admins", r.handleAdmins)
	mux.HandleFunc("/api/admins/", r.handleAdminsByID)

	// Summary
	mux.HandleFunc("/api/summary", r.handleSummary)

	// Debug endpoints
	mux.HandleFunc("/debug", r.handleDebug)
	mux.HandleFunc("/api/debug", r.handleDebug)

	logger.Info("[Router] API routes configured")
}

// handleHealth handles health check requests
func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "yimao",
		"version": "2.0.0",
	})
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

	// Get quota info
	totalQuotas := 0
	if r.quotaService != nil {
		// Could get more detailed stats if needed
		totalQuotas = len(r.sessMgr.Stats())
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

// handleAdmins handles admin management requests
func (r *Router) handleAdmins(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch req.Method {
	case http.MethodGet:
		r.getAdmins(w, req)
	case http.MethodPost:
		r.addAdmin(w, req)
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

// addAdmin adds a new admin
func (r *Router) addAdmin(w http.ResponseWriter, req *http.Request) {
	var payload struct {
		UserID string `json:"user_id"`
		Name   string `json:"name"`
	}

	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if request has admin authorization
	adminUserID := req.Header.Get("X-Admin-User-ID")
	if adminUserID == "" {
		http.Error(w, "Missing X-Admin-User-ID header", http.StatusUnauthorized)
		return
	}

	if !r.adminService.IsAdmin(parseUserID(adminUserID)) {
		http.Error(w, "Unauthorized: not an admin", http.StatusForbidden)
		return
	}

	userID := parseUserID(payload.UserID)
	if userID == 0 {
		http.Error(w, "Invalid user_id", http.StatusBadRequest)
		return
	}

	if payload.Name == "" {
		payload.Name = payload.UserID
	}

	if err := r.adminService.AddAdmin(userID, payload.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("[API] Admin added: %s (%s) by %s", payload.Name, payload.UserID, adminUserID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Admin added successfully",
		"user_id": payload.UserID,
		"name":    payload.Name,
	})
}

// handleAdminsByID handles admin management by ID
func (r *Router) handleAdminsByID(w http.ResponseWriter, req *http.Request) {
	// Extract user ID from path
	path := strings.TrimPrefix(req.URL.Path, "/api/admins/")
	userID := parseUserID(path)

	if userID == 0 {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch req.Method {
	case http.MethodDelete:
		r.removeAdmin(w, req, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// removeAdmin removes an admin
func (r *Router) removeAdmin(w http.ResponseWriter, req *http.Request, userID int64) {
	// Check if request has admin authorization
	adminUserID := req.Header.Get("X-Admin-User-ID")
	if adminUserID == "" {
		http.Error(w, "Missing X-Admin-User-ID header", http.StatusUnauthorized)
		return
	}

	adminID := parseUserID(adminUserID)
	if !r.adminService.IsAdmin(adminID) {
		http.Error(w, "Unauthorized: not an admin", http.StatusForbidden)
		return
	}

	// Prevent self-deletion
	if adminID == userID {
		http.Error(w, "Cannot delete yourself", http.StatusBadRequest)
		return
	}

	if err := r.adminService.RemoveAdmin(userID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("[API] Admin removed: %d by %s", userID, adminUserID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Admin removed successfully",
		"user_id": userID,
	})
}

// HandleSummary sends daily summary (public method for server routing)
func (r *Router) HandleSummary(w http.ResponseWriter, req *http.Request) {
	r.handleSummary(w, req)
}

// handleSummary sends daily summary
func (r *Router) handleSummary(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if request has admin authorization
	adminUserID := req.Header.Get("X-Admin-User-ID")
	if adminUserID == "" {
		http.Error(w, "Missing X-Admin-User-ID header", http.StatusUnauthorized)
		return
	}

	if !r.adminService.IsAdmin(parseUserID(adminUserID)) {
		http.Error(w, "Unauthorized: not an admin", http.StatusForbidden)
		return
	}

	// Generate summary message
	message := r.generateSummary()

	// Send to all admins
	adminIDs := r.adminService.GetAdminIDs()
	successCount := 0

	for _, adminID := range adminIDs {
		if _, err := r.telegram.SendMessage(adminID, message, "", nil); err != nil {
			logger.Info("[API] Failed to send summary to admin %d: %v", adminID, err)
		} else {
			successCount++
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"message":         "Summary sent",
		"admins_notified": successCount,
	})
}

// generateSummary generates a daily summary message
func (r *Router) generateSummary() string {
	// Get pending requests from review service
	pendingCount := 0

	// Get session stats
	sessionStats := r.sessMgr.Stats()
	totalSessions := 0
	if count, ok := sessionStats["total_sessions"].(int); ok {
		totalSessions = count
	}

	message := "📊 今日统计汇总\n\n"
	message += fmt.Sprintf("⏳ 待处理请求: %d\n", pendingCount)
	message += fmt.Sprintf("👥 活跃用户: %d\n", totalSessions)
	message += fmt.Sprintf("🛡️ 管理员数: %d\n", r.adminService.GetAdminCount())
	message += fmt.Sprintf("\n生成时间: %s", formatCurrentTime())

	return message
}

// handleDebug handles debug requests.
// NOTE: this returns only non-sensitive runtime counters. Do not echo back
// configured URLs, data paths, or credentials here — earlier versions leaked
// moviepilot_url/emby_url/data_dir which is unsafe if this route is ever wired
// without authentication.
func (r *Router) handleDebug(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats := r.sessMgr.Stats()

	debugInfo := map[string]interface{}{
		"sessions": stats,
		"config": map[string]interface{}{
			"emby_configured":       r.cfg.EmbyURL != "",
			"moviepilot_configured": r.cfg.MoviePilotURL != "",
			"has_admins":            r.adminService.HasAdmins(),
		},
		"registry": map[string]interface{}{
			"status": "active",
		},
	}

	json.NewEncoder(w).Encode(debugInfo)
}

// parseUserID parses user ID from string
func parseUserID(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// formatCurrentTime returns formatted current time
func formatCurrentTime() string {
	return "刚刚"
}

// HandleWebhook handles webhook POST requests (for external services)
func (r *Router) HandleWebhook(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Optional webhook authentication: when WEBHOOK_SECRET is configured, every
	// inbound webhook must carry a valid HMAC-SHA256 signature over the raw body
	// (header "X-Webhook-Signature: sha256=<hex>"). A "?token=" / "X-Webhook-Token"
	// shared-secret fallback is also accepted. Without a secret configured this
	// check is skipped for backward compatibility.
	if secret := r.cfg.WebhookSecret; secret != "" {
		body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20)) // cap at 1MB
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
// It accepts either an HMAC-SHA256 signature of the raw body or a plain
// shared-secret token, both compared in constant time.
func verifyWebhookAuth(req *http.Request, body []byte, secret string) bool {
	// 1) HMAC signature: "X-Webhook-Signature: sha256=<hex>"
	sig := req.Header.Get("X-Webhook-Signature")
	if sig != "" {
		sig = strings.TrimPrefix(sig, "sha256=")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}
	// 2) Plain shared-secret token fallback (?token= or X-Webhook-Token)
	token := req.URL.Query().Get("token")
	if token == "" {
		token = req.Header.Get("X-Webhook-Token")
	}
	if token != "" {
		return subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
	}
	return false
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
