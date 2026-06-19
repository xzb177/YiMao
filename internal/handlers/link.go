package handlers

import (
	"fmt"

	"github.com/xzb177/yimao/internal/callback"
	"github.com/xzb177/yimao/internal/config"
	"github.com/xzb177/yimao/internal/services"
	"github.com/xzb177/yimao/internal/session"
	"github.com/xzb177/yimao/pkg/logger"
)

// LinkHandler handles account linking
type LinkHandler struct {
	cfg                   *config.Config
	sessMgr               *session.Manager
	telegram              *services.TelegramClient
	moviepilot            *services.MoviePilotClient
	userMapping           services.UserMappingStore
	bindingRequestService *services.BindingRequestService
}

func NewLinkHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	userMapping services.UserMappingStore,
	bindingRequestService *services.BindingRequestService,
) *LinkHandler {
	return &LinkHandler{
		cfg:                   cfg,
		sessMgr:               sessMgr,
		telegram:              telegram,
		moviepilot:            moviepilot,
		userMapping:           userMapping,
		bindingRequestService: bindingRequestService,
	}
}

func (h *LinkHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Check if already linked
	if moviepilotID, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID); exists {
		logger.Info("[LinkHandler] User %d already linked to moviepilot_id=%d", ctx.UserID, moviepilotID)
		// Update session with the MoviePilot ID
		sess := h.sessMgr.GetOrCreate(ctx.UserID)
		sess.Set("moviepilot_id", int(moviepilotID))

		return &callback.Response{
			Text: "✅ 已绑定\n\n你的账号已绑定，可以使用完整功能",
			Edit: true,
		}, nil
	}

	logger.Info("[LinkHandler] User %d not linked yet", ctx.UserID)

	// Show link instructions
	return &callback.Response{
		Text: h.getLinkInstructions(),
		Edit: true,
	}, nil
}

func (h *LinkHandler) getLinkInstructions() string {
	return `🔗 绑定账号

绑定后可以：

• 查看/管理你提交过的求片
• 收取完成通知
• 查看下载进度

📝 怎么绑定：
/link 用户名

📌 示例：
/link cabbeenpoom

💡 不需要密码，首次会自动创建账号`
}

// HandleWithCredentials handles linking with username and password
func (h *LinkHandler) HandleWithCredentials(telegramID int64, username, password string) error {
	// First, try to get user by username from MoviePilot
	user, err := h.moviepilot.GetUserByUsername(username)

	// If user doesn't exist, register automatically
	if err != nil {
		logger.Info("[LinkHandler] User not found, registering new user: %s", username)

		// Auto-register user in MoviePilot
		email := username + "@local" // Placeholder email

		user, err = h.moviepilot.RegisterUser(username, password, email)
		if err != nil {
			return fmt.Errorf("注册失败: %w", err)
		}

		logger.Info("[LinkHandler] Successfully registered new user: %s (ID: %d)", username, user.ID)
	} else {
		logger.Info("[LinkHandler] Found existing user %s (ID: %d)", username, user.ID)
	}

	// Create binding request
	requestID := fmt.Sprintf("bind_%d_%d", telegramID, user.ID)

	req := &services.BindingRequest{
		RequestID:          requestID,
		TelegramID:         telegramID,
		MoviePilotID:       user.ID,
		MoviePilotName:     user.Username,
		MoviePilotUsername: user.Username,
		Status:             "approved", // Auto-approve for direct login
	}

	if err := h.bindingRequestService.CreateRequest(req); err != nil {
		return err
	}

	// Approve immediately (self-binding via credentials)
	if err := h.bindingRequestService.ApproveRequest(requestID, h.userMapping); err != nil {
		return err
	}

	// Update session with MoviePilot user ID
	sess := h.sessMgr.GetOrCreate(telegramID)
	sess.Set("moviepilot_id", int(user.ID))

	logger.Info("[LinkHandler] User %d linked to MoviePilot user %s (ID: %d)", telegramID, username, user.ID)

	return nil
}

// HandleUnlink handles unlinking
func (h *LinkHandler) HandleUnlink(telegramID int64) error {
	return h.userMapping.RemoveMapping(telegramID)
}

// IsLinked checks if user is linked
func (h *LinkHandler) IsLinked(telegramID int64) bool {
	_, exists := h.userMapping.GetMoviePilotUserID(telegramID)
	return exists
}

// GetLinkedUser returns the linked MoviePilot user ID
func (h *LinkHandler) GetLinkedUser(telegramID int64) (int64, bool) {
	return h.userMapping.GetMoviePilotUserID(telegramID)
}

// HandleResetPW handles the resetpw callback (from keyboard button)
func (h *LinkHandler) HandleResetPW(ctx *callback.Context) (*callback.Response, error) {
	// Check if DB path is configured
	if h.cfg.MoviePilotDBPath == "" {
		return &callback.Response{
			Text: "❌ 密码重置功能未配置，请联系管理员",
			Edit: true,
		}, nil
	}

	// Get linked MoviePilot username
	mpUsername, err := h.userMapping.GetMoviePilotUsername(ctx.UserID)
	if err != nil || mpUsername == "" {
		return &callback.Response{
			Text: "🔗 未绑定账号\n\n请先绑定，或使用命令：\n/resetpw 用户名",
			Edit: true,
		}, nil
	}

	// Reset password
	newPassword, err := h.moviepilot.ResetUserPassword(h.cfg.MoviePilotDBPath, mpUsername)
	if err != nil {
		logger.Info("[ResetPW Callback] Failed for %s: %v", mpUsername, err)
		return &callback.Response{
			Text: "❌ 密码重置失败：" + err.Error(),
			Edit: true,
		}, nil
	}

	text := fmt.Sprintf("🔑 密码重置成功！\n\n"+
		"👤 用户名：<code>%s</code>\n"+
		"🔐 新密码：<code>%s</code>\n\n"+
		"请用新密码绑定：\n<code>/link %s %s</code>\n\n"+
		"⚠️ 请妥善保管",
		mpUsername, newPassword, mpUsername, newPassword)

	if ctx.ChatType == "group" || ctx.ChatType == "supergroup" {
		if _, err := h.telegram.SendMessage(ctx.UserID, text, "HTML", nil); err != nil {
			return &callback.Response{Text: "🔒 密码已重置，但私聊发送失败。请先私聊机器人发送任意消息。", Edit: true}, nil
		}
		return &callback.Response{Text: "✅ 密码已重置，请查看私聊消息", Edit: true}, nil
	}

	return &callback.Response{
		Text:      text,
		Edit:      true,
		ParseMode: "HTML",
	}, nil
}
