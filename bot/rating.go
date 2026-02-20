package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
)

// RatingBridge provides bridge functions for the rating system
// This allows the bot package to interact with the main package's rating system
type RatingBridge struct {
	// Function pointers (will be set by main package)
	rateMediaFunc      func(userID int64, tmdbID int64, rating float64, title string) error
	getUserRatingFunc   func(userID int64, tmdbID int64) float64
	getAverageRatingFunc func(tmdbID int64) (float64, int)
	getUserRatingsFunc  func(userID int64) map[int64]float64

	mu sync.RWMutex
}

var ratingBridge *RatingBridge

// InitRatingBridge initializes the rating bridge
func InitRatingBridge() {
	ratingBridge = &RatingBridge{}
	log.Println("[RatingBridge] Initialized")
}

// SetRateMediaFunc sets the rate media function
func SetRateMediaFunc(fn func(userID int64, tmdbID int64, rating float64, title string) error) {
	if ratingBridge != nil {
		ratingBridge.mu.Lock()
		ratingBridge.rateMediaFunc = fn
		ratingBridge.mu.Unlock()
	}
}

// SetGetUserRatingFunc sets the get user rating function
func SetGetUserRatingFunc(fn func(userID int64, tmdbID int64) float64) {
	if ratingBridge != nil {
		ratingBridge.mu.Lock()
		ratingBridge.getUserRatingFunc = fn
		ratingBridge.mu.Unlock()
	}
}

// SetGetAverageRatingFunc sets the get average rating function
func SetGetAverageRatingFunc(fn func(tmdbID int64) (float64, int)) {
	if ratingBridge != nil {
		ratingBridge.mu.Lock()
		ratingBridge.getAverageRatingFunc = fn
		ratingBridge.mu.Unlock()
	}
}

// SetGetUserRatingsFunc sets the get user ratings function
func SetGetUserRatingsFunc(fn func(userID int64) map[int64]float64) {
	if ratingBridge != nil {
		ratingBridge.mu.Lock()
		ratingBridge.getUserRatingsFunc = fn
		ratingBridge.mu.Unlock()
	}
}

// Bridge methods

func rateMedia(userID int64, tmdbID int64, rating float64, title string) error {
	if ratingBridge == nil || ratingBridge.rateMediaFunc == nil {
		return fmt.Errorf("评分系统未初始化")
	}
	ratingBridge.mu.RLock()
	defer ratingBridge.mu.RUnlock()
	return ratingBridge.rateMediaFunc(userID, tmdbID, rating, title)
}

func getUserRating(userID int64, tmdbID int64) float64 {
	if ratingBridge == nil || ratingBridge.getUserRatingFunc == nil {
		return 0
	}
	ratingBridge.mu.RLock()
	defer ratingBridge.mu.RUnlock()
	return ratingBridge.getUserRatingFunc(userID, tmdbID)
}

func getAverageRating(tmdbID int64) (float64, int) {
	if ratingBridge == nil || ratingBridge.getAverageRatingFunc == nil {
		return 0, 0
	}
	ratingBridge.mu.RLock()
	defer ratingBridge.mu.RUnlock()
	return ratingBridge.getAverageRatingFunc(tmdbID)
}

func getUserRatings(userID int64) map[int64]float64 {
	if ratingBridge == nil || ratingBridge.getUserRatingsFunc == nil {
		return make(map[int64]float64)
	}
	ratingBridge.mu.RLock()
	defer ratingBridge.mu.RUnlock()
	return ratingBridge.getUserRatingsFunc(userID)
}

// ===================================================================
// Rating Commands (for integration with handler)
// ===================================================================

// handleRateCommandWithBridge handles rate command using the bridge
func handleRateCommandWithBridge(args string, userID int64, selectedItem *SearchItem) (string, error) {
	// Parse args: /rate <tmdbID> <rating> [title]
	if args == "" {
		return "", fmt.Errorf("格式错误")
	}

	parts := strings.Fields(args)
	if len(parts) < 2 {
		return "", fmt.Errorf("格式错误")
	}

	// Parse TMDB ID
	tmdbID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return "", fmt.Errorf("TMDB ID 必须是数字")
	}

	// Parse rating
	rating, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || rating < 1 || rating > 10 {
		return "", fmt.Errorf("评分必须在 1-10 之间")
	}

	// Get title if provided
	title := ""
	if len(parts) >= 3 {
		title = strings.Join(parts[2:], " ")
	} else if selectedItem != nil {
		title = selectedItem.Title
	}

	// Call rating system
	if err := rateMedia(userID, tmdbID, rating, title); err != nil {
		return "", err
	}

	// Get user's new rating
	userRating := getUserRating(userID, tmdbID)
	avgRating, count := getAverageRating(tmdbID)

	var msg strings.Builder
	msg.WriteString("✅ 评分成功！\n\n")
	if title != "" {
		msg.WriteString(fmt.Sprintf("🎬 %s\n\n", title))
	}
	msg.WriteString(fmt.Sprintf("⭐ 你的评分: %.1f/10\n", userRating))
	msg.WriteString(fmt.Sprintf("📊 平均评分: %.1f/10 (%d人评分)", avgRating, count))

	return msg.String(), nil
}

// handleRatingsCommandWithBridge handles ratings command using the bridge
func handleRatingsCommandWithBridge(userID int64) string {
	ratings := getUserRatings(userID)

	if len(ratings) == 0 {
		return `📝 我的评分

你还没有评分过任何作品

━━━━━━━━━━━━━━━━━━━━━

使用 /rate <ID> <评分> 为看过的作品打分`
	}

	var msg strings.Builder
	msg.WriteString("📝 我的评分\n\n")
	msg.WriteString(fmt.Sprintf("共评分了 %d 部作品\n\n", len(ratings)))
	msg.WriteString("━━━━━━━━━━━━━━━━\n\n")

	// Show up to 20 ratings
	count := 0
	for tmdbID, rating := range ratings {
		if count >= 20 {
			msg.WriteString(fmt.Sprintf("... 还有 %d 部作品\n\n", len(ratings)-20))
			break
		}

		avg, avgCount := getAverageRating(tmdbID)
		msg.WriteString(fmt.Sprintf("⭐ %.1f 分 | ID:%d (平均 %.1f, %d人)\n",
			rating, tmdbID, avg, avgCount))
		count++
	}

	return msg.String()
}

// formatRatingForDisplay formats rating with stars
func formatRatingForDisplay(rating float64) string {
	stars := ""
	fullStars := int(rating / 2) // Convert 10-scale to 5-scale
	for i := 0; i < 5; i++ {
		if i < fullStars {
			stars += "★"
		} else {
			stars += "☆"
		}
	}
	return fmt.Sprintf("%.1f %s", rating, stars)
}
