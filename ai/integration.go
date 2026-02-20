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
)

// Initialize initializes the global AI manager
func Initialize(apiKey string) *Manager {
	once.Do(func() {
		globalManager = &Manager{
			agent:   NewAgent(apiKey),
			enabled: NewAgent(apiKey).IsEnabled(),
		}
	})
	return globalManager
}

// GetManager returns the global AI manager
func GetManager() *Manager {
	if globalManager == nil {
		// Try to initialize from environment -优先使用智谱 AI
		apiKey := os.Getenv("ZHIPU_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("CLAUDE_API_KEY")
		}
		if apiKey == "" {
			// Read from .env file
			data, err := os.ReadFile("/root/emby-telegram-bot/.env")
			if err == nil {
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					if strings.HasPrefix(line, "ZHIPU_API_KEY=") {
						apiKey = strings.TrimPrefix(line, "ZHIPU_API_KEY=")
						apiKey = strings.TrimSpace(apiKey)
						break
					}
					if apiKey == "" && strings.HasPrefix(line, "CLAUDE_API_KEY=") {
						apiKey = strings.TrimPrefix(line, "CLAUDE_API_KEY=")
						apiKey = strings.TrimSpace(apiKey)
					}
				}
			}
		}
		agent := NewAgent(apiKey)
		globalManager = &Manager{
			agent:   agent,
			enabled: agent.IsEnabled(),
		}
	}
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
		return "🤖 AI 功能暂未启用\n\n💡 请在服务器上配置 ZHIPU_API_KEY 环境变量", nil
	}

	return manager.GetAgent().HandleAICommand(userID, args)
}

// GetAIRecommendations gets AI-powered recommendations
func GetAIRecommendations(mood string, count int) (string, error) {
	manager := GetManager()
	if !manager.IsEnabled() {
		return "", fmt.Errorf("AI is not enabled")
	}

	results, err := manager.GetAgent().GetRecommendation().GetMoodBasedRecommendations(mood, count)
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

	results, err := manager.GetAgent().GetRecommendation().GetSimilarRecommendations(title, mediaType, count)
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
	sb.WriteString("🤖 **AI 智能推荐**\n\n")

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. 🎬 **%s**", i+1, r.Title))
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
