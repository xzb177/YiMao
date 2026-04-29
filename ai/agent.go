// Package ai provides AI agent orchestration
package ai

import (
	"fmt"
	"os"
	"sync"

	"github.com/xzb177/yimao/pkg/logger"
)

// Agent orchestrates all AI capabilities
type Agent struct {
	claude    *ClaudeClient
	zhipu     *ZhipuClient
	recommend *MediaRecommendationAI
	search    *SearchAI
	mu        sync.RWMutex
	enabled   bool
}

// NewAgent creates a new AI agent.
// Priority: OPENAI_BASE_URL (proxy) > ZHIPU_API_KEY > CLAUDE_API_KEY
func NewAgent(apiKey string) *Agent {
	// Priority 1: OpenAI-compatible proxy (e.g. one-api, new-api)
	openaiKey := os.Getenv("OPENAI_API_KEY")
	openaiBase := os.Getenv("OPENAI_BASE_URL")
	if openaiKey != "" && openaiBase != "" {
		claude := NewClaudeClient("") // Will pick up OPENAI_* env vars
		if claude.IsEnabled() {
			logger.Info("[AI] Using OpenAI-compatible proxy: %s", openaiBase)
			return &Agent{
				claude:    claude,
				recommend: NewMediaRecommendationAI(claude),
				search:    NewSearchAI(claude),
				enabled:   true,
			}
		}
	}

	// Priority 2: Zhipu AI
	zhipu := NewZhipuClient("")
	if zhipu.IsEnabled() {
		return &Agent{
			zhipu:    zhipu,
			recommend: NewMediaRecommendationAIWithZhipu(zhipu),
			search:    NewSearchAIWithZhipu(zhipu),
			enabled:   true,
		}
	}

	// Priority 3: Native Anthropic API
	claudeKey := os.Getenv("ANTHROPIC_API_KEY")
	if claudeKey == "" {
		claudeKey = os.Getenv("CLAUDE_API_KEY")
	}
	if claudeKey != "" {
		claude := NewClaudeClient(claudeKey)
		if claude.IsEnabled() {
			return &Agent{
				claude:    claude,
				recommend: NewMediaRecommendationAI(claude),
				search:    NewSearchAI(claude),
				enabled:   true,
			}
		}
	}

	return &Agent{
		enabled: false,
	}
}

// IsEnabled returns whether the agent is enabled
func (a *Agent) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// GetRecommendation returns the AI recommendation service
func (a *Agent) GetRecommendation() *MediaRecommendationAI {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.recommend
}

// GetSearch returns the AI search service
func (a *Agent) GetSearch() *SearchAI {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.search
}

// HandleAICommand handles the /ai command
func (a *Agent) HandleAICommand(userID int64, args string) (string, error) {
	if !a.IsEnabled() {
		return "🤖 AI 功能暂未启用\n\n💡 请在 .env 文件中配置 ZHIPU_API_KEY 或 CLAUDE_API_KEY", nil
	}

	if args == "" {
		return "🤖 **AI 智能助手**\n\n" +
			"💫 **心情推荐**\n" +
			"使用「心情关键词」获取推荐：\n" +
			"• 开心、难过、治愈、放松\n" +
			"• 紧张、兴奋、思考、浪漫\n" +
			"• 怀旧、无聊、孤独、困倦\n\n" +
			"🎬 **使用方法**\n" +
			"直接告诉我你现在的心情！\n" +
			"例如：「我想看治愈的剧」", nil
	}

	// Treat args as mood preference
	results, err := a.recommend.GetMoodBasedRecommendations(args, 3)
	if err != nil {
		return "", fmt.Errorf("获取推荐失败: %w", err)
	}

	return formatRecommendations(results), nil
}

// GetStats returns agent statistics
func (a *Agent) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := map[string]interface{}{
		"enabled": a.enabled,
	}

	if a.zhipu != nil {
		stats["zhipu"] = a.zhipu.GetStats()
	}
	if a.claude != nil {
		stats["claude"] = a.claude.GetStats()
	}

	return stats
}
