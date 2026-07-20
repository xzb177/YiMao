package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/xzb177/yimao/pkg/logger"
	"github.com/xzb177/yimao/pkg/types"
)

// Action represents a callback action
type Action string

const (
	ActionSearch        Action = "search"
	ActionSubscribe     Action = "subscribe"
	ActionDownload      Action = "download"
	ActionPage          Action = "page"
	ActionCancel        Action = "cancel"
	ActionSelect        Action = "select"
	ActionBack          Action = "back"
	ActionDetail        Action = "detail"
	ActionDetailSeasons Action = "detail_seasons"
	ActionRequest       Action = "request"
	ActionFeedback      Action = "feedback"
	ActionStart         Action = "start"
	ActionRandom        Action = "random"
	ActionRequests      Action = "requests"
	ActionLink          Action = "link"
	ActionHelp          Action = "help"
	ActionHelpTopic     Action = "help_topic"
	ActionSettings      Action = "settings"
	ActionAI            Action = "ai"
	ActionRequestHeat   Action = "request_heat"
	ActionMood          Action = "mood"
	ActionMoodPick      Action = "moodpick"
	ActionQuickPick     Action = "quickpick"
	ActionHot           Action = "hot"
	ActionNew           Action = "new"

	// My Requests pagination actions
	ActionMyReqsPage Action = "myreqs_page"
	ActionMyReqsItem Action = "myreqs_item"

	// Resource candidate actions
	ActionResourceList Action = "res_list" // Show candidate list
	ActionResourcePick Action = "res_pick" // Pick a resource
	ActionResourceSort Action = "res_sort" // Sort resources
	ActionResourcePrev Action = "res_prev" // Previous page
	ActionResourceNext Action = "res_next" // Next page
)

// validActions is the whitelist of allowed callback actions
var validActions = map[Action]bool{
	// Standard actions
	ActionStart:  true,
	ActionSearch: true,

	ActionDetail:        true,
	ActionDetailSeasons: true,
	ActionRequest:       true,
	ActionPage:          true,
	ActionSelect:        true,
	ActionBack:          true,
	ActionCancel:        true,
	ActionRequests:      true,
	ActionLink:          true,
	ActionHelp:          true,
	ActionHelpTopic:     true,
	ActionSettings:      true,
	ActionRequestHeat:   true,
	ActionFeedback:      true,
	"my_feedback":       true, // User feedback list
	"start_settings":    true, // Settings page
	"start_ai":          true, // Reserved
	"ai":                true, // start_ai 剥前缀后的实际 action
	"wish":              true, // 许愿池入口（start_wish 剥前缀后）
	"wish_cancel":       true, // 许愿撤回
	"myreq_cancel":      true, // 用户撤回 pending 求片申请
	"my_requests":       true, // 历史消息中的求片进度按钮

	"notify_settings": true, // 通知设置页
	"notify_toggle":   true, // 通知开关切换
	"resetpw":         true, // 重置密码

	// My Requests pagination actions
	ActionMyReqsPage: true,
	ActionMyReqsItem: true,

	// Review system actions
	"review_approve": true,
	"review_reject":  true,
	"review_cancel":  true,
	"my_reviews":     true,
	"review_list":    true,
	// Short format actions (to keep CallbackData under 64 bytes)
	"rv_a": true, // approve by token
	"rv_r": true, // reject by token

	// Admin actions
	"admin_approve":               true,
	"admin_decline":               true,
	"admin_pending":               true,
	"admin_issue_reply":           true,
	"admin_issue_fixed":           true,
	"admin_issue_processing":      true,
	"admin_issue_close":           true,
	"admin_menu":                  true,
	"admin_notif_settings":        true,
	"admin_notif_toggle_instant":  true,
	"admin_notif_toggle_daily":    true,
	"admin_notif_toggle":          true,
	"admin_notif_settime":         true,
	"admin_notif_format_simple":   true,
	"admin_notif_format_detailed": true,
	// 新的 V2 通知设置回调 - 状态融合按钮
	"admin_notif_toggle_single_v2": true,
	"admin_notif_toggle_daily_v2":  true,
	"admin_notif_toggle_format":    true,
	"admin_notif_disable_all":      true,
	// 管理员管理回调 - 仅超级管理员可用
	"admin_mgmt":              true,
	"admin_list":              true,
	"admin_add_start":         true,
	"admin_remove_list":       true,
	"admin_remove_confirm":    true,
	"admin_dashboard":         true, // 数据概览面板
	"admin_todo":              true, // 管理员待办中心
	"admin_request_stats":     true, // 求片统计面板
	"admin_notif_custom_time": true, // 自定义每日汇总时间输入

	// Search History actions
	"search_history_menu":    true,
	"search_stats":           true,
	"search_popular":         true,
	"popular_week":           true,
	"popular_all":            true,
	"search_trends":          true,
	"search_manage":          true,
	"search_delete":          true,
	"search_clear_all":       true,
	"search_popular_refresh": true,
	"search_trends_refresh":  true,
	"search_input":           true, // 快速搜索输入

	// Request related actions
	"force_subscribe": true,
	"cancel_request":  true,
	"carpool":         true, // 拼车 +1：用户标记「我也想看」
	"wish_request":    true, // #6 许愿池：出源喜报「立即求片」按钮（走现有 request 流程 + 确认）
	"wish_add":        true, // #1 搜索无结果「🌟 加入许愿池」按钮（片名存 session，回调不带超长参数）

	// Admin Feedback Panel actions
	"admin_feedback":          true, // Feedback management main panel
	"admin_feedback_stats":    true, // Feedback statistics
	"admin_feedback_list":     true, // Feedback list
	"admin_feedback_filter":   true, // Filter by status
	"admin_feedback_priority": true, // Adjust priority
	"admin_feedback_detail":   true, // View feedback detail
	"admin_feedback_reply":    true, // Reply to feedback
	"admin_feedback_template": true, // Quick reply template

	// User Feedback actions
	"feedback_follow_up": true, // User follow-up message
	"feedback_close":     true, // User close feedback
	"feedback_rate_1":    true, // 1 star rating
	"feedback_rate_2":    true, // 2 star rating
	"feedback_rate_3":    true, // 3 star rating
	"feedback_rate_4":    true, // 4 star rating
	"feedback_rate_5":    true, // 5 star rating

	// Weekly Report actions
	"weekly_report":      true, // Show weekly report
	"weekly_report_send": true, // Send weekly report

	// Portrait actions
	"portrait": true, // 观影画像

	// Game center actions
	"game_menu":                 true, // 游戏中心主菜单
	"game_rank":                 true, // 段位系统
	"game_narrator":             true, // AI解说入口
	"game_narrate":              true, // AI解说生成/剧透切换
	"game_blindbox":             true, // 盲盒入口
	"game_blindbox_open":        true, // 盲盒开盒
	"game_blindbox_horror":      true, // 恐怖盲盒
	"game_blindbox_personality": true, // 盲盒性格分析
	"game_personality":          true, // 旧画像入口（兼容提示）
	"game_contract":             true, // 旧契约入口（兼容提示）
	"game_social":               true, // 社交动态
	"game_emotion":              true, // 情绪画像
	"game_achievements":         true, // 成就系统
	"game_compare":              true, // 品味对比

	// Adventure game actions
	"adventure_start":         true, // 电影冒险入口
	"adventure_choice":        true, // 选择选项
	"adventure_hint":          true, // 问导演（花HP换线索）
	"adventure_retry":         true, // 重试冒险
	"adventure_quit":          true, // 退出冒险
	"adventure_share":         true, // 分享战绩到群
	"adventure_revive":        true, // 🩸 每日免费复活
	"adventure_gamble":        true, // 🎰 双倍或归零 - 赌
	"adventure_gamble_safe":   true, // 📦 双倍或归零 - 安全领
	"adventure_gamble_triple": true, // 尝试三倍奖励
	"game_adventure_stats":    true, // 冒险统计
	"game_adventure_rank":     true, // 冒险排行榜
	"game_daily_challenge":    true, // 每日挑战

	// Resource candidate actions
	ActionResourceList: true,
	ActionResourcePick: true,
	ActionResourceSort: true,
	ActionResourcePrev: true,
	ActionResourceNext: true,
	"rp":               true, // Short format for resource pick (rp:%d)
}

// isValidAction checks if an action is in the whitelist
func isValidAction(action Action) bool {
	return validActions[action]
}

// actionWhiteListMu protects access to validActions for future dynamic updates
var actionWhiteListMu sync.RWMutex

// Callback represents a standardized callback query
type Callback struct {
	Action Action            `json:"action"`
	Params map[string]string `json:"params,omitempty"`
	Raw    string            `json:"-"`
}

// Handler handles a specific callback action
type Handler interface {
	Handle(ctx *Context) (*Response, error)
	Action() Action
}

// HandlerFunc is a function adapter for Handler
type HandlerFunc func(ctx *Context) (*Response, error)

func (h HandlerFunc) Handle(ctx *Context) (*Response, error) {
	return h(ctx)
}

func (h HandlerFunc) Action() Action {
	return "" // Will be set during registration
}

// Context provides context for callback handling
type Context struct {
	UserID             int64
	ChatID             int64
	ChatType           string // "private", "group", "supergroup", "channel"
	MessageID          int64
	MessageThreadID    int64
	EphemeralMessageID int64
	CallbackID         string
	Callback           *Callback
	SessionData        interface{}
}

// Response represents the result of callback handling
type Response struct {
	Text                  string
	Edit                  bool
	ShowAlert             bool
	Keyboard              *Keyboard
	CallbackMsg           string
	Photo                 string                          // Photo URL to send (will send as new message, not edit)
	PhotoCaption          string                          // Caption for the photo
	DeleteMessage         bool                            // If true, delete the current message before sending new one
	ParseMode             string                          // Parse mode for formatting (HTML, Markdown, or empty for none)
	RichMessage           string                          // Rich Message markdown content (legacy-compatible)
	StructuredRichMessage *types.TelegramInputRichMessage // Typed Bot API 10.2 rich content
}

// Keyboard represents an inline keyboard
type Keyboard struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}

// Button represents a keyboard button
type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// Parser parses and formats callback data
type Parser struct {
	// Standard format: action:param1:value1:param2:value2
	// JSON format: {"action":"search","params":{"id":"123","type":"movie"}}
	useJSON bool
}

// NewParser creates a new callback parser
func NewParser() *Parser {
	return &Parser{
		useJSON: false, // Default to colon-separated format
	}
}

// NewJSONParser creates a JSON-based callback parser
func NewJSONParser() *Parser {
	return &Parser{
		useJSON: true,
	}
}

// Parse parses callback data string
func (p *Parser) Parse(data string) (*Callback, error) {
	if data == "" {
		return nil, fmt.Errorf("empty callback data")
	}

	// Try JSON format first
	if strings.HasPrefix(data, "{") {
		var cb Callback
		if err := json.Unmarshal([]byte(data), &cb); err != nil {
			return nil, fmt.Errorf("invalid JSON callback: %w", err)
		}
		cb.Raw = data
		if cb.Params == nil {
			cb.Params = make(map[string]string)
		}
		// Validate action against whitelist
		actionWhiteListMu.RLock()
		valid := isValidAction(cb.Action)
		actionWhiteListMu.RUnlock()
		if !valid {
			return nil, fmt.Errorf("invalid action: %s", cb.Action)
		}
		return &cb, nil
	}

	// Parse colon-separated format: action:param1:value1:param2:value2
	parts := strings.Split(data, ":")
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid callback format")
	}

	// Handle legacy start_* format by stripping "start_" prefix
	actionStr := parts[0]
	if strings.HasPrefix(actionStr, "start_") {
		actionStr = strings.TrimPrefix(actionStr, "start_")
	}

	action := Action(actionStr)

	// Validate action against whitelist
	actionWhiteListMu.RLock()
	valid := isValidAction(action)
	actionWhiteListMu.RUnlock()
	if !valid {
		return nil, fmt.Errorf("invalid action: %s", action)
	}

	cb := &Callback{
		Action: action,
		Params: make(map[string]string),
		Raw:    data,
	}

	// Parse key-value pairs
	for i := 1; i < len(parts); i += 2 {
		if i+1 < len(parts) {
			key := parts[i]
			value := parts[i+1]
			cb.Params[key] = value
		}
	}

	return cb, nil
}

// MustParse parses callback data or panics
func (p *Parser) MustParse(data string) *Callback {
	cb, err := p.Parse(data)
	if err != nil {
		panic(err)
	}
	return cb
}

// Format formats a callback to string
func (p *Parser) Format(action Action, params map[string]string) string {
	if p.useJSON {
		cb := Callback{
			Action: action,
			Params: params,
		}
		data, err := json.Marshal(cb)
		if err != nil {
			// Fallback to colon format
			return p.formatColon(action, params)
		}
		return string(data)
	}

	return p.formatColon(action, params)
}

// formatColon formats callback as colon-separated string
func (p *Parser) formatColon(action Action, params map[string]string) string {
	if len(params) == 0 {
		return string(action)
	}

	parts := []string{string(action)}
	// Sort keys to ensure consistent order
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		parts = append(parts, k, params[k])
	}
	return strings.Join(parts, ":")
}

// FormatSimple formats a simple callback with just an action
func (p *Parser) FormatSimple(action Action) string {
	return p.Format(action, nil)
}

// Registry manages callback handlers
type Registry struct {
	handlers   map[Action]Handler
	middleware []Middleware
	parser     *Parser
}

// Middleware is a function that wraps a handler
type Middleware func(Handler) Handler

// NewRegistry creates a new callback registry
func NewRegistry() *Registry {
	return &Registry{
		handlers:   make(map[Action]Handler),
		middleware: make([]Middleware, 0),
		parser:     NewParser(),
	}
}

// Use adds a middleware to the registry
func (r *Registry) Use(mw Middleware) {
	r.middleware = append(r.middleware, mw)
}

// Register registers a handler for an action
func (r *Registry) Register(action Action, handler Handler) {
	r.handlers[action] = handler
}

// RegisterFunc registers a handler function for an action
func (r *Registry) RegisterFunc(action Action, handler func(ctx *Context) (*Response, error)) {
	r.Register(action, HandlerFunc(handler))
}

// Get retrieves a handler for an action
func (r *Registry) Get(action Action) (Handler, bool) {
	handler, exists := r.handlers[action]
	if !exists {
		return nil, false
	}

	// Apply middleware
	for i := len(r.middleware) - 1; i >= 0; i-- {
		handler = r.middleware[i](handler)
	}

	return handler, true
}

// Parser returns the parser
func (r *Registry) Parser() *Parser {
	return r.parser
}

// Match checks if a callback data matches an action pattern
// Supports wildcards, e.g., "search:*" matches all search callbacks
func (r *Registry) Match(pattern string, data string) bool {
	cb, err := r.parser.Parse(data)
	if err != nil {
		return false
	}

	// Exact match
	if pattern == string(cb.Action) {
		return true
	}

	// Wildcard match
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(string(cb.Action), prefix)
	}

	// Regex match
	if strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$") {
		matched, err := regexp.MatchString(pattern, data)
		if err != nil {
			// Invalid regex pattern - log and treat as no match
			logger.Info("[Callback] Invalid regex pattern: %s, error: %v", pattern, err)
			return false
		}
		return matched
	}

	return false
}

// Helper functions for building callbacks

// ShortRef returns a deterministic compact reference for session-backed
// callback payloads. Twelve hexadecimal characters provide ample separation
// while keeping callback_data comfortably below Telegram's 64-byte limit.
func ShortRef(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}

// BuildCallback builds a callback string
func BuildCallback(action Action, params map[string]string) string {
	p := NewParser()
	return p.Format(action, params)
}

// BuildSimpleCallback builds a simple callback with just an action
func BuildSimpleCallback(action Action) string {
	return BuildCallback(action, nil)
}

// BuildSearchCallback builds a search callback
func BuildSearchCallback(mediaID, mediaType string) string {
	return BuildCallback(ActionSearch, map[string]string{
		"id":   mediaID,
		"type": mediaType,
	})
}

// BuildDetailCallback builds a detail callback
func BuildDetailCallback(mediaID, mediaType string) string {
	return BuildCallback(ActionDetail, map[string]string{
		"id":   mediaID,
		"type": mediaType,
	})
}

// BuildRequestCallback builds a request callback
func BuildRequestCallback(mediaID, mediaType string, season int) string {
	params := map[string]string{
		"id":   mediaID,
		"type": mediaType,
	}
	if season > 0 {
		params["season"] = fmt.Sprintf("%d", season)
	}
	return BuildCallback(ActionRequest, params)
}

// BuildPageCallback builds a page navigation callback
func BuildPageCallback(page int, source string) string {
	return BuildCallback(ActionPage, map[string]string{
		"num":    fmt.Sprintf("%d", page),
		"source": source,
	})
}

// BuildBackCallback builds a back navigation callback
func BuildBackCallback() string {
	return BuildSimpleCallback(ActionBack)
}

// BuildCancelCallback builds a cancel callback
func BuildCancelCallback() string {
	return BuildSimpleCallback(ActionCancel)
}
