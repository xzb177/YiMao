// Package ai provides integration layer for the AI module
package ai

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Manager manages all AI components
type Manager struct {
	agent   *Agent
	enabled bool
	mu      sync.RWMutex
}

var (
	globalManager *Manager
	once          sync.Once
	managerMu     sync.RWMutex
)

// Initialize initializes the global AI manager
func Initialize(apiKey string) *Manager {
	once.Do(func() {
		agent := NewAgent(apiKey)
		mgr := &Manager{
			agent:   agent,
			enabled: agent.IsEnabled(),
		}
		managerMu.Lock()
		globalManager = mgr
		managerMu.Unlock()
	})
	managerMu.RLock()
	defer managerMu.RUnlock()
	return globalManager
}

// GetManager returns the global AI manager
func GetManager() *Manager {
	managerMu.RLock()
	if globalManager != nil {
		managerMu.RUnlock()
		return globalManager
	}
	managerMu.RUnlock()

	// 支持手动声明 AI 可用（适用于 Mimo 等非 Zhipu/Claude 提供商）
	// 设 AI_ENABLED=true 即可让 AI 按钮显示，实际调用走 minis-model-use 代理。
	if os.Getenv("AI_ENABLED") == "true" {
		mgr := &Manager{
			agent:   &Agent{},
			enabled: true,
		}
		managerMu.Lock()
		if globalManager == nil {
			globalManager = mgr
		}
		managerMu.Unlock()
		managerMu.RLock()
		defer managerMu.RUnlock()
		return globalManager
	}

	// Try to initialize from environment - 优先使用智谱 AI
	apiKey := os.Getenv("ZHIPU_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}
	if apiKey == "" {
		// Read from .env file (try multiple paths)
		for _, path := range []string{"/root/YiMao/.env", ".env", "/app/data/.env"} {
			if data, err := os.ReadFile(path); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "ZHIPU_API_KEY=") {
						apiKey = strings.TrimSpace(strings.TrimPrefix(line, "ZHIPU_API_KEY="))
						break
					}
					if apiKey == "" && strings.HasPrefix(line, "CLAUDE_API_KEY=") {
						apiKey = strings.TrimSpace(strings.TrimPrefix(line, "CLAUDE_API_KEY="))
					}
				}
				if apiKey != "" {
					break
				}
			}
		}
	}

	agent := NewAgent(apiKey)
	mgr := &Manager{
		agent:   agent,
		enabled: agent.IsEnabled(),
	}

	managerMu.Lock()
	// Double-check in case another goroutine initialized first
	if globalManager == nil {
		globalManager = mgr
	}
	managerMu.Unlock()

	managerMu.RLock()
	defer managerMu.RUnlock()
	return globalManager
}

// IsEnabled returns whether AI is enabled
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// GetAgent returns the AI agent
func (m *Manager) GetAgent() *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agent
}

// HandleAICommand handles the /ai command
func HandleAICommand(userID int64, args string) (string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return "🤖 AI 功能暂未启用\n\n💡 请联系管理员配置 AI_ENABLED=true，并设置 ZHIPU_API_KEY / CLAUDE_API_KEY / ANTHROPIC_API_KEY 之一", nil
	}

	return manager.GetAgent().HandleAICommand(userID, args)
}

// GetAIRecommendations gets AI-powered recommendations
func GetAIRecommendations(mood string, count int) (string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return "", fmt.Errorf("AI is not enabled")
	}

	rec := manager.GetAgent().GetRecommendation()
	if rec == nil {
		return "", fmt.Errorf("AI recommendation service not initialized — please configure ZHIPU_API_KEY, CLAUDE_API_KEY, or ANTHROPIC_API_KEY")
	}

	results, err := rec.GetMoodBasedRecommendations(mood, count)
	if err != nil {
		return "", err
	}

	return formatRecommendations(results), nil
}

// GetAISimilarRecommendations gets recommendations similar to a title
func GetAISimilarRecommendations(title, mediaType string, count int) (string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return "", fmt.Errorf("AI is not enabled")
	}

	rec := manager.GetAgent().GetRecommendation()
	if rec == nil {
		return "", fmt.Errorf("AI recommendation service not initialized — please configure ZHIPU_API_KEY, CLAUDE_API_KEY, or ANTHROPIC_API_KEY")
	}

	results, err := rec.GetSimilarRecommendations(title, mediaType, count)
	if err != nil {
		return "", err
	}

	return formatRecommendations(results), nil
}

// ParseNaturalLanguageSearch parses a natural language search query
func ParseNaturalLanguageSearch(query string) (string, string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return query, "", nil
	}

	parsed, err := manager.GetAgent().GetSearch().ParseNaturalLanguageQuery(query)
	if err != nil {
		return query, "", nil
	}

	searchTerm := parsed.SearchTerm
	mediaType := parsed.MediaType

	return searchTerm, mediaType, nil
}

// AnswerQuestion answers a user's question about media
func AnswerQuestion(question string) (string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return "", fmt.Errorf("AI is not enabled")
	}

	return manager.GetAgent().GetSearch().AnswerQuestion(question)
}

// ExplainMovie explains what a movie is about
func ExplainMovie(title string) (string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return "", fmt.Errorf("AI is not enabled")
	}

	return manager.GetAgent().GetRecommendation().ExplainMovie(title)
}

// GetStats returns AI statistics
func GetStats() map[string]interface{} {
	manager := GetManager()
	if manager == nil {
		return map[string]interface{}{"enabled": false}
	}
	return manager.GetAgent().GetStats()
}

// formatRecommendations formats recommendation results
func formatRecommendations(results []*RecommendationResult) string {
	if len(results) == 0 {
		return "😕 没有找到合适的推荐"
	}

	var sb strings.Builder
	sb.WriteString("🤖 <b>AI 智能推荐</b>\n\n")

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. 🎬 <b>%s</b>", i+1, r.Title))
		if r.Year > 0 {
			sb.WriteString(fmt.Sprintf(" (%d)", r.Year))
		}
		sb.WriteString("\n")
		if r.Genre != "" {
			sb.WriteString(fmt.Sprintf("   🎭 %s", r.Genre))
		}
		if r.MediaType != "" {
			mediaType := "🎬 电影"
			if r.MediaType == "tv" {
				mediaType = "📺 剧集"
			}
			sb.WriteString(fmt.Sprintf(" · %s", mediaType))
		}
		sb.WriteString("\n")
		if r.Reason != "" {
			sb.WriteString(fmt.Sprintf("   💡 %s\n", r.Reason))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
