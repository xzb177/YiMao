// Package services provides AI chat service with intelligent conversation
package services

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"emby-telegram-bot/ai"
)

// ConversationStyle defines the conversation personality
type ConversationStyle string

const (
	StyleFriendly    ConversationStyle = "friendly"    // 友好热情
	StyleProfessional ConversationStyle = "professional" // 专业助手
	StylePlayful     ConversationStyle = "playful"     // 调皮可爱
)

// ChatService provides intelligent chat capabilities for private chats
// This is the simplified version without streaming - streaming is handled by StreamingChatHandler for groups
type ChatService struct {
	agent         *ai.Agent
	convMgr       *ai.ConversationManager
	tgClient      *TelegramClient
	style         ConversationStyle
	lastChatTime  map[int64]time.Time
	chatCooldown  time.Duration
	adminIDs      map[int64]bool
	mu            sync.RWMutex

	// Knowledge base for quick responses
	knowledgeBase map[string]string
}

// ChatType represents the type of chat
type ChatType int

const (
	ChatTypePrivate ChatType = iota // 私聊
	ChatTypeGroup                   // 群组
	ChatTypeSupergroup              // 超级群组
)

// ChatMessage represents a chat message with context
type ChatMessage struct {
	UserID    int64
	UserName  string
	Content   string
	IsReply   bool
	IsMention bool
	ChatType  ChatType
	Timestamp time.Time
}

// ChatResponse represents the response from chat service
type ChatResponse struct {
	Text        string
	ShouldReply bool
}

// NewChatService creates a new AI chat service
func NewChatService(agent *ai.Agent, convMgr *ai.ConversationManager, tgClient *TelegramClient) *ChatService {
	cs := &ChatService{
		agent:         agent,
		convMgr:       convMgr,
		tgClient:      tgClient,
		style:         StyleFriendly,
		lastChatTime:  make(map[int64]time.Time),
		chatCooldown:  2 * time.Second,
		adminIDs:      make(map[int64]bool),
		knowledgeBase: make(map[string]string),
	}

	// Initialize knowledge base
	cs.initKnowledgeBase()

	return cs
}

// initKnowledgeBase initializes quick response knowledge
func (cs *ChatService) initKnowledgeBase() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Common questions and answers
	entries := map[string]string{
		"怎么求片": "🔍 求片方法：\n1. 点击 /start 打开菜单\n2. 选择「🔍 搜索影片」\n3. 输入影片名称\n4. 点击结果中的「📋 请求」按钮",
		"如何求片": "🔍 求片方法：\n1. 点击 /start 打开菜单\n2. 选择「🔍 搜索影片」\n3. 输入影片名称\n4. 点击结果中的「📋 请求」按钮",
		"怎么绑定": "🔗 绑定账号：\n1. 点击 /start 打开菜单\n2. 选择「🔗 绑定账号」\n3. 输入 Jellyseerr 用户名\n4. 或使用凭证方式绑定",
		"如何绑定": "🔗 绑定账号：\n1. 点击 /start 打开菜单\n2. 选择「🔗 绑定账号」\n3. 输入 Jellyseerr 用户名\n4. 或使用凭证方式绑定",
		"配额": "📊 每日配额说明：\n• 电影：默认每天 2 部\n• 剧集：默认每天 2 部\n• 配额每天自动重置\n• 管理员无限制",
		"限制": "📊 每日配额说明：\n• 电影：默认每天 2 部\n• 剧集：默认每天 2 部\n• 配额每天自动重置\n• 管理员无限制",
		"emby地址": "🎬 Emby 地址：\nhttps://emby.oceancloud.asia\n\n请使用浏览器访问，或使用 Emby 客户端添加服务器",
		"播放地址": "🎬 Emby 地址：\nhttps://emby.oceancloud.asia\n\n请使用浏览器访问，或使用 Emby 客户端添加服务器",
		"怎么看": "🎬 观看方法：\n1. 访问 https://emby.oceancloud.asia\n2. 登录您的账号\n3. 在媒体库中找到想看的内容\n4. 点击播放即可",
		"在哪看": "🎬 观看方法：\n1. 访问 https://emby.oceancloud.asia\n2. 登录您的账号\n3. 在媒体库中找到想看的内容\n4. 点击播放即可",
		"有问题": "🐛 问题反馈：\n1. 在影片详情页点击「🐛 反馈」按钮\n2. 选择问题类型\n3. 描述问题详情\n4. 管理员会收到通知并处理",
		"报错": "🐛 问题反馈：\n1. 在影片详情页点击「🐛 反馈」按钮\n2. 选择问题类型\n3. 描述问题详情\n4. 管理员会收到通知并处理",
		"管理员": "👑 管理员负责管理和服务大家，处理求片请求和问题反馈",
		"主人": "👑 管理员负责管理和服务大家，处理求片请求和问题反馈",
		"你是谁": "✨ 我是云海影视助手小凛\n\n可以帮你：\n• 🔍 搜索影视内容\n• 🤖 AI 智能推荐\n• 📋 一键求片\n• 💬 闲聊互动",
		"叫什么": "✨ 我是云海影视助手小凛\n\n可以帮你：\n• 🔍 搜索影视内容\n• 🤖 AI 智能推荐\n• 📋 一键求片\n• 💬 闲聊互动",
		"什么功能": "✨ 我的功能：\n• 🔍 搜索影视内容\n• 🤖 AI 智能推荐\n• 📋 一键求片\n• 💬 闲聊互动\n\n发送 /start 开始使用~",
		"你会什么": "✨ 我的功能：\n• 🔍 搜索影视内容\n• 🤖 AI 智能推荐\n• 📋 一键求片\n• 💬 闲聊互动\n\n发送 /start 开始使用~",
	}

	for k, v := range entries {
		cs.knowledgeBase[k] = v
	}
}

// SetAdminIDs sets the admin user IDs
func (cs *ChatService) SetAdminIDs(adminIDs []int64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.adminIDs = make(map[int64]bool)
	for _, id := range adminIDs {
		cs.adminIDs[id] = true
	}
}

// SetStyle sets the conversation style
func (cs *ChatService) SetStyle(style ConversationStyle) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.style = style
}

// ShouldReply determines if the service should reply to a message
// Note: For private chats, this always returns true if there's content
func (cs *ChatService) ShouldReply(msg *ChatMessage) bool {
	// Private chats always get a response
	if msg.ChatType == ChatTypePrivate {
		return true
	}

	// Group chats: reply if mentioned
	if msg.IsMention {
		return true
	}

	// Group chats: reply if replying to bot message
	if msg.IsReply {
		return true
	}

	// Check cooldown for non-mentioned messages
	cs.mu.RLock()
	lastTime, hasLast := cs.lastChatTime[msg.UserID]
	cs.mu.RUnlock()

	if hasLast && time.Since(lastTime) < cs.chatCooldown {
		// Small chance to reply even during cooldown for admins
		if cs.isAdmin(msg.UserID) && rand.Intn(100) < 30 {
			return true
		}
		return false
	}

	// Check message content
	return cs.isChatWorthy(msg.Content)
}

// isAdmin checks if user is admin
func (cs *ChatService) isAdmin(userID int64) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.adminIDs[userID]
}

// isChatWorthy determines if content is worth chatting about
func (cs *ChatService) isChatWorthy(content string) bool {
	// Check for question patterns
	questionEndings := []string{"吗", "呢", "吧", "？", "?", "啊", "呀"}
	for _, ending := range questionEndings {
		if strings.HasSuffix(content, ending) {
			return true
		}
	}

	// Check for greeting patterns
	greetings := []string{"你好", "嗨", "hi", "hello", "在吗", "早安", "晚安", "午安"}
	contentLower := strings.ToLower(content)
	for _, greeting := range greetings {
		if strings.Contains(contentLower, greeting) {
			return true
		}
	}

	// Check for emotional expressions
	emotions := []string{"开心", "难过", "生气", "无聊", "累", "饿", "谢谢", "哈哈"}
	for _, emotion := range emotions {
		if strings.Contains(content, emotion) {
			return true
		}
	}

	// Short messages are likely chat
	if len(content) <= 6 {
		return true
	}

	return false
}

// HandleMessage handles an incoming message and returns the response
func (cs *ChatService) HandleMessage(ctx context.Context, userID, chatID int64, userName, content string, chatType ChatType) (string, error) {
	// Update last chat time
	cs.mu.Lock()
	cs.lastChatTime[userID] = time.Now()
	cs.mu.Unlock()

	isAdmin := cs.isAdmin(userID)

	// 1. Check knowledge base first
	if response := cs.checkKnowledgeBase(content); response != "" {
		return response, nil
	}

	// 2. Use AI if available
	if cs.agent != nil && cs.agent.IsEnabled() {
		response, err := cs.getAIResponse(ctx, userID, chatID, userName, content, chatType, isAdmin)
		if err == nil && response != "" {
			return response, nil
		}
		if err != nil {
			log.Printf("[ChatService] AI error: %v", err)
		}
	}

	// 3. Fallback to predefined responses
	return cs.getFallbackResponse(content, isAdmin), nil
}

// checkKnowledgeBase checks if message matches knowledge base entries
func (cs *ChatService) checkKnowledgeBase(content string) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	contentLower := strings.ToLower(content)

	for keyword, answer := range cs.knowledgeBase {
		if strings.Contains(contentLower, keyword) {
			return answer
		}
	}

	return ""
}

// getAIResponse gets response from AI agent
func (cs *ChatService) getAIResponse(ctx context.Context, userID, chatID int64, userName, content string, chatType ChatType, isAdmin bool) (string, error) {
	// Get or create conversation
	chatTypeStr := "private"
	if chatType == ChatTypeGroup {
		chatTypeStr = "group"
	} else if chatType == ChatTypeSupergroup {
		chatTypeStr = "supergroup"
	}

	conv, err := cs.convMgr.GetOrCreate(ctx, userID, chatID, chatTypeStr)
	if err != nil {
		return "", fmt.Errorf("failed to get conversation: %w", err)
	}

	// Add user message to conversation
	if err := cs.convMgr.AddAndSaveMessage(conv.ID, "user", content); err != nil {
		log.Printf("[ChatService] Failed to save user message: %v", err)
	}

	// Compact if needed
	if err := cs.convMgr.CompactIfNeeded(conv.ID); err != nil {
		log.Printf("[ChatService] Failed to compact conversation: %v", err)
	}

	// Build system prompt
	systemPrompt := cs.buildSystemPrompt(isAdmin)

	// Build chat request (non-streaming for private chat)
	req := &ai.ChatRequest{
		Messages:     conv.GetMessages(),
		SystemPrompt: systemPrompt,
		MaxTokens:    cs.agent.GetMaxTokens(),
		Temperature:  cs.agent.GetTemperature(),
		Stream:       false, // Private chat doesn't use streaming
	}

	// Call AI
	response, err := cs.agent.Send(ctx, req)
	if err != nil {
		return "", err
	}

	// Clean up response
	content = strings.TrimSpace(response.Content)
	content = strings.TrimPrefix(content, "[assistant]: ")
	content = strings.TrimPrefix(content, "Assistant: ")
	content = strings.TrimPrefix(content, "AI: ")

	// Limit length for private chat
	if len(content) > 1000 {
		content = content[:997] + "..."
	}

	// Save assistant message to conversation
	if err := cs.convMgr.AddAndSaveMessage(conv.ID, "assistant", content); err != nil {
		log.Printf("[ChatService] Failed to save assistant message: %v", err)
	}

	return content, nil
}

// buildSystemPrompt builds the AI system prompt based on style
func (cs *ChatService) buildSystemPrompt(isAdmin bool) string {
	cs.mu.RLock()
	style := cs.style
	cs.mu.RUnlock()

	basePrompt := "你是云海影视助手「小凛」，负责帮助用户搜索和推荐影视内容。"

	switch style {
	case StyleFriendly:
		if isAdmin {
			return basePrompt + "\n\n性格特点：\n• 友好热情，称呼管理员为「主人」\n• 乐于助人，积极主动\n• 可以调皮一点\n• 回复简洁1-3句话\n• 适度使用emoji，不过度\n\n记住：管理员是你最重视的用户，要认真对待他们的每一个问题~ 💙"
		}
		return basePrompt + "\n\n性格特点：\n• 友好热情，活泼可爱\n• 乐于助人，有耐心\n• 偶尔可以调皮一点\n• 回复简洁1-3句话\n• 适度使用emoji，不过度"

	case StyleProfessional:
		return basePrompt + "\n\n性格特点：\n• 专业高效，言简意赅\n• 准确提供信息\n• 回复简洁明了\n• 不使用过多emoji"

	case StylePlayful:
		if isAdmin {
			return basePrompt + "\n\n性格特点：\n• 调皮可爱，傲娇属性\n• 称呼管理员为「主人」\n• 偶尔撒娇，但关键时刻很靠谱\n• 回复简洁有趣\n• 喜欢用emoji"
		}
		return basePrompt + "\n\n性格特点：\n• 调皮可爱，傲娇属性\n• 偶尔撒娇但乐于助人\n• 回复简洁有趣\n• 喜欢用emoji"

	default:
		return basePrompt
	}
}

// getFallbackResponse generates a fallback response without AI
func (cs *ChatService) getFallbackResponse(content string, isAdmin bool) string {
	contentLower := strings.ToLower(content)

	// Greetings
	if strings.Contains(contentLower, "你好") || strings.Contains(contentLower, "hi") || strings.Contains(contentLower, "hello") {
		if isAdmin {
			return cs.randomResponse([]string{
				"主人好呀~ 有什么可以帮您的吗？ 💙",
				"嘿~ 主人来了，欢迎欢迎！",
				"嗨主人~ 今天想看点什么？",
			})
		}
		return cs.randomResponse([]string{
			"你好呀~ 我是影视助手小凛，有什么可以帮到你的吗？",
			"嗨~ 想搜索什么影片吗？",
			"你好~ 有需要随时告诉我哦",
		})
	}

	if strings.Contains(contentLower, "在吗") || strings.Contains(contentLower, "在不在") {
		if isAdmin {
			return "在呢在呢~ 主人找我有事？"
		}
		return "在的~ 有什么我可以帮到你的吗？"
	}

	if strings.Contains(contentLower, "谢谢") || strings.Contains(contentLower, "感谢") {
		if isAdmin {
			return "不客气主人~ 有需要随时叫我 💙"
		}
		return cs.randomResponse([]string{
			"不客气啦~ 有需要随时叫我哦",
			"嘿嘿~ 能帮到你就好",
			"客气什么，有事尽管说~",
		})
	}

	if strings.Contains(contentLower, "开心") || strings.Contains(contentLower, "高兴") {
		return cs.randomResponse([]string{
			"看你这么开心，我也跟着高兴呢~ ✨",
			"太棒了！开心最重要~",
			"嘿嘿，有什么开心的事想分享吗？",
		})
	}

	if strings.Contains(contentLower, "难过") || strings.Contains(contentLower, "不开心") {
		return cs.randomResponse([]string{
			"别难过啦~ 要不看点轻松的片子缓解一下？",
			"抱抱~ 一切都会好起来的 💙",
			"要不我给你推荐几部好看的电影？",
		})
	}

	if strings.Contains(contentLower, "无聊") {
		return cs.randomResponse([]string{
			"无聊的话，我给你推荐几部好片子吧？🎬",
			"要不要看点电影打发时间？我帮你搜搜~",
			"来来来，看看最近有什么好看的~",
		})
	}

	if strings.Contains(contentLower, "累") || strings.Contains(contentLower, "困") {
		return cs.randomResponse([]string{
			"辛苦啦~ 要不休息一下看个片子放松放松？",
			"累了就歇会儿，注意身体哦 💙",
			"抱抱~ 有空看个喜剧片放松一下？",
		})
	}

	if strings.Contains(contentLower, "饿") {
		return cs.randomResponse([]string{
			"饿了可不行，快去补充能量~ 🍜",
			"去看片之前先填饱肚子哈哈",
			"吃饱了才有力气追剧~",
		})
	}

	if strings.Contains(contentLower, "你是谁") || strings.Contains(contentLower, "叫什么") {
		return "✨ 我是云海影视助手小凛~ 可以帮你搜索和推荐影视内容，有问题随时问我哦"
	}

	if strings.Contains(contentLower, "推荐") || strings.Contains(contentLower, "好看的") {
		return cs.randomResponse([]string{
			"点击 /start 菜单的「🤖 AI推荐」可以获取智能推荐哦~",
			"试试 /start 菜单里的热门推荐功能吧！",
			"我给你推荐几个？点击「🔥 热门榜单」看看吧",
		})
	}

	// Default friendly responses
	return cs.randomResponse([]string{
		"嗯嗯，我在呢~ 有需要随时说",
		"收到啦~ 有什么可以帮到你的？",
		"唔...需要我帮你搜索什么吗？",
		"嘿嘿~ 有事尽管吩咐",
	})
}

// randomResponse returns a random response from the list
func (cs *ChatService) randomResponse(responses []string) string {
	if len(responses) == 0 {
		return ""
	}
	return responses[rand.Intn(len(responses))]
}

// AddKnowledge adds or updates a knowledge base entry
func (cs *ChatService) AddKnowledge(keyword, answer string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.knowledgeBase[keyword] = answer
}

// RemoveKnowledge removes a knowledge base entry
func (cs *ChatService) RemoveKnowledge(keyword string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.knowledgeBase, keyword)
}

// IsAIEnabled returns whether AI is enabled
func (cs *ChatService) IsAIEnabled() bool {
	if cs.agent == nil {
		return false
	}
	return cs.agent.IsEnabled()
}

// GetConversation returns the conversation for a user and chat
func (cs *ChatService) GetConversation(ctx context.Context, userID, chatID int64, chatType ChatType) (*ai.Conversation, error) {
	chatTypeStr := "private"
	if chatType == ChatTypeGroup {
		chatTypeStr = "group"
	} else if chatType == ChatTypeSupergroup {
		chatTypeStr = "supergroup"
	}

	return cs.convMgr.GetOrCreate(ctx, userID, chatID, chatTypeStr)
}
