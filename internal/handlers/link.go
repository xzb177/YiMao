package handlers

import (
	"fmt"
	"log"

	"emby-telegram-bot/internal/callback"
	"emby-telegram-bot/internal/config"
	"emby-telegram-bot/internal/services"
	"emby-telegram-bot/internal/session"
)

// LinkHandler handles account linking
type LinkHandler struct {
	cfg                *config.Config
	sessMgr            *session.Manager
	telegram           *services.TelegramClient
	moviepilot         *services.MoviePilotClient
	userMapping        *services.UserMappingService
	bindingRequestService *services.BindingRequestService
}

func NewLinkHandler(
	cfg *config.Config,
	sessMgr *session.Manager,
	telegram *services.TelegramClient,
	moviepilot *services.MoviePilotClient,
	userMapping *services.UserMappingService,
	bindingRequestService *services.BindingRequestService,
) *LinkHandler {
	return &LinkHandler{
		cfg:                  cfg,
		sessMgr:              sessMgr,
		telegram:             telegram,
		moviepilot:           moviepilot,
		userMapping:          userMapping,
		bindingRequestService: bindingRequestService,
	}
}

func (h *LinkHandler) Handle(ctx *callback.Context) (*callback.Response, error) {
	// Check if already linked
	if _, exists := h.userMapping.GetMoviePilotUserID(ctx.UserID); exists {
		return &callback.Response{
			Text:   "✅ 账号已绑定\n\n您已经绑定了 MoviePilot 账号",
			Edit:   true,
		}, nil
	}

	// Show link instructions
	return &callback.Response{
		Text:   h.getLinkInstructions(),
		Edit:   true,
	}, nil
}

func (h *LinkHandler) getLinkInstructions() string {
	return `🔗 绑定 MoviePilot 账号

绑定后即可使用求片功能并同步观影记录

📝 绑定格式：
/link 用户名 密码

📌 示例：
/link johndoe mypassword123

💡 您的凭据直接发送至 MoviePilot 服务器验证，机器人不做存储`
}

// HandleWithCredentials handles linking with username and password
func (h *LinkHandler) HandleWithCredentials(telegramID int64, username, password string) error {
	// First, try to get user by username from MoviePilot
	user, err := h.moviepilot.GetUserByUsername(username)

	// If user doesn't exist, try to register automatically
	if err != nil {
		log.Printf("[LinkHandler] User not found, attempting to register: %s", username)

		// Use username as email if email is provided separately
		email := username + "@local" // Placeholder email

		user, err = h.moviepilot.RegisterUser(username, password, email)
		if err != nil {
			return fmt.Errorf("用户不存在且注册失败: %w", err)
		}

		log.Printf("[LinkHandler] Successfully registered new user: %s (ID: %d)", username, user.ID)
	}

	// Create binding request
	requestID := fmt.Sprintf("bind_%d_%d", telegramID, user.ID)

	req := &services.BindingRequest{
		RequestID:        requestID,
		TelegramID:       telegramID,
		MoviePilotID:     user.ID,
		MoviePilotName:   user.Email,
		MoviePilotUsername: user.Username,
		Status:           "approved", // Auto-approve for direct login
	}

	if err := h.bindingRequestService.CreateRequest(req); err != nil {
		return err
	}

	// Approve immediately (self-binding via credentials)
	if err := h.bindingRequestService.ApproveRequest(requestID, h.userMapping); err != nil {
		return err
	}

	log.Printf("[LinkHandler] User %d linked to MoviePilot user %s (%s)", telegramID, username, user.Email)

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
