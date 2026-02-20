package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"

	"emby-telegram-bot/callback"
)

// RecommendationBridge provides bridge functions for the recommendation system
type RecommendationBridge struct {
	getRecommendationsFunc func(userID int64, recType string, limit int) []RecommendationItem
	recordActionFunc       func(userID int64, action, tmdbID, mediaType string)

	mu sync.RWMutex
}

var recommendationBridge *RecommendationBridge

// RecommendationItem represents a recommended media item
type RecommendationItem struct {
	TmdbID        int     `json:"tmdbId"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"originalTitle"`
	ReleaseDate   string  `json:"releaseDate"`
	PosterPath    string  `json:"posterPath"`
	VoteAverage   float64 `json:"voteAverage"`
	MediaType     string  `json:"mediaType"`
	Overview      string  `json:"overview"`
	GenreNames    []string `json:"genreNames"`
	Reason        string  `json:"reason"`
	Score         float64 `json:"score"`
}

// InitRecommendationBridge initializes the recommendation bridge
func InitRecommendationBridge() {
	recommendationBridge = &RecommendationBridge{}
	log.Println("[RecommendationBridge] Initialized")
}

// SetGetRecommendationsFunc sets the get recommendations function
func SetGetRecommendationsFunc(fn func(userID int64, recType string, limit int) []RecommendationItem) {
	if recommendationBridge != nil {
		recommendationBridge.mu.Lock()
		recommendationBridge.getRecommendationsFunc = fn
		recommendationBridge.mu.Unlock()
	}
}

// SetRecordActionFunc sets the record action function
func SetRecordActionFunc(fn func(userID int64, action, tmdbID, mediaType string)) {
	if recommendationBridge != nil {
		recommendationBridge.mu.Lock()
		recommendationBridge.recordActionFunc = fn
		recommendationBridge.mu.Unlock()
	}
}

// getRecommendations gets recommendations for a user
func getRecommendations(userID int64, recType string, limit int) []RecommendationItem {
	if recommendationBridge == nil || recommendationBridge.getRecommendationsFunc == nil {
		log.Printf("[RecommendationBridge] No recommendations function available")
		return []RecommendationItem{}
	}
	recommendationBridge.mu.RLock()
	defer recommendationBridge.mu.RUnlock()
	return recommendationBridge.getRecommendationsFunc(userID, recType, limit)
}

// recordRecommendationAction records a user action for learning
func recordRecommendationAction(userID int64, action, tmdbID, mediaType string) {
	if recommendationBridge == nil || recommendationBridge.recordActionFunc == nil {
		return
	}
	recommendationBridge.mu.RLock()
	defer recommendationBridge.mu.RUnlock()
	recommendationBridge.recordActionFunc(userID, action, tmdbID, mediaType)
}

// RecommendationManager handles recommendation-related operations
type RecommendationManager struct {
	callbackParser *callback.CallbackParser
}

// NewRecommendationManager creates a new recommendation manager
func NewRecommendationManager() *RecommendationManager {
	return &RecommendationManager{
		callbackParser: callback.NewCallbackParser(),
	}
}

// GetRecommendationMenu returns the recommendation menu keyboard
func (rm *RecommendationManager) GetRecommendationMenu() [][]map[string]string {
	return [][]map[string]string{
		{
			{"text": "🎯 为你推荐", "callback_data": "rec:similar"},
			{"text": "🔥 热门推荐", "callback_data": "rec:trending"},
		},
		{
			{"text": "🎲 探索发现", "callback_data": "rec:discovery"},
			{"text": "👥 大家都在看", "callback_data": "rec:social"},
		},
		{
			{"text": "🔄 刷新推荐", "callback_data": "rec:refresh"},
		},
	}
}

// BuildRecommendationMessage builds a message with recommendations
func (rm *RecommendationManager) BuildRecommendationMessage(userID int64, recType string) (string, [][]map[string]string) {
	recTypeMap := map[string]string{
		"similar":   "为你推荐",
		"trending":  "热门推荐",
		"discovery": "探索发现",
		"social":    "大家都在看",
	}

	typeName := recTypeMap[recType]
	if typeName == "" {
		typeName = "推荐"
	}

	// Get recommendations
	recs := getRecommendations(userID, recType, 6)

	if len(recs) == 0 {
		return fmt.Sprintf("✨ %s\n\n暂无推荐内容\n\n💡 多使用搜索和请求功能后，推荐会更准确哦", typeName), rm.GetRecommendationMenu()
	}

	// Build message
	var msg string
	msg += fmt.Sprintf("✨ %s\n\n", typeName)

	// Build keyboard with recommendation buttons
	keyboard := [][]map[string]string{}

	for i, rec := range recs {
		if i >= 6 {
			break
		}

		emoji := "🎬"
		if rec.MediaType == "tv" {
			emoji = "📺"
		}

		// Add to message
		msg += fmt.Sprintf("%d. %s *%s*", i+1, emoji, rec.Title)
		if rec.VoteAverage > 0 {
			msg += fmt.Sprintf(" ⭐%.1f", rec.VoteAverage)
		}
		msg += "\n"

		if rec.Reason != "" {
			msg += fmt.Sprintf("   💡 %s\n", rec.Reason)
		}
		msg += "\n"

		// Add button row with quick request
		buttonText := fmt.Sprintf("📋 %s", rec.Title)
		if len(buttonText) > 25 {
			buttonText = buttonText[:22] + "..."
		}

		keyboard = append(keyboard, []map[string]string{
			{"text": buttonText, "callback_data": rm.callbackParser.FormatWithData("quick_request", map[string]string{
				"id":    strconv.Itoa(rec.TmdbID),
				"type":  rec.MediaType,
				"title": rec.Title,
			})},
		})
	}

	// Add navigation buttons
	keyboard = append(keyboard, []map[string]string{
		{"text": "🔄 换一批", "callback_data": fmt.Sprintf("rec:%s", recType)},
		{"text": "📋 推荐菜单", "callback_data": "rec:menu"},
	})

	return msg, keyboard
}

// BuildRecommendationItemMessage builds detailed message for a single recommendation
func (rm *RecommendationManager) BuildRecommendationItemMessage(userID int64, rec *RecommendationItem) (string, [][]map[string]string) {
	var msg string

	emoji := "🎬"
	if rec.MediaType == "tv" {
		emoji = "📺"
	}

	msg += fmt.Sprintf("%s %s\n\n", emoji, rec.Title)

	if rec.OriginalTitle != "" && rec.OriginalTitle != rec.Title {
		msg += fmt.Sprintf("原名: %s\n", rec.OriginalTitle)
	}

	if rec.VoteAverage > 0 {
		msg += fmt.Sprintf("⭐ 评分: %.1f/10\n", rec.VoteAverage)
	}

	if rec.ReleaseDate != "" && len(rec.ReleaseDate) >= 4 {
		msg += fmt.Sprintf("📅 年份: %s\n", rec.ReleaseDate[:4])
	}

	if len(rec.GenreNames) > 0 {
		msg += fmt.Sprintf("🏷️ 类型: %s\n", joinStrings(rec.GenreNames, ", "))
	}

	if rec.Reason != "" {
		msg += fmt.Sprintf("\n💡 推荐理由: %s\n", rec.Reason)
	}

	if rec.Overview != "" {
		msg += fmt.Sprintf("\n📝 简介:\n%s\n", truncateString(rec.Overview, 200))
	}

	// Build keyboard
	mediaTypeLabel := "movie"
	if rec.MediaType == "tv" {
		mediaTypeLabel = "tv"
	}

	keyboard := [][]map[string]string{
		{
			{"text": "📋 发起请求", "callback_data": rm.callbackParser.FormatWithData("subscribe", map[string]string{
				"id":    strconv.Itoa(rec.TmdbID),
				"type":  mediaTypeLabel,
				"title": rec.Title,
			})},
		},
		{
			{"text": "⬅️ 返回推荐", "callback_data": "rec:back"},
			{"text": "❌ 关闭", "callback_data": "cancel"},
		},
	}

	return msg, keyboard
}

// RecordRequested records when a user requests a recommended item
func (rm *RecommendationManager) RecordRequested(userID int64, tmdbID int, mediaType string) {
	recordRecommendationAction(userID, "requested", strconv.Itoa(tmdbID), mediaType)
	log.Printf("[RecommendationManager] Recorded request: userID=%d, tmdbID=%d, type=%s", userID, tmdbID, mediaType)
}

// RecordIgnored records when a user ignores a recommendation
func (rm *RecommendationManager) RecordIgnored(userID int64, tmdbID int, mediaType string) {
	recordRecommendationAction(userID, "ignored", strconv.Itoa(tmdbID), mediaType)
}

// Helper functions

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// FormatRecommendationsForDisplay formats recommendations as JSON for bridge
func FormatRecommendationsForDisplay(recs []RecommendationItem) string {
	if len(recs) == 0 {
		return "暂无推荐内容"
	}

	var msg string
	for i, rec := range recs {
		if i >= 10 {
			break
		}

		emoji := "🎬"
		if rec.MediaType == "tv" {
			emoji = "📺"
		}

		msg += fmt.Sprintf("%d. %s %s", i+1, emoji, rec.Title)
		if rec.VoteAverage > 0 {
			msg += fmt.Sprintf(" ⭐%.1f", rec.VoteAverage)
		}
		msg += "\n"

		if rec.Reason != "" {
			msg += fmt.Sprintf("   💡 %s\n", rec.Reason)
		}
		msg += "\n"
	}

	return msg
}

// ConvertToRecommendationItems converts from main package format
func ConvertToRecommendationItems(data interface{}) []RecommendationItem {
	// Handle JSON marshal/unmarshal for type conversion
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("[RecommendationManager] Failed to marshal: %v", err)
		return []RecommendationItem{}
	}

	var items []RecommendationItem
	if err := json.Unmarshal(jsonData, &items); err != nil {
		log.Printf("[RecommendationManager] Failed to unmarshal: %v", err)
		return []RecommendationItem{}
	}

	return items
}
