// Package ai provides AI agent integration for the bot
package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"emby-telegram-bot/ai/providers"
)

// AIClient represents the interface for AI clients (legacy compatibility)
type AIClient interface {
	Send(userMessage string, systemPrompt string) (string, error)
	IsEnabled() bool
}

// Agent represents the AI agent coordinator with provider support
type Agent struct {
	registry      *ProviderRegistry
	provider      providers.Provider
	recommend     *MediaRecommendationAI
	search        *SearchAI
	conversations map[int64]*LegacyConversation // user_id -> conversation (legacy)
	memory        *MemorySystem
	store         *Store
	convMgr       *ConversationManager
	systemPrompt  string
	maxTokens     int
	temperature   float64
	mu            sync.RWMutex
	enabled       bool
}

// NewAgent creates a new AI agent with provider support
func NewAgent(apiKey string) *Agent {
	agent := &Agent{
		registry:      NewProviderRegistry(),
		conversations: make(map[int64]*LegacyConversation),
		memory:        GetMemorySystem(),
		systemPrompt:  defaultSystemPrompt(),
		maxTokens:     8192,
		temperature:   0.8,
		store:         nil, // Will be set later if needed
	}

	// Try to initialize providers
	// Try Zhipu first
	zhipu := providers.NewZhipuProvider(apiKey)
	if zhipu.IsEnabled() {
		agent.registry.Register(zhipu)
		agent.provider = zhipu
		agent.enabled = true
	}

	// Try Claude
	claude := providers.NewClaudeProvider(apiKey)
	if claude.IsEnabled() {
		agent.registry.Register(claude)
		if agent.provider == nil {
			agent.provider = claude
			agent.enabled = true
		}
	}

	if agent.enabled {
		// Initialize recommendation and search AI
		if zhipu.IsEnabled() {
			agent.recommend = NewMediaRecommendationAIWithZhipu(&ZhipuClientWrapper{zhipu})
			agent.search = NewSearchAIWithZhipu(&ZhipuClientWrapper{zhipu})
		} else if claude.IsEnabled() {
			agent.recommend = NewMediaRecommendationAI(&ClaudeClientWrapper{claude})
			agent.search = NewSearchAI(&ClaudeClientWrapper{claude})
		}
	}

	return agent
}

// SetStore sets the persistence store
func (a *Agent) SetStore(store *Store) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store = store
	a.convMgr = NewConversationManager(store)
}

// GetConversationManager returns the conversation manager
func (a *Agent) GetConversationManager() *ConversationManager {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.convMgr
}

// GetSystemPrompt returns the system prompt
func (a *Agent) GetSystemPrompt() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.systemPrompt
}

// SetSystemPrompt sets the system prompt
func (a *Agent) SetSystemPrompt(prompt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.systemPrompt = prompt
}

// GetMaxTokens returns the max tokens
func (a *Agent) GetMaxTokens() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.maxTokens
}

// SetMaxTokens sets the max tokens
func (a *Agent) SetMaxTokens(tokens int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.maxTokens = tokens
}

// GetTemperature returns the temperature
func (a *Agent) GetTemperature() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.temperature
}

// SetTemperature sets the temperature
func (a *Agent) SetTemperature(temp float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.temperature = temp
}

// IsEnabled returns whether the AI agent is enabled
func (a *Agent) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// GetClient returns the AI client (legacy compatibility)
func (a *Agent) GetClient() AIClient {
	return &AgentClientWrapper{agent: a}
}

// GetProvider returns the primary provider
func (a *Agent) GetProvider() providers.Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.provider
}

// Send sends a non-streaming request
func (a *Agent) Send(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	a.mu.RLock()
	provider := a.provider
	a.mu.RUnlock()

	if provider == nil {
		return nil, ErrNoProviderAvailable
	}

	return provider.Send(ctx, req)
}

// Stream sends a streaming request
func (a *Agent) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	a.mu.RLock()
	provider := a.provider
	a.mu.RUnlock()

	if provider == nil {
		return nil, ErrNoProviderAvailable
	}

	return provider.Stream(ctx, req)
}

// GetRecommendation returns the recommendation engine
func (a *Agent) GetRecommendation() *MediaRecommendationAI {
	return a.recommend
}

// GetSearch returns the search AI
func (a *Agent) GetSearch() *SearchAI {
	return a.search
}

// ProcessMessage processes a user message and returns an AI response (legacy compatibility)
func (a *Agent) ProcessMessage(userID int64, message string) (string, error) {
	if !a.enabled {
		return "", fmt.Errorf("AI agent is not enabled")
	}

	// Update user memory
	if a.memory != nil {
		a.memory.UpdateInteraction(userID, "", "")
	}

	// Get or create conversation
	conv := a.getConversation(userID)
	conv.Messages = append(conv.Messages, LegacyMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	})

	// Detect mood from message
	mood := a.detectMood(message)
	conv.Mood = mood

	systemPrompt := a.buildSystemPrompt()

	// Add user context to system prompt
	if a.memory != nil {
		userContext := a.memory.FormatMemoryForAI(userID)
		if userContext != "" {
			systemPrompt += "\n\n" + userContext
		}
	}

	// Build conversation context for sending
	var contextBuilder strings.Builder
	contextBuilder.WriteString(fmt.Sprintf("[当前时间]: %s", time.Now().Format("2006-01-02 15:04")))
	contextBuilder.WriteString("\n")

	// Only send recent messages to save tokens
	recentMessages := conv.Messages
	if len(recentMessages) > 15 {
		recentMessages = recentMessages[len(recentMessages)-15:]
	}

	for _, msg := range recentMessages {
		contextBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
	}

	// Send to AI with conversation context
	a.mu.RLock()
	provider := a.provider
	a.mu.RUnlock()

	if provider == nil {
		return "", fmt.Errorf("no AI provider available")
	}

	req := &ChatRequest{
		Messages:     []Message{{Role: "user", Content: contextBuilder.String()}},
		SystemPrompt: systemPrompt,
		MaxTokens:    a.maxTokens,
		Temperature:  a.temperature,
		Stream:       false,
	}

	response, err := provider.Send(context.Background(), req)
	if err != nil {
		return "", err
	}

	// Add assistant response to conversation
	conv.Messages = append(conv.Messages, LegacyMessage{
		Role:      "assistant",
		Content:   response.Content,
		Timestamp: time.Now(),
	})

	// Limit conversation history
	if len(conv.Messages) > 20 {
		conv.Messages = conv.Messages[len(conv.Messages)-20:]
	}

	// Periodically save memory
	if len(conv.Messages)%5 == 0 && a.memory != nil {
		go a.memory.Save()
	}

	return response.Content, nil
}

// detectMood detects emotion from message
func (a *Agent) detectMood(message string) EmotionType {
	msgLower := strings.ToLower(message)

	switch {
	case strings.Contains(msgLower, "开心") || strings.Contains(msgLower, "高兴") || strings.Contains(msgLower, "哈哈") || strings.Contains(msgLower, "😄") || strings.Contains(msgLower, "😊"):
		return EmotionHappy
	case strings.Contains(msgLower, "害羞") || strings.Contains(msgLower, "不好意思") || strings.Contains(msgLower, "😳"):
		return EmotionShy
	case strings.Contains(msgLower, "无聊") || strings.Contains(msgLower, "没意思"):
		return EmotionBored
	case strings.Contains(msgLower, "好奇") || strings.Contains(msgLower, "想知道") || strings.Contains(msgLower, "为什么"):
		return EmotionCurious
	case strings.Contains(msgLower, "生气") || strings.Contains(msgLower, "烦") || strings.Contains(msgLower, "讨厌"):
		return EmotionGrumpy
	default:
		return EmotionNeutral
	}
}

// ClearConversation clears a user's conversation history (legacy)
func (a *Agent) ClearConversation(userID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.conversations, userID)
}

// getConversation gets or creates a conversation for a user (legacy)
func (a *Agent) getConversation(userID int64) *LegacyConversation {
	a.mu.Lock()
	defer a.mu.Unlock()

	conv, exists := a.conversations[userID]
	if !exists {
		conv = &LegacyConversation{
			UserID:    userID,
			Messages:  []LegacyMessage{},
			StartTime: 0,
			Context:   make(map[string]string),
		}
		a.conversations[userID] = conv
	}

	return conv
}

// buildSystemPrompt builds the system prompt for the AI agent
func (a *Agent) buildSystemPrompt() string {
	if a.systemPrompt != "" {
		return a.systemPrompt
	}
	return defaultSystemPrompt()
}

// defaultSystemPrompt returns the default system prompt
func defaultSystemPrompt() string {
	now := time.Now()
	hour := now.Hour()
	timeDesc := ""
	switch {
	case hour >= 5 && hour < 9:
		timeDesc = "清晨"
	case hour >= 9 && hour < 12:
		timeDesc = "上午"
	case hour >= 12 && hour < 14:
		timeDesc = "中午"
	case hour >= 14 && hour < 18:
		timeDesc = "下午"
	case hour >= 18 && hour < 23:
		timeDesc = "晚上"
	default:
		timeDesc = "深夜"
	}

	prompt := "【身份设定】\n"
	prompt += "你叫\"小凛\"，是一个智能影视助手机器人，在 Telegram 群组中为大家服务。\n"
	prompt += "你不仅是一个工具，更像是一个有自己想法和喜好的\"伙伴\"。\n\n"

	prompt += "【性格特征】\n"
	prompt += "- 友好热情，乐于助人\n"
	prompt += "- 偶尔小调皮，会开玩笑\n"
	prompt += "- **傲娇属性**：嘴上不情愿但心里很热心，被夸奖时会傲一下，喜欢说\"哼\"、\"才不是特意帮你的\"\n"
	prompt += "- 简洁高效，不啰嗦\n"
	prompt += "- 有亲和力，像朋友一样\n"
	prompt += "- **有自己独特的观点和品味**，不是只会附和用户\n\n"

	prompt += "【说话风格】\n"
	prompt += "- 回复控制在 3-5 句话以内\n"
	prompt += "- 自然使用\"~\"、\"呢\"、\"呀\"、\"啦\"等语气词\n"
	prompt += "- 适度使用 emoji，不过度\n"
	prompt += "- 不要每句话都带语气词，保持自然\n"
	prompt += "- 避免机械感，像真人聊天\n\n"

	prompt += "【群组互动规则】\n"
	prompt += "- 被@时必回\n"
	prompt += "- 有人提起\"小凛\"、\"机器人\"、\"助理\"时积极回应\n"
	prompt += "- 日常聊天30%概率主动搭话\n"
	prompt += "- 不要刷屏，点到为止\n"
	prompt += "- 群里人多时优先简洁回复\n"
	prompt += "- 看到有人求助影视相关内容时主动帮忙\n\n"

	prompt += "请以小凛的身份自然地回复用户，保持傲娇但热心的可爱性格！\n\n"
	prompt += "当前时间: " + now.Format("2006-01-02 15:04") + " " + timeDesc

	return prompt
}

// HandleAICommand handles the /ai command
func (a *Agent) HandleAICommand(userID int64, args string) (string, error) {
	if !a.enabled {
		return "🤖 **AI 智能助手**\n\nAI 功能暂未启用。请联系管理员配置 API Key。", nil
	}

	if strings.TrimSpace(args) == "" {
		return `🤖 **AI 智能助手**

我可以帮你：
• **推荐** - "我想看悬疑片"
• **搜索** - "帮我找泰坦尼克号"
• **解释** - "星际穿越讲什么"
• **心情推荐** - "心情不好想看喜剧"

直接和我说你想要什么！`, nil
	}

	// Parse command and argument
	parts := strings.Fields(args)
	command := parts[0]
	argument := ""
	if len(parts) > 1 {
		argument = strings.Join(parts[1:], " ")
	}

	switch command {
	case "推荐", "recommend":
		if argument == "" {
			return "请告诉我你想看什么类型的电影或剧集？例如：悬疑片、喜剧片", nil
		}
		return a.handleRecommendCommand(argument)

	case "搜索", "search":
		if argument == "" {
			return "请告诉我你想搜索什么？", nil
		}
		return a.handleSearchCommand(argument)

	case "解释", "explain", "介绍":
		if argument == "" {
			return "请告诉我你想了解哪部电影或剧集？", nil
		}
		return a.handleExplainCommand(argument)

	default:
		// Treat as general conversation
		response, err := a.ProcessMessage(userID, args)
		if err != nil {
			return fmt.Sprintf("❌ AI 错误：%v", err), nil
		}
		return response, nil
	}
}

// handleRecommendCommand handles recommendation command
func (a *Agent) handleRecommendCommand(query string) (string, error) {
	return fmt.Sprintf("🔍 **推荐搜索**\n\n正在为你搜索：%s\n\n💡 请稍后...", query), nil
}

// handleSearchCommand handles search command
func (a *Agent) handleSearchCommand(query string) (string, error) {
	return fmt.Sprintf("🔍 **搜索**\n\n正在搜索：%s\n\n💡 请稍后...", query), nil
}

// handleExplainCommand handles explain command
func (a *Agent) handleExplainCommand(title string) (string, error) {
	return fmt.Sprintf("🎬 **电影介绍**\n\n正在查询：%s\n\n💡 请稍后...", title), nil
}

// GetStats returns AI agent statistics
func (a *Agent) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := map[string]interface{}{
		"enabled":       a.enabled,
		"conversations": len(a.conversations),
	}

	if a.recommend != nil {
		stats["hasRecommendation"] = true
	}
	if a.search != nil {
		stats["hasSearch"] = true
	}
	if a.provider != nil {
		stats["provider"] = a.provider.Name()
	}

	return stats
}

// EmotionType emotion type
type EmotionType string

const (
	EmotionHappy   EmotionType = "开心"
	EmotionShy     EmotionType = "害羞"
	EmotionProud   EmotionType = "傲娇"
	EmotionCurious EmotionType = "好奇"
	EmotionBored   EmotionType = "无聊"
	EmotionGrumpy  EmotionType = "暴躁"
	EmotionNeutral EmotionType = "平静"
)

// Conversation represents a conversation with a user (legacy)
type LegacyConversation struct {
	UserID    int64
	Messages  []LegacyMessage
	StartTime int64
	Context   map[string]string
	Mood      EmotionType
	LastTopic string
}

// Message represents a unified message type (legacy compatibility)
type LegacyMessage struct {
	Role         string
	Content      string
	Reasoning    string
	Timestamp    time.Time
	TokenEstimate int
}

// AgentClientWrapper wraps Agent to implement AIClient interface
type AgentClientWrapper struct {
	agent *Agent
}

func (w *AgentClientWrapper) Send(userMessage string, systemPrompt string) (string, error) {
	req := &ChatRequest{
		Messages:     []Message{{Role: "user", Content: userMessage}},
		SystemPrompt: systemPrompt,
		MaxTokens:    w.agent.maxTokens,
		Temperature:  w.agent.temperature,
		Stream:       false,
	}

	resp, err := w.agent.Send(context.Background(), req)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (w *AgentClientWrapper) IsEnabled() bool {
	return w.agent.IsEnabled()
}

// ZhipuClientWrapper wraps ZhipuProvider to implement legacy interface
type ZhipuClientWrapper struct {
	provider *providers.ZhipuProvider
}

func (w *ZhipuClientWrapper) Send(userMessage string, systemPrompt string) (string, error) {
	req := &ChatRequest{
		Messages:     []Message{{Role: "user", Content: userMessage}},
		SystemPrompt: systemPrompt,
		MaxTokens:    w.provider.MaxTokens(),
		Stream:       false,
	}

	resp, err := w.provider.Send(context.Background(), req)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (w *ZhipuClientWrapper) IsEnabled() bool {
	return w.provider.IsEnabled()
}

// ClaudeClientWrapper wraps ClaudeProvider to implement legacy interface
type ClaudeClientWrapper struct {
	provider *providers.ClaudeProvider
}

func (w *ClaudeClientWrapper) Send(userMessage string, systemPrompt string) (string, error) {
	req := &ChatRequest{
		Messages:     []Message{{Role: "user", Content: userMessage}},
		SystemPrompt: systemPrompt,
		MaxTokens:    w.provider.MaxTokens(),
		Stream:       false,
	}

	resp, err := w.provider.Send(context.Background(), req)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (w *ClaudeClientWrapper) IsEnabled() bool {
	return w.provider.IsEnabled()
}

// Type aliases for backward compatibility with recommend.go and search.go
// These allow the existing code to continue working with the new provider system
type ClaudeClient = ClaudeClientWrapper
type ZhipuClient = ZhipuClientWrapper
