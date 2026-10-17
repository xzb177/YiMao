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

	switch resolveWebhookType(req) {
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

// resolveWebhookType 决定外部 webhook 的类型。优先级：
//  1. URL 路径（/webhook/emby、/webhook/jellyseerr、/webhook/moviepilot、/webhook/mp）
//  2. ?type= 查询参数（兼容旧调用方）
//  3. X-Webhook-Type 请求头
//  4. "" —— 走请求体自动识别
//
// 审计第 10 条：以前只看 2/3，路径被完全忽略——没带 type 的 Jellyseerr /
// MoviePilot 事件会掉进 auto-detect，被误判成 Emby（Jellyseerr 的 "event"
// 字段恰好命中 Emby payload 的 Event 备选字段），返回 200 却什么都没做。
func resolveWebhookType(req *http.Request) string {
	path := strings.ToLower(strings.TrimSuffix(req.URL.Path, "/"))
	switch {
	case strings.HasSuffix(path, "/webhook/emby"):
		return "emby"
	case strings.HasSuffix(path, "/webhook/jellyseerr"):
		return "jellyseerr"
	case strings.HasSuffix(path, "/webhook/moviepilot"), strings.HasSuffix(path, "/webhook/mp"):
		return "moviepilot"
	}
	if t := req.URL.Query().Get("type"); t != "" {
		return strings.ToLower(t)
	}
	if t := req.Header.Get("X-Webhook-Type"); t != "" {
		return strings.ToLower(t)
	}
	return ""
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

	switch kind := classifyAutoDetectWebhook(body); kind {
	case "emby":
		var embyPayload services.EmbyWebhookPayload

		_ = json.Unmarshal(body, &embyPayload)
		event := embyPayload.Event
		if event == "" {
			event = embyPayload.EventField
		}
		logger.Info("[API] Detected Emby webhook: %s", event)
		if err := r.webhookService.HandleEmbyWebhook(embyPayload); err != nil {
			logger.Info("[API] Failed to handle Emby webhook: %v", err)
			http.Error(w, "Failed to process", http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
				logger.Info("[API] Failed to write Emby webhook response: %v", err)
			}
		}
	case "jellyseerr":
		var jellyseerrPayload services.JellyseerrWebhookPayload

		_ = json.Unmarshal(body, &jellyseerrPayload)
		logger.Info("[API] Detected Jellyseerr webhook: %s", jellyseerrPayload.Event)
		if err := r.webhookService.HandleJellyseerrWebhook(jellyseerrPayload); err != nil {
			logger.Info("[API] Failed to handle Jellyseerr webhook: %v", err)
			http.Error(w, "Failed to process", http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
				logger.Info("[API] Failed to write Jellyseerr webhook response: %v", err)
			}
		}
	default:
		// 无法识别：明确返回 400 并记日志，不再假装 200。
		logger.Warn("[API] Unknown webhook type (path=%s), Size: %d bytes", req.URL.Path, len(body))
		http.Error(w, "Unknown webhook type", http.StatusBadRequest)
	}
}

// classifyAutoDetectWebhook 在没有显式类型（路径/?type=/请求头）时，根据请求体
// 判定事件来源。审计第 10 条的教训：Jellyseerr 的 "event" 字段会命中
// EmbyWebhookPayload 的 Event 备选字段（JSON 大小写不敏感），旧代码把
// Jellyseerr 事件误判成 Emby、返回 200 却什么都没做。
// 判定规则只认各自独有的标记，不认通用字段：
//   - emby: 必须有 Emby 原生标记（NotificationType / ItemId / ItemName）
//   - jellyseerr: 必须有 "event" 且带其独有键（subject / media / request / issue / user）
//
// 两个都不满足 → "unknown"，调用方返回 400。
func classifyAutoDetectWebhook(body []byte) string {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(body, &generic); err != nil {
		return "unknown"
	}
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := generic[k]; ok {
				return true
			}
		}
		return false
	}

	// 先判 Jellyseerr：它的 event 字段正是之前误判的来源。
	if _, ok := generic["event"]; ok {
		if has("subject", "media", "request", "issue", "user") {
			return "jellyseerr"
		}
	}

	// Emby 只认原生标记，不认通用的 Event 备选字段。
	var embyPayload services.EmbyWebhookPayload
	if err := json.Unmarshal(body, &embyPayload); err == nil {
		if embyPayload.Event != "" || embyPayload.ItemID != "" || embyPayload.ItemName != "" {
			return "emby"
		}
	}

	return "unknown"
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
