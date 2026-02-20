// Package ai provides AI agent integration for the bot
package ai

import (
	"fmt"
	"strings"
	"sync"
)

// AIClient represents the interface for AI clients
type AIClient interface {
	Send(userMessage string, systemPrompt string) (string, error)
	IsEnabled() bool
}

// Message represents a unified message type
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Agent represents the AI agent coordinator
type Agent struct {
	client       AIClient
	recommend    *MediaRecommendationAI
	search       *SearchAI
	conversations map[int64]*Conversation // user_id -> conversation
	mu           sync.RWMutex
	enabled      bool
}

// Conversation represents a conversation with a user
type Conversation struct {
	UserID    int64
	Messages  []Message
	StartTime int64
	Context   map[string]string
}

// NewAgent creates a new AI agent
func NewAgent(apiKey string) *Agent {
	// Try Zhipu AI first
	zhipu := NewZhipuClient(apiKey)
	if zhipu.IsEnabled() {
		return &Agent{
			client:        zhipu,
			recommend:     NewMediaRecommendationAIWithZhipu(zhipu),
			search:        NewSearchAIWithZhipu(zhipu),
			conversations: make(map[int64]*Conversation),
			enabled:       true,
		}
	}

	// Fallback to Claude
	claude := NewClaudeClient(apiKey)
	return &Agent{
		client:        claude,
		recommend:     NewMediaRecommendationAI(claude),
		search:        NewSearchAI(claude),
		conversations: make(map[int64]*Conversation),
		enabled:       claude.IsEnabled(),
	}
}

// IsEnabled returns whether the AI agent is enabled
func (a *Agent) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// GetClient returns the AI client
func (a *Agent) GetClient() AIClient {
	return a.client
}

// GetRecommendation returns the recommendation engine
func (a *Agent) GetRecommendation() *MediaRecommendationAI {
	return a.recommend
}

// GetSearch returns the search AI
func (a *Agent) GetSearch() *SearchAI {
	return a.search
}

// ProcessMessage processes a user message and returns an AI response
func (a *Agent) ProcessMessage(userID int64, message string) (string, error) {
	if !a.enabled {
		return "", fmt.Errorf("AI agent is not enabled")
	}

	// Get or create conversation
	conv := a.getConversation(userID)
	conv.Messages = append(conv.Messages, Message{
		Role:    "user",
		Content: message,
	})

	systemPrompt := a.buildSystemPrompt()

	// Build conversation context for sending
	var contextBuilder strings.Builder
	for i, msg := range conv.Messages {
		if i > 0 {
			contextBuilder.WriteString("\n")
		}
		contextBuilder.WriteString(fmt.Sprintf("[%s]: %s", msg.Role, msg.Content))
	}

	// Send to AI with conversation context
	response, err := a.client.Send(contextBuilder.String(), systemPrompt)
	if err != nil {
		return "", err
	}

	// Add assistant response to conversation
	conv.Messages = append(conv.Messages, Message{
		Role:    "assistant",
		Content: response,
	})

	// Limit conversation history
	if len(conv.Messages) > 20 {
		conv.Messages = conv.Messages[len(conv.Messages)-20:]
	}

	return response, nil
}

// ClearConversation clears a user's conversation history
func (a *Agent) ClearConversation(userID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.conversations, userID)
}

// getConversation gets or creates a conversation for a user
func (a *Agent) getConversation(userID int64) *Conversation {
	a.mu.Lock()
	defer a.mu.Unlock()

	conv, exists := a.conversations[userID]
	if !exists {
		conv = &Conversation{
			UserID:    userID,
			Messages:  []Message{},
			StartTime: 0,
			Context:   make(map[string]string),
		}
		a.conversations[userID] = conv
	}

	return conv
}

// buildSystemPrompt builds the system prompt for the AI agent
func (a *Agent) buildSystemPrompt() string {
	return `你是云海看板娘的 AI 助手，帮助用户发现和请求影视内容。

你的能力：
1. 推荐电影和剧集
2. 回答影视相关的问题
3. 帮助用户搜索内容
4. 解释剧情（不剧透）

回复风格：
- 友好、简洁
- 使用 emoji 表情
- 中文回复
- 不确定时诚实告知

如果用户想要搜索或请求内容，建议他们使用搜索功能。
如果用户请求推荐，请推荐合适的影视内容。`
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
		"enabled": a.enabled,
		"conversations": len(a.conversations),
	}

	if a.recommend != nil {
		stats["hasRecommendation"] = true
	}
	if a.search != nil {
		stats["hasSearch"] = true
	}

	return stats
}
