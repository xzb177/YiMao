package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EmbyWebhookPayload represents the incoming webhook from Emby
type EmbyWebhookPayload struct {
	Event      string `json:"Event"`
	ItemType   string `json:"ItemType"`
	ItemID     string `json:"ItemId"`
	ItemName   string `json:"ItemName"`
	Overview   string `json:"Overview"`
	ParentName string `json:"ParentName"` // Series name for episodes
	SeasonName string `json:"SeasonName"`
	IndexNumber *int  `json:"IndexNumber"` // Episode number
	Year       *int   `json:"Year"`
	Genres     []string `json:"Genres"`

	// Nested Item field (library.new format)
	Item       *struct {
		Name            string `json:"Name"`
		Type            string `json:"Type"`
		ParentId        string `json:"ParentId"`
		SeriesName      string `json:"SeriesName"`
		SeasonName      string `json:"SeasonName"`
		IndexNumber     int    `json:"IndexNumber"`
		ProductionYear  int    `json:"ProductionYear"`
		Overview        string `json:"Overview"`
		Genres          []string `json:"Genres"`
		CommunityRating float64 `json:"CommunityRating"`
		FileName        string `json:"FileName"`
	} `json:"Item"`

	// Title field from notification wrapper
	NotificationTitle string `json:"Title"`
}

// JellyseerrWebhookPayload represents the incoming webhook from Jellyseerr
// Supports both nested and flat formats
type JellyseerrWebhookPayload struct {
	NotificationType string `json:"notification_type"`
	Subject          string `json:"subject"`
	Message          string `json:"message"`
	Event            string `json:"event"`
	Image            string `json:"image"`

	// Flat format fields (for Jellyseerr webhook)
	MediaType   string `json:"mediaType"`
	TmdbID      string `json:"tmdbId"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	ReleaseDate string `json:"releaseDate"`
	PosterPath  string `json:"posterPath"`
	Overview    string `json:"overview"`
	RequestID   string `json:"requestId,omitempty"`
	RequestID2  string `json:"request_id,omitempty"`
	RequestStatus string `json:"requestStatus"`
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	UserEmail   string `json:"userEmail"`

	// Nested format (for future use)
	Media *struct {
		MediaType   string `json:"mediaType"`
		TmdbID      int    `json:"tmdbId"`
		Title       string `json:"title,omitempty"`
		Name        string `json:"name,omitempty"`
		ReleaseDate string `json:"releaseDate,omitempty"`
		PosterPath  string `json:"posterPath,omitempty"`
		BackdropPath string `json:"backdropPath,omitempty"`
		Overview    string `json:"overview,omitempty"`
		Status      string `json:"status,omitempty"`
	} `json:"media"`
	Request *struct {
		ID        int    `json:"id"`
		Status    string `json:"status"`
		CreatedAt string `json:"createdAt"`
		ModifiedAt string `json:"modifiedAt"`
	} `json:"request"`
	Issue *struct {
		ID        int    `json:"id"`
		Status    string `json:"status"`
		Problem   string `json:"problem"`
		CreatedAt string `json:"createdAt"`
	} `json:"issue"`
	Comment *struct {
		ID        int    `json:"id"`
		Message   string `json:"message"`
	} `json:"comment"`
	User *struct {
		ID       int    `json:"id"`
		Email    string `json:"email,omitempty"`
		Username string `json:"username,omitempty"`
		Avatar   string `json:"avatar,omitempty"`
	} `json:"user"`
	Extra []map[string]interface{} `json:"extra"`
}

// TelegramMessage represents the message to send to Telegram
type TelegramMessage struct {
	ChatID         string                `json:"chat_id"`
	Text           string                `json:"text"`
	ParseMode      string                `json:"parse_mode"`
	ReplyMarkup    *TelegramInlineKeyboard `json:"reply_markup,omitempty"`
}

// TelegramCallbackQuery represents callback query from inline buttons
type TelegramCallbackQuery struct {
	ID      string `json:"id"`
	From    struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"from"`
	Message *struct {
		MessageID int64 `json:"message_id"`
		Chat struct {
			ID int64 `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
	Data string `json:"data"`
}

// TelegramInlineKeyboard represents inline keyboard markup
type TelegramInlineKeyboard struct {
	InlineKeyboard [][]map[string]string `json:"inline_keyboard"`
}

// TelegramUpdate represents an incoming update
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64  `json:"message_id"`
		From      struct {
			ID        int64  `json:"id"`
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
	CallbackQuery *TelegramCallbackQuery `json:"callback_query"`
}

var (
	botToken    string
	chatID      string
	serverPort  string

	// Admin configuration
	admins      map[string]string // telegram user ID -> name
	adminsMutex sync.RWMutex
	adminsFile  string // 管理员数据保存文件

	// Statistics
	stats       Statistics
	statsMutex  sync.Mutex

	// Pending requests for button actions
	pendingRequests map[string]*PendingRequest
	requestsMutex   sync.Mutex

	// Jellyseerr API URL
	jellyseerrURL string

	// Pending issue replies (userID -> issueID)
	pendingIssueReplies map[int64]int64
	issueReplyMutex     sync.RWMutex
)

// PendingRequest holds info about a request waiting for admin action
type PendingRequest struct {
	RequestID   string
	MediaTitle  string
	MediaType   string
	Username    string
	IssueType   string
	CreatedAt   time.Time
	JellyseerrURL string
}

// Statistics holds daily notification stats
type Statistics struct {
	Date           string
	RequestCount   int
	IssueCount     int
	ApprovedCount  int
	DeclinedCount  int
	AvailableCount int
	MediaAdded     int
	LastUpdateTime time.Time
}

func init() {
	pendingIssueReplies = make(map[int64]int64)
	botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID = os.Getenv("TELEGRAM_CHAT_ID")
	serverPort = os.Getenv("PORT")
	if serverPort == "" {
		serverPort = "8080"
	}

	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}
	if chatID == "" {
		log.Fatal("TELEGRAM_CHAT_ID environment variable is required")
	}

	// Initialize admins from environment
	// Format: ADMINS=user1:Name1,user2:Name2,user3:Name3
	admins = make(map[string]string)
	adminsFile = "/root/emby-telegram-bot/admins.json"

	// 先从文件加载管理员
	loadAdminsFromFile()

	adminsStr := os.Getenv("ADMINS")
	if adminsStr != "" {
		adminList := strings.Split(adminsStr, ",")
		for _, admin := range adminList {
			parts := strings.SplitN(admin, ":", 2)
			if len(parts) == 2 {
				admins[parts[0]] = parts[1]
			}
		}
		saveAdminsToFile() // 保存到文件
	}

	// Initialize pending requests
	pendingRequests = make(map[string]*PendingRequest)

	// Initialize stats
	stats.Date = time.Now().Format("2006-01-02")
	stats.LastUpdateTime = time.Now()

	// Get Jellyseerr URL from env
	jellyseerrURL = os.Getenv("JELLYSEERR_URL")
	if jellyseerrURL == "" {
		jellyseerrURL = "https://embyrequest.oceancloud.asia"
	}

	// Initialize Jellyseerr API client
	InitJellyseerrClient()

	// Initialize aggregation buffer
	InitAggregation()

	// Initialize analytics system
	InitAnalytics()

	// Initialize preference manager
	InitPreferenceManager()

	// Initialize user request manager
	InitUserRequestManager()

	// Initialize smart search
	InitSmartSearch()

	// Initialize request tracker
	InitRequestTracker()

	// Initialize NLP parser
	InitNLP()

	// Initialize smart search manager (enhanced)
	InitSmartSearchManager()

	// Initialize onboarding manager
	InitOnboarding()

	// Initialize admin panel manager
	InitAdminPanelManager()

	// Initialize user sync manager
	InitUserSyncManager()

	// Initialize issue manager
	InitIssueManager()

	// Initialize new modules (2026-02-18)
	InitLogger(os.Getenv("LOG_LEVEL"), "/tmp/emby-bot.log")        // Enhanced logging
	InitQuickLinkManager()                                          // Quick account linking
	InitSearchHistoryManager()                                     // Search history
	InitRecommendationEngine()                                     // AI recommendations
	InitQuotaReminderManager()                                     // Quota reminders

	// Initialize engagement and retention systems (2026-02-18)
	InitEngagementSystem()                                         // Gamification
	InitNotificationRewards()                                      // Random rewards
	InitPushNotificationSystem()                                   // Re-engagement

	// Initialize command center and help system (2026-02-18)
	InitCommands()                                                 // Centralized command handling

	// Start daily summary routine
	go startDailySummary()
}

func formatEmbyNotification(payload EmbyWebhookPayload) string {
	// For library.new, extract info from nested Item structure
	itemType := payload.ItemType
	itemName := payload.ItemName
	parentName := payload.ParentName
	seasonName := payload.SeasonName
	indexNumber := payload.IndexNumber
	year := payload.Year
	genres := payload.Genres

	// If library.new with nested Item, extract from there
	if payload.Event == "library.new" && payload.Item != nil {
		itemType = payload.Item.Type
		itemName = payload.Item.Name
		parentName = payload.Item.SeriesName
		seasonName = payload.Item.SeasonName
		if payload.Item.IndexNumber > 0 {
			indexNumber = &payload.Item.IndexNumber
		}
		if payload.Item.ProductionYear > 0 {
			year = &payload.Item.ProductionYear
		}
		genres = payload.Item.Genres
	}

	emoji := getEmojiForEventType(payload.Event, itemType)

	var text string
	switch payload.Event {
	case "item.updated":
		// Ignore item.updated as it's too frequent
		return ""
	case "library.new", "item.added":
		if itemType == "Episode" || (payload.Item != nil && payload.Item.Type == "Episode") {
			episodeNum := ""
			if indexNumber != nil {
				episodeNum = fmt.Sprintf("E%02d", *indexNumber)
			}
			// Extract season number for display
			seasonNum := ""
			if seasonName != "" {
				if strings.Contains(seasonName, "Season") {
					parts := strings.Split(seasonName, " ")
					if len(parts) > 1 {
						seasonNum = "S" + parts[len(parts)-1]
					}
				} else if strings.Contains(seasonName, "第") && strings.Contains(seasonName, "季") {
					re := regexp.MustCompile(`\d+`)
					if matches := re.FindStringSubmatch(seasonName); len(matches) > 0 {
						seasonNum = "S" + matches[0]
					}
				}
			}

			// Artistic episode design
			text += "┌──────────────┐\n"
			text += "│ 📺 剧集来啦 ✨│\n"
			text += "└──────────────┘\n\n"

			// Series name with style
			text += "  ✨ " + parentName
			if seasonNum != "" && episodeNum != "" {
				text += " " + seasonNum + episodeNum
			}
			text += "\n\n"

			// Episode as a quote
			text += "  ··············\n"
			text += fmt.Sprintf("  「 %s 」", itemName)
			if year != nil {
				text += fmt.Sprintf("\n  🎬 %d", *year)
			}
			text += "\n  ··············"

		} else if itemType == "Movie" || (payload.Item != nil && payload.Item.Type == "Movie") {
			// Artistic movie design - cinema ticket style
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n"
			text += "  🎬 新电影来噜 ✨\n"
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n\n"

			movYear := 0
			if year != nil {
				movYear = *year
			}

			// Get rating info (includes Chinese name and genres)
			mediaRating := getMediaRating(itemName, movYear, "movie")

			// Use Chinese name if available, otherwise use original
			displayTitle := itemName
			if mediaRating.CNName != "" && mediaRating.CNName != itemName {
				displayTitle = mediaRating.CNName
			}

			// Main title
			text += "  🎫 " + displayTitle
			if movYear > 0 {
				text += fmt.Sprintf("\n  ········\n  📅 %d", movYear)
			}
			text += "\n"

			// Use Chinese genres from API if available, otherwise use provided genres
			genreText := ""
			if mediaRating.GenreCN != "" {
				genreText = mediaRating.GenreCN
			} else if len(genres) > 0 {
				displayGenres := genres
				if len(genres) > 3 {
					displayGenres = genres[:3]
				}
				for i, g := range displayGenres {
					if i > 0 {
						genreText += " · "
					}
					genreText += g
				}
			}

			if genreText != "" {
				text += "\n  🏷️ " + genreText
			}

			// Show rating with source
			if mediaRating.Score > 0 {
				text += fmt.Sprintf("\n  ⭐ TMDB %.1f 分", mediaRating.Score)
			} else {
				text += "\n  ⭐ 评分暂无"
			}
		} else {
			text = "✨ 新内容入库\n\n"
			text += fmt.Sprintf("《%s》\n", itemName)
			text += fmt.Sprintf("📝 %s\n", itemType)
		}
	case "system.notificationtest":
		text = "🔔 *Emby 测试通知*\n\n✅ Webhook 连接成功！"
	default:
		text = fmt.Sprintf("%s *%s*\n\n", emoji, payload.Event)
		if payload.ItemName != "" {
			text += fmt.Sprintf("📦 名称: %s\n", payload.ItemName)
		}
		if payload.ItemType != "" {
			text += fmt.Sprintf("📝 类型: %s\n", payload.ItemType)
		}
		if len(genres) > 0 {
			text += fmt.Sprintf("🏷️ %v\n", genres)
		}
	}

	text += fmt.Sprintf("\n🕐 %s", time.Now().Format("2006-01-02 15:04:05"))

	return text
}

func getEmojiForEventType(event, itemType string) string {
	switch event {
	case "item.added":
		switch itemType {
		case "Movie":
			return "🎥"
		case "Episode":
			return "📺"
		case "Series":
			return "🎬"
		case "Season":
			return "📼"
		default:
			return "✨"
		}
	case "item.updated":
		return "🔄"
	default:
		return "📢"
	}
}

// DoubanRatingCache caches rating results to avoid repeated API calls
var doubanCache = struct {
	sync.RWMutex
	data map[string]float64
}{data: make(map[string]float64)}

// getDoubanRating fetches movie rating from Douban API
func getDoubanRating(title string, year int) float64 {
	cacheKey := fmt.Sprintf("%s_%d", title, year)

	// Check cache first
	doubanCache.RLock()
	if rating, exists := doubanCache.data[cacheKey]; exists {
		doubanCache.RUnlock()
		return rating
	}
	doubanCache.RUnlock()

	// Search on Douban
	searchURL := fmt.Sprintf("https://www.douban.com/search?q=%s", url.QueryEscape(title))
	if year > 0 {
		searchURL = fmt.Sprintf("https://www.douban.com/search?q=%s+%d", url.QueryEscape(title), year)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	// Parse rating from search results HTML
	// Douban search shows rating as: <span class="rating_nums">8.5</span>
	re := regexp.MustCompile(`class="rating_nums">([^<]+)<`)
	matches := re.FindStringSubmatch(string(body))
	if len(matches) > 1 {
		rating, err := strconv.ParseFloat(matches[1], 64)
		if err == nil && rating > 0 {
			// Cache the result
			doubanCache.Lock()
			doubanCache.data[cacheKey] = rating
			doubanCache.Unlock()
			return rating
		}
	}

	return 0
}

// MediaRating holds rating information
type MediaRating struct {
	Score    float64
	Source   string
	CNName   string // Chinese name
	GenreCN  string // Chinese genre name
}

// RatingCache caches rating results
var ratingCache = struct {
	sync.RWMutex
	data map[string]MediaRating
}{data: make(map[string]MediaRating)}

// getMediaRating fetches rating from TMDB API (free)
func getMediaRating(title string, year int, mediaType string) MediaRating {
	cacheKey := fmt.Sprintf("%s_%d_%s", title, year, mediaType)

	// Check cache first
	ratingCache.RLock()
	if rating, exists := ratingCache.data[cacheKey]; exists {
		ratingCache.RUnlock()
		return rating
	}
	ratingCache.RUnlock()

	// Use TMDB API (free tier)
	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=2dca580c2a14b55200e784d157207b4d&query=%s&language=zh-CN", url.QueryEscape(title))
	if year > 0 {
		searchURL += fmt.Sprintf("&year=%d", year)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		log.Printf("TMDB API error: %v", err)
		return MediaRating{Score: 0}
	}
	defer resp.Body.Close()

	var tmdbResult struct {
		Results []struct {
			VoteAverage float64 `json:"vote_average"`
			Title       string  `json:"title"`
			GenreIds    []int   `json:"genre_ids"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tmdbResult); err != nil {
		log.Printf("TMDB decode error: %v", err)
		return MediaRating{Score: 0}
	}

	if len(tmdbResult.Results) > 0 && tmdbResult.Results[0].VoteAverage > 0 {
		movie := tmdbResult.Results[0]
		result := MediaRating{
			Score:  movie.VoteAverage,
			Source: "TMDB",
			CNName: movie.Title,
		}

		// Map genre IDs to Chinese names
		genreMap := map[int]string{
			28:     "动作",
			12:     "冒险",
			16:     "动画",
			35:     "喜剧",
			80:     "犯罪",
			99:     "纪录",
			18:     "剧情",
			10751:  "家庭",
			14:     "奇幻",
			36:     "历史",
			27:     "恐怖",
			10402:  "音乐",
			9648:   "悬疑",
			10749:  "爱情",
			878:    "科幻",
			10770:  "电视电影",
			53:     "惊悚",
			10752:  "战争",
			37:     "西部",
		}

		var genres []string
		for _, gid := range movie.GenreIds {
			if name, ok := genreMap[gid]; ok {
				genres = append(genres, name)
			}
			if len(genres) >= 3 {
				break
			}
		}
		if len(genres) > 0 {
			result.GenreCN = strings.Join(genres, " · ")
		}

		// Cache the result
		ratingCache.Lock()
		ratingCache.data[cacheKey] = result
		ratingCache.Unlock()
		log.Printf("Got TMDB rating for %s: %.1f, CN: %s", title, result.Score, result.CNName)
		return result
	}

	return MediaRating{Score: 0}
}

func formatJellyseerrNotification(payload JellyseerrWebhookPayload) string {
	log.Printf("[DEBUG] formatJellyseerrNotification called, notification_type=%s", payload.NotificationType)
	var text string

	// Get event type from notification_type (not event field)
	// notification_type contains values like "ISSUE_CREATED", "REQUEST_CREATED"
	// event field contains descriptive values like "New Video Issue Reported"
	eventType := payload.NotificationType
	if eventType == "" {
		eventType = payload.Event
	}
	log.Printf("[DEBUG] eventType=%s", eventType)

	// Get media title - try both flat and nested formats
	mediaTitle := payload.Title
	if mediaTitle == "" {
		mediaTitle = payload.Name
	}
	if mediaTitle == "" && payload.Media != nil {
		mediaTitle = payload.Media.Title
		if mediaTitle == "" {
			mediaTitle = payload.Media.Name
		}
	}

	// Get media type - try both formats
	mediaType := payload.MediaType
	if mediaType == "" && payload.Media != nil {
		mediaType = payload.Media.MediaType
	}

	// Get release date - try both formats
	releaseDate := payload.ReleaseDate
	if releaseDate == "" && payload.Media != nil {
		releaseDate = payload.Media.ReleaseDate
	}

	// Get poster path - try both formats
	posterPath := payload.PosterPath
	if posterPath == "" && payload.Media != nil {
		posterPath = payload.Media.PosterPath
	}

	// Get overview - try both formats
	overview := payload.Overview
	if overview == "" && payload.Media != nil {
		overview = payload.Media.Overview
	}

	// Get username - try both formats
	username := payload.Username
	if username == "" && payload.User != nil {
		username = payload.User.Username
	}
	if username == "" {
		username = "用户"
	}

	// Get request ID - try both formats
	requestID := payload.RequestID
	if requestID == "" && payload.Request != nil {
		requestID = fmt.Sprintf("%d", payload.Request.ID)
	}

	switch eventType {
	case "TEST_NOTIFICATION":
		log.Printf("[DEBUG] Handling TEST_NOTIFICATION")
		text = "🔔 *Jellyseerr 测试通知*\n\n✅ Webhook 连接成功！"
		if payload.Message != "" {
			text += fmt.Sprintf("\n📝 消息: %s", payload.Message)
		}

	case "REQUEST_CREATED", "request_created", "MEDIA_PENDING":
		log.Printf("[DEBUG] Handling REQUEST_CREATED")

		// Record to analytics
		if analytics != nil && requestID != "" {
			tmdbID := 0
			if payload.TmdbID != "" {
				fmt.Sscanf(payload.TmdbID, "%d", &tmdbID)
			}
			RecordRequest(requestID, mediaTitle, mediaType, tmdbID, payload.UserID, username)
		}
		log.Printf("[DEBUG] After RecordRequest")

		// Get rating info for the requested media
		reqYear := 0
		if releaseDate != "" && len(releaseDate) >= 4 {
			fmt.Sscanf(releaseDate[:4], "%d", &reqYear)
		}
		mediaRating := getMediaRating(mediaTitle, reqYear, mediaType)
		log.Printf("[DEBUG] After getMediaRating")

		// Use Chinese name if available
		displayTitle := mediaTitle
		if mediaRating.CNName != "" {
			displayTitle = mediaRating.CNName
		}

		// Use same style as media library notification
		if mediaType == "movie" {
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n"
			text += "  🎬 新求片来噜 ✨\n"
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n\n"

			// Main title
			text += "  🎫 " + displayTitle
			if reqYear > 0 {
				text += fmt.Sprintf("\n  ········\n  📅 %d", reqYear)
			}
			text += "\n"

			// Genres and rating
			if mediaRating.GenreCN != "" {
				text += fmt.Sprintf("\n  🏷️ %s", mediaRating.GenreCN)
			}
			if mediaRating.Score > 0 {
				text += fmt.Sprintf("\n  ⭐ TMDB %.1f 分", mediaRating.Score)
			}
		} else if mediaType == "tv" {
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n"
			text += "  📺 新剧集来噜 ✨\n"
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n\n"

			// Main title
			text += "  🎫 " + displayTitle
			if reqYear > 0 {
				text += fmt.Sprintf("\n  ········\n  📅 %d", reqYear)
			}
			text += "\n"

			// Genres and rating
			if mediaRating.GenreCN != "" {
				text += fmt.Sprintf("\n  🏷️ %s", mediaRating.GenreCN)
			}
			if mediaRating.Score > 0 {
				text += fmt.Sprintf("\n  ⭐ TMDB %.1f 分", mediaRating.Score)
			}
		} else {
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n"
			text += "  ✨ 新内容来噜 ✨\n"
			text += "╺━━━━━━━━━━━━━━━━━━━━━━╸\n\n"
			text += fmt.Sprintf("  📦 %s\n", displayTitle)
		}

		text += fmt.Sprintf("\n  👤 %s 请求", username)
		if requestID != "" {
			text += fmt.Sprintf(" · #%s", requestID)
		}

		log.Printf("[DEBUG] Before TrackRequest")
		// Track request for auto-reminder and notification
		tmdbID := 0
		if payload.TmdbID != "" {
			fmt.Sscanf(payload.TmdbID, "%d", &tmdbID)
		}
		TrackRequest(requestID, mediaTitle, mediaType, tmdbID, payload.UserID, username)
		log.Printf("[DEBUG] After TrackRequest")

		// Send private notification to admins with buttons
		notifyAdminsRequest(mediaTitle, mediaType, username, requestID)
		log.Printf("[DEBUG] After notifyAdminsRequest")

		log.Printf("[DEBUG] REQUEST_CREATED case done, text length=%d", len(text))

	case "REQUEST_APPROVED", "request_approved", "MEDIA_APPROVED":
		// Update analytics
		if analytics != nil && requestID != "" {
			UpdateRequestStatus(requestID, "approved")
		}
		// Update tracker
		UpdateTrackedRequestStatus(requestID, "approved")

		text = "╔════════════════════════════════════╗\n"
		text += "║     ✅ 请求已批准 · Approved        ║\n"
		text += "╚════════════════════════════════════╝\n\n"
		text += fmt.Sprintf("  📦 %s\n", mediaTitle)
		text += fmt.Sprintf("  👤 %s 请求\n", username)
		text += "  ──────────────────────────────────\n"
		text += "  🎬 正在处理中，请耐心等待~"

	case "REQUEST_AVAILABLE", "request_available", "MEDIA_AVAILABLE":
		// Update analytics
		if analytics != nil && requestID != "" {
			UpdateRequestStatus(requestID, "available")
		}
		// Update tracker (will notify requester)
		UpdateTrackedRequestStatus(requestID, "available")

		text = "╔════════════════════════════════════╗\n"
		text += "║     🎉 已可用 · Available           ║\n"
		text += "╚════════════════════════════════════╝\n\n"
		text += fmt.Sprintf("  🎉 %s 已上架！\n\n", mediaTitle)
		text += "  ┌─────────────────────────────────┐\n"
		text += "  │  🍿 快去观看吧！                 │\n"
		text += "  └─────────────────────────────────┘"

	case "REQUEST_DECLINED", "request_declined", "MEDIA_DECLINED":
		// Update analytics
		if analytics != nil && requestID != "" {
			UpdateRequestStatus(requestID, "declined")
		}
		// Remove from tracker
		if requestTracker != nil {
			requestTracker.requestMutex.Lock()
			delete(requestTracker.pendingRequests, requestID)
			requestTracker.requestMutex.Unlock()
		}

		text = "╔════════════════════════════════════╗\n"
		text += "║     ❌ 请求已拒绝 · Declined        ║\n"
		text += "╚════════════════════════════════════╝\n\n"
		text += fmt.Sprintf("  📦 %s\n", mediaTitle)
		text += fmt.Sprintf("  👤 %s 请求\n\n", username)
		text += "  ──────────────────────────────────\n"
		text += "  💡 如有疑问请联系管理员"

	case "MEDIA_AUTO_APPROVED", "media_auto_approved":
		text = fmt.Sprintf("🤖 *自动批准求片请求*\n\n")
		text += fmt.Sprintf("📦 名称: %s\n", mediaTitle)
		text += fmt.Sprintf("👤 请求者: %s\n", username)

	case "ISSUE_CREATED", "issue_created":
		log.Printf("[DEBUG] Handling ISSUE_CREATED, event=%s", payload.Event)
		// Determine issue type from event field
		issueEmoji := "🐛"
		issueType := "问题报告"
		issuePriority := "🟡 普通" // Default priority
		isUrgent := false

		if strings.Contains(payload.Event, "Subtitle") {
			issueEmoji = "💬"
			issueType = "字幕问题"
		} else if strings.Contains(payload.Event, "Video") {
			issueEmoji = "🎬"
			issueType = "视频问题"
			issuePriority = "🟠 重要"
			isUrgent = true
		} else if strings.Contains(payload.Event, "Audio") {
			issueEmoji = "🔊"
			issueType = "音频问题"
			issuePriority = "🟠 重要"
			isUrgent = true
		}

		text = fmt.Sprintf("%s *新%s*\n\n", issueEmoji, issueType)
		text += fmt.Sprintf("%s 优先级\n\n", issuePriority)
		if payload.Subject != "" {
			text += fmt.Sprintf("📦 媒体: %s\n", payload.Subject)
		}
		if payload.Message != "" && !strings.Contains(payload.Message, "{{") {
			text += fmt.Sprintf("📝 问题描述: %s\n", payload.Message)
		}

		// Add reporter info
		reporterInfo := ""
		if payload.UserID != "" && userSyncMgr != nil {
			jellyseerrID, _ := strconv.ParseInt(payload.UserID, 10, 64)
			telegramID, tgUsername, ok := userSyncMgr.GetTelegramUserInfo(jellyseerrID)
			if ok {
				reporterInfo = fmt.Sprintf("\n👉 %s (@%s) (%d)", username, tgUsername, telegramID)
			}
		}
		if reporterInfo == "" {
			reporterInfo = fmt.Sprintf("\n👉 %s", username)
		}
		text += reporterInfo
		text += fmt.Sprintf("\n⚠️ 请前往 Jellyseerr 管理面板处理")

		// Send private notification to admins for urgent issues
		if isUrgent {
			privateMsg := fmt.Sprintf("%s *新%s*\n\n", issueEmoji, issueType)
			privateMsg += fmt.Sprintf("%s 优先级\n\n", issuePriority)
			privateMsg += fmt.Sprintf("📦 媒体: %s\n", payload.Subject)
			if payload.Message != "" && !strings.Contains(payload.Message, "{{") {
				privateMsg += fmt.Sprintf("📝 问题描述: %s\n", payload.Message)
			}
			privateMsg += reporterInfo
			privateMsg += "\n⚠️ 请前往 Jellyseerr 管理面板处理"
			notifyAdminsPrivately(privateMsg)
		}

	case "ISSUE_COMMENT", "issue_comment":
		text = fmt.Sprintf("💬 *问题有新评论*\n\n")
		if mediaTitle != "" {
			text += fmt.Sprintf("📦 相关: %s\n", mediaTitle)
		}
		text += fmt.Sprintf("👤 评论者: %s\n", username)
		if payload.Comment != nil && payload.Comment.Message != "" {
			text += fmt.Sprintf("💬 评论: %s\n", payload.Comment.Message)
		}

	case "ISSUE_RESOLVED", "issue_resolved":
		text = fmt.Sprintf("✅ *问题已解决*\n\n")
		if mediaTitle != "" {
			text += fmt.Sprintf("📦 相关: %s\n", mediaTitle)
		}

	default:
		log.Printf("[DEBUG] Handling DEFAULT, eventType=%s", eventType)
		// Generic notification - use subject and message
		if payload.Subject != "" {
			text = fmt.Sprintf("📢 *%s*\n\n", payload.Subject)
		} else {
			text = "📢 *Jellyseerr 通知*\n\n"
		}
		if payload.Message != "" && !strings.Contains(payload.Message, "{{") {
			text += fmt.Sprintf("📝 %s\n", payload.Message)
		}
		if mediaTitle != "" && !strings.Contains(mediaTitle, "{{") {
			text += fmt.Sprintf("📦 名称: %s\n", mediaTitle)
		}
	}

	log.Printf("[DEBUG] After switch, text length=%d", len(text))

	// Add overview if available and not too long
	if overview != "" && len(overview) < 200 && !strings.Contains(overview, "{{") {
		text += fmt.Sprintf("\n📖 简介: %s", overview)
	}

	text += fmt.Sprintf("\n\n🕐 时间: %s", time.Now().Format("2006-01-02 15:04:05"))

	log.Printf("[DEBUG] About to return text, length=%d", len(text))
	return text
}

func sendTelegramMessage(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	msg := TelegramMessage{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "Markdown",
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Debug log
	fmt.Printf("[DEBUG] Sending Telegram message: %s\n", text)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("[DEBUG] Error sending message: %v\n", err)
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[DEBUG] Telegram API returned status %d\n", resp.StatusCode)
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	fmt.Println("[DEBUG] Telegram message sent successfully")
	return nil
}

// notifyAdminsRequest notifies all admins about a new request with action buttons
func notifyAdminsRequest(mediaTitle, mediaType, username, requestID string) {
	log.Printf("[DEBUG] notifyAdminsRequest called, admins=%v", admins)
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	if len(admins) == 0 {
		log.Printf("⚠️ 没有管理员，无法发送私聊通知")
		return
	}

	log.Printf("🔔 准备发送管理员私聊通知，管理员数量: %d", len(admins))

	// Create request ID for callback
	callbackID := fmt.Sprintf("req_%s_%d", requestID, time.Now().Unix())

	// Store pending request
	requestsMutex.Lock()
	pendingRequests[callbackID] = &PendingRequest{
		RequestID:  requestID,
		MediaTitle: mediaTitle,
		MediaType:  mediaType,
		Username:   username,
		CreatedAt:  time.Now(),
	}
	requestsMutex.Unlock()

	mediaEmoji := "📀"
	if mediaType == "movie" {
		mediaEmoji = "🎬"
	} else if mediaType == "tv" {
		mediaEmoji = "📺"
	}

	msg := fmt.Sprintf("%s *新求片请求*\n\n", mediaEmoji)
	msg += fmt.Sprintf("📦 %s\n", mediaTitle)
	msg += fmt.Sprintf("👤 %s 请求\n", username)
	if requestID != "" {
		msg += fmt.Sprintf("🆔 ID: %s", requestID)
	}
	msg += fmt.Sprintf("\n\n请选择操作：")

	// Create inline keyboard
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "✅ 批准", "callback_data": fmt.Sprintf("approve_%s", callbackID)},
				{"text": "❌ 拒绝", "callback_data": fmt.Sprintf("decline_%s", callbackID)},
			},
			{
				{"text": "🔗 详情", "url": fmt.Sprintf("%s/requests/%s", jellyseerrURL, requestID)},
			},
		},
	}

	successCount := 0
	for userID, userName := range admins {
		log.Printf("📤 发送私聊给管理员: %s (ID: %s)", userName, userID)
		userIDInt, err := strconv.ParseInt(userID, 10, 64)
		if err != nil {
			log.Printf("❌ 无法解析用户ID %s: %v", userID, err)
			continue
		}
		if err := sendPrivateMessage(userIDInt, msg, keyboard); err != nil {
			log.Printf("❌ 发送私聊给管理员 %s (ID:%s) 失败: %v", userName, userID, err)
		} else {
			log.Printf("✅ 成功发送给管理员 %s (ID:%s)", userName, userID)
			successCount++
		}
	}
	log.Printf("📊 管理员私聊通知发送完成: %d/%d 成功", successCount, len(admins))
}

// handleRequestAction handles admin action on a request (approve/decline)
func handleRequestAction(action, callbackID string) string {
	requestsMutex.Lock()
	defer requestsMutex.Unlock()

	request, exists := pendingRequests[callbackID]
	if !exists {
		return "❌ 请求已过期或不存在"
	}

	// Clean up old requests
	delete(pendingRequests, callbackID)

	// Convert request ID to int
	requestID, err := strconv.Atoi(request.RequestID)
	if err != nil {
		return fmt.Sprintf("❌ 无效的请求ID: %s", request.RequestID)
	}

	switch action {
	case "approve":
		if jellyseerrClient == nil {
			return fmt.Sprintf("⚠️ Jellyseerr API 未配置\n\n请在管理面板批准: %s", request.MediaTitle)
		}
		if err := jellyseerrClient.ApproveRequest(requestID); err != nil {
			return fmt.Sprintf("❌ 批准失败: %v\n\n请在管理面板操作", err)
		}
		return fmt.Sprintf("✅ 已批准: %s\n\nJellyseerr 将自动下载此内容", request.MediaTitle)

	case "decline":
		if jellyseerrClient == nil {
			return fmt.Sprintf("⚠️ Jellyseerr API 未配置\n\n请在管理面板拒绝: %s", request.MediaTitle)
		}
		if err := jellyseerrClient.DeclineRequest(requestID); err != nil {
			return fmt.Sprintf("❌ 拒绝失败: %v\n\n请在管理面板操作", err)
		}
		return fmt.Sprintf("❌ 已拒绝: %s", request.MediaTitle)

	default:
		return "❓ 未知操作"
	}
}

// sendPrivateMessage sends a message to a specific user via private chat
func sendPrivateMessage(userID int64, text string, replyMarkup *TelegramInlineKeyboard) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	msg := TelegramMessage{
		ChatID:      fmt.Sprintf("%d", userID),
		Text:        text,
		ParseMode:   "", // Disable parse mode to avoid markdown parsing issues
		ReplyMarkup: replyMarkup,
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Telegram API error: %s", string(bodyBytes))
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// notifyAdminsPrivately notifies all admins about urgent issues
func notifyAdminsPrivately(message string) {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	if len(admins) == 0 {
		return
	}

	urgentMessage := "🚨 *紧急问题通知*\n\n" + message
	urgentMessage += fmt.Sprintf("\n🕐 %s", time.Now().Format("2006-01-02 15:04:05"))

	for userID := range admins {
		userIDInt, _ := strconv.ParseInt(userID, 10, 64)
		if err := sendPrivateMessage(userIDInt, urgentMessage, nil); err != nil {
			log.Printf("Error sending private message to admin %s: %v", userID, err)
		}
	}
}

// notifyAdminsOfBindingRequest notifies admins about a new binding request
func notifyAdminsOfBindingRequest(req *BindingRequest) {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	if len(admins) == 0 {
		log.Println("User sync: No admins to notify about binding request")
		return
	}

	msg := "🔗 *新的账号绑定请求*\n\n"
	msg += fmt.Sprintf("👤 用户: %s (ID: `%d`)\n", req.TelegramName, req.TelegramID)
	msg += fmt.Sprintf("🎬 请求绑定: %s (@%s, ID: `%d`)\n\n", req.JellyseerrName, req.JellyseerrUsername, req.JellyseerrID)
	msg += fmt.Sprintf("⏰ 时间: %s\n", req.CreatedAt.Format("2006-01-02 15:04"))
	msg += fmt.Sprintf("📝 请求ID: `%s`\n\n", req.RequestID)

	// Create inline keyboard with approve/reject buttons
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "✅ 批准", "callback_data": "approve_bind:" + req.RequestID},
				{"text": "❌ 拒绝", "callback_data": "reject_bind:" + req.RequestID},
			},
			{
				{"text": "📋 查看全部", "callback_data": "list_bind_requests"},
			},
		},
	}

	for userIDStr := range admins {
		userIDInt, _ := strconv.ParseInt(userIDStr, 10, 64)
		if err := sendPrivateMessage(userIDInt, msg, keyboard); err != nil {
			log.Printf("Error sending binding request notification to admin %s: %v", userIDStr, err)
		}
	}

	log.Printf("User sync: Notified %d admins about binding request %s", len(admins), req.RequestID)
}

// handlePrivateMessage handles private messages to the bot
func handlePrivateMessage(update *TelegramUpdate) {
	if update.Message == nil {
		return
	}
	log.Printf("[DEBUG] handlePrivateMessage called with text: %s", update.Message.Text)

	userID := fmt.Sprintf("%d", update.Message.From.ID)
	username := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		username += " " + update.Message.From.LastName
	}
	if update.Message.From.Username != "" {
		username += "(@" + update.Message.From.Username + ")"
	}

	// Save Telegram username for issue reporting
	if userSyncMgr != nil && update.Message.From.Username != "" {
		userSyncMgr.SetTelegramUsername(update.Message.From.ID, update.Message.From.Username)
	}

	text := strings.TrimSpace(update.Message.Text)

	// Check if this is a first-time user (show onboarding)
	if ShouldShowOnboarding(update.Message.From.ID) {
		if text == "/start" {
			msg, keyboard := GetWelcomeForNewUser(update.Message.From.ID, username)
			sendPrivateMessage(update.Message.From.ID, msg, keyboard)
			return
		}
		// Complete onboarding after first interaction
		if onboardingMgr != nil {
			onboardingMgr.CompleteOnboarding(update.Message.From.ID)
		}
	}

	// Extract command if text contains space (for arguments)
	command := text
	if idx := strings.Index(text, " "); idx > 0 {
		command = text[:idx]
	}

	// Check if user has a pending issue reply (admin only)
	issueReplyMutex.Lock()
	if pendingIssueID, hasPending := pendingIssueReplies[update.Message.From.ID]; hasPending {
		// This is a reply to an issue
		issueReplyMutex.Unlock()

		// Check if user is admin
		adminsMutex.RLock()
		_, isAdmin := admins[userID]
		adminsMutex.RUnlock()

		if isAdmin {
			// Determine actual issue ID and whether to close after reply
			actualIssueID := pendingIssueID
			shouldClose := false

			if pendingIssueID < 0 {
				// Negative ID means "close after reply"
				actualIssueID = -pendingIssueID
				shouldClose = true
			}

			// Post the comment to Jellyseerr
			if issueMgr != nil {
				if err := issueMgr.AddComment(actualIssueID, text); err != nil {
					sendPrivateMessage(update.Message.From.ID, fmt.Sprintf("❌ 回复失败: %v", err), nil)
				} else {
					log.Printf("Admin %d replied to issue %d: %s", update.Message.From.ID, actualIssueID, text)

					// Close issue if requested
					if shouldClose {
						if err := issueMgr.DeleteIssue(actualIssueID); err != nil {
							log.Printf("Error closing issue %d: %v", actualIssueID, err)
							sendPrivateMessage(update.Message.From.ID, "✅ 回复已发送，但关闭问题失败", nil)
						} else {
							sendPrivateMessage(update.Message.From.ID, "✅ 回复已发送，问题已关闭", nil)
						}
					} else {
						sendPrivateMessage(update.Message.From.ID, "✅ 回复已发送", nil)
					}
				}
			} else {
				sendPrivateMessage(update.Message.From.ID, "❌ 问题管理器未初始化", nil)
			}

			// Clear the pending state
			issueReplyMutex.Lock()
			delete(pendingIssueReplies, update.Message.From.ID)
			issueReplyMutex.Unlock()
			return
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无法回复问题", nil)
			issueReplyMutex.Lock()
			delete(pendingIssueReplies, update.Message.From.ID)
			issueReplyMutex.Unlock()
			return
		}
	}
	issueReplyMutex.Unlock()

	switch command {
	case "/start", "/help":
		// Check if user needs onboarding
		if ShouldShowOnboarding(update.Message.From.ID) && text == "/start" {
			msg, keyboard := GetWelcomeForNewUser(update.Message.From.ID, username)
			sendPrivateMessage(update.Message.From.ID, msg, keyboard)
			return
		}

		// Show simplified help
		helpMsg := "🤖 *云海看板娘*\n\n"
		helpMsg += "📱 点击左下角菜单查看所有功能\n\n"
		helpMsg += "• 直接输入内容名搜索\n"
		helpMsg += "• 点击按钮发起请求\n"
		helpMsg += "• 完成后自动通知你\n\n"
		helpMsg += "试试：\n"
		helpMsg += "• 复仇者联盟\n"
		helpMsg += "• 权力的游戏\n"
		helpMsg += "• 2024年的电影"
		sendPrivateMessage(update.Message.From.ID, helpMsg, nil)

	case "/my", "/myrequests", "/me":
		// Show user's requests
		handleMyRequestsPrivate(update.Message.From.ID)

	case "/register":
		// Check if there are any existing admins
		adminsMutex.Lock()
		hasExistingAdmins := len(admins) > 0
		_, alreadyAdmin := admins[userID]
		adminsMutex.Unlock()

		// If already admin, notify
		if alreadyAdmin {
			msg := fmt.Sprintf("✅ 你已经是管理员了\n\n")
			msg += fmt.Sprintf("用户ID: `%s`\n", userID)
			msg += fmt.Sprintf("数字ID: `%d`", update.Message.From.ID)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
			return
		}

		// If no admins exist, allow first registration
		if !hasExistingAdmins {
			adminsMutex.Lock()
			admins[userID] = username
			adminsMutex.Unlock()
			saveAdminsToFile()

			msg := fmt.Sprintf("✅ *注册成功*\n\n")
			msg += fmt.Sprintf("你已成为第一位管理员\n")
			msg += fmt.Sprintf("用户名: %s\n", username)
			msg += fmt.Sprintf("用户ID: `%s`\n", userID)
			msg += fmt.Sprintf("数字ID: `%d`\n", update.Message.From.ID)
			msg += "\n🔔 你将收到：\n"
			msg += "• 紧急问题私聊通知"
			sendPrivateMessage(update.Message.From.ID, msg, nil)

			log.Printf("首位管理员注册: %s (%s)", username, userID)
			return
		}

		// Otherwise, require existing admin approval
		msg := "⚠️ *无法自行注册*\n\n"
		msg += "系统已有管理员，请让现有管理员通过以下方式添加你：\n\n"
		msg += "1. 现有管理员私聊我发送 `/addadmin 你的用户ID`\n"
		msg += "2. 或者通过 API 添加\n\n"
		msg += fmt.Sprintf("你的用户ID: `%d`", update.Message.From.ID)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/unregister":
		adminsMutex.Lock()
		delete(admins, userID)
		adminsMutex.Unlock()

		msg := "❌ *已取消管理员*\n\n"
		msg += "你将不再收到私聊通知。"
		sendPrivateMessage(update.Message.From.ID, msg, nil)

		log.Printf("Admin unregistered: %s (%s)", username, userID)

	case "/status":
		adminsMutex.RLock()
		isAdmin, exists := admins[userID]
		adminsMutex.RUnlock()

		statusMsg := fmt.Sprintf("📊 *当前状态*\n\n")
		statusMsg += fmt.Sprintf("用户ID: `%s`\n", userID)
		statusMsg += fmt.Sprintf("数字ID: `%d`\n", update.Message.From.ID)
		statusMsg += fmt.Sprintf("管理员状态: %s\n", map[bool]string{true: "✅ 是管理员", false: "❌ 非管理员"}[exists])
		if exists {
			statusMsg += fmt.Sprintf("注册名称: %s\n", isAdmin)
		}
		// 显示所有管理员ID用于调试
		statusMsg += fmt.Sprintf("\n📋 所有管理员ID:\n")
		adminsMutex.RLock()
		for uid, name := range admins {
			statusMsg += fmt.Sprintf("• ID: `%s` - %s\n", uid, name)
		}
		adminsMutex.RUnlock()
		statusMsg += fmt.Sprintf("\n群组ID: `%s`", chatID)
		sendPrivateMessage(update.Message.From.ID, statusMsg, nil)

	case "/admins":
		adminsMutex.RLock()
		defer adminsMutex.RUnlock()

		if len(admins) == 0 {
			sendPrivateMessage(update.Message.From.ID, "📋 *管理员列表*\n\n暂无管理员", nil)
			return
		}

		msg := "📋 *管理员列表*\n\n"
		for uid, name := range admins {
			msg += fmt.Sprintf("• %s (`%s`)\n", name, uid)
		}
		msg += fmt.Sprintf("\n共 %d 位管理员", len(admins))
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/stats":
		// Get today's stats from analytics system
		today := time.Now().Format("2006-01-02")
		weekday := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[time.Now().Weekday()]

		analytics.mutex.RLock()
		var dailyStats *DailyCount
		if ds, exists := analytics.DailyStats[today]; exists {
			dailyStats = ds
		} else {
			dailyStats = &DailyCount{Date: today}
		}

		// Count pending requests for today
		pendingCount := 0
		for _, req := range analytics.Requests {
			if req.CreatedAt.Format("2006-01-02") == today && req.Status == "pending" {
				pendingCount++
			}
		}
		analytics.mutex.RUnlock()

		// Get current stats for issue count and media added
		statsMutex.Lock()
		issueCount := stats.IssueCount
		mediaAdded := stats.MediaAdded
		statsMutex.Unlock()

		totalRequests := pendingCount + dailyStats.ApprovedCount + dailyStats.DeclinedCount

		msg := "┌──────────────────┐\n"
		msg += "│  📊 每日数据看板 │\n"
		msg += "└──────────────────┘\n\n"
		msg += fmt.Sprintf("📅 %s · %s\n\n", today, weekday)

		msg += "┌─ 求片统计 ─────────┐\n"
		msg += fmt.Sprintf("│ 总计    │%12d │\n", totalRequests)
		msg += "├─────────────────────┤\n"
		msg += fmt.Sprintf("│ ⏳ 待处理 │%11d │\n", pendingCount)
		msg += fmt.Sprintf("│ ✅ 已批准 │%11d │\n", dailyStats.ApprovedCount)
		msg += fmt.Sprintf("│ ❌ 已拒绝 │%11d │\n", dailyStats.DeclinedCount)
		msg += fmt.Sprintf("│ 🎉 已可用 │%11d │\n", dailyStats.AvailableCount)
		msg += "└─────────────────────┘\n"

		msg += "┌─ 今日概览 ─────────┐\n"
		msg += fmt.Sprintf("│ 🐛 问题报告 │%9d │\n", issueCount)
		msg += fmt.Sprintf("│ 📀 新增媒体 │%9d │\n", mediaAdded)
		msg += "└─────────────────────┘"

		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/search":
		// Enhanced search with filters
		// Usage: /search  [筛选参数]
		// Filters: --year=2024 --type=movie --rating=7 --genre=action
		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法: [筛选条件]\n\n"+
				"*筛选条件:*\n"+
				"• `--type=movie` - 只搜索电影\n"+
				"• `--type=tv` - 只搜索剧集\n"+
				"• `--year=2024` - 指定年份\n"+
				"• `--rating=7` - 最低评分\n"+
				"• `--genre=动作` - 类型筛选\n\n"+
				"*示例:*\n"+
				"/search 漫威 --type=movie --year=2024 --rating=7", nil)
			return
		}

		// Parse query and filters
		var queryParts []string
		filter := SearchFilter{
			MinRating: 0,
		}

		for _, part := range parts[1:] {
			if strings.HasPrefix(part, "--") {
				// This is a filter
				kv := strings.TrimPrefix(part, "--")
				if strings.Contains(kv, "=") {
					parts := strings.SplitN(kv, "=", 2)
					key := strings.ToLower(parts[0])
					value := parts[1]

					switch key {
					case "type":
						filter.MediaType = value
					case "year":
						filter.Year = value
					case "rating":
						if rating, err := strconv.ParseFloat(value, 64); err == nil {
							filter.MinRating = rating
						}
					case "genre":
						filter.Genre = value
					}
				}
			} else {
				queryParts = append(queryParts, part)
			}
		}

		query := strings.Join(queryParts, " ")

		if smartSearch != nil && smartSearch.apiKey != "" {
			// Use smart search with filters
			results, err := smartSearch.SearchWithFilter(query, filter)
			if err != nil {
				sendPrivateMessage(update.Message.From.ID, "❌ 搜索失败: "+err.Error(), nil)
				log.Printf("Error searching with filter: %v", err)
				return
			}

			msg := FormatSearchResultsWithDetails(results, query)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else if jellyseerrClient != nil {
			// Fallback to basic search
			results, err := jellyseerrClient.SearchMedia(query)
			if err != nil {
				sendPrivateMessage(update.Message.From.ID, "❌ 搜索失败: "+err.Error(), nil)
				log.Printf("Error searching media: %v", err)
				return
			}

			msg := FormatSearchResults(results, query)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 搜索功能暂不可用", nil)
		}

	case "/stuck":
		// Show stuck requests (admin only)
		adminsMutex.RLock()
		_, isCallerAdmin := admins[userID]
		adminsMutex.RUnlock()

		if !isCallerAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员", nil)
			return
		}

		stuck := GetStuckRequests()
		msg := FormatStuckRequests(stuck)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/request":
		// Direct request by TMDB ID
		parts := strings.Fields(text)
		if len(parts) < 3 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法: \n\n类型: movie 或 tv\n\n示例: /request 12345 movie", nil)
			return
		}

		tmdbID, err := strconv.Atoi(parts[1])
		if err != nil {
			sendPrivateMessage(update.Message.From.ID, "❌ 无效的 TMDB ID", nil)
			return
		}

		mediaType := strings.ToLower(parts[2])
		if mediaType != "movie" && mediaType != "tv" {
			sendPrivateMessage(update.Message.From.ID, "❌ 类型必须是 movie 或 tv", nil)
			return
		}

		if jellyseerrClient == nil {
			sendPrivateMessage(update.Message.From.ID, "❌ Jellyseerr API 未配置", nil)
			return
		}

		// For now, just show the request info
		// In real implementation, you'd need to map Telegram user to Jellyseerr user
		msg := fmt.Sprintf("📝 *发起请求*\n\n")
		msg += fmt.Sprintf("TMDB ID: %d\n", tmdbID)
		msg += fmt.Sprintf("类型: %s\n\n", map[string]string{"movie": "电影", "tv": "剧集"}[mediaType])
		msg += "⚠️ 需要先绑定 Jellyseerr 账号\n\n"
		msg += "请联系管理员绑定账号后使用此功能"
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/addadmin":
		// Check if user is admin
		adminsMutex.RLock()
		_, isCallerAdmin := admins[userID]
		adminsMutex.RUnlock()

		if !isCallerAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权添加其他管理员", nil)
			return
		}

		// Parse target user ID from command (format: /addadmin  )
		parts := strings.Fields(text)
		if len(parts) < 2 {
			msg := "❓ 用法: [姓名]\n\n"
			msg += "示例:\n"
			msg += "/addadmin 123456 张三\n\n"
			msg += "💡 提示: 你可以用 /status 查看你自己的用户ID"
			sendPrivateMessage(update.Message.From.ID, msg, nil)
			return
		}

		targetUserID := parts[1]
		targetName := targetUserID
		if len(parts) >= 3 {
			targetName = strings.Join(parts[2:], " ")
		}

		adminsMutex.Lock()
		admins[targetUserID] = targetName
		adminsMutex.Unlock()
		saveAdminsToFile()

		msg := fmt.Sprintf("✅ *管理员添加成功*\n\n")
		msg += fmt.Sprintf("用户ID: `%s`\n", targetUserID)
		msg += fmt.Sprintf("姓名: %s\n", targetName)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

		log.Printf("管理员 %s 添加了新管理员: %s (%s)", username, targetName, targetUserID)

	case "/deladmin":
		// Check if user is admin
		adminsMutex.RLock()
		_, isCallerAdmin := admins[userID]
		adminsMutex.RUnlock()

		if !isCallerAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权删除其他管理员", nil)
			return
		}

		// Parse target user ID from command (format: /deladmin )
		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法:\n\n示例: /deladmin 123456", nil)
			return
		}

		targetUserID := parts[1]

		// Prevent self-deletion
		if targetUserID == userID {
			sendPrivateMessage(update.Message.From.ID, "❌ 不能删除自己，请使用 /unregister", nil)
			return
		}

		adminsMutex.Lock()
		if _, exists := admins[targetUserID]; exists {
			delete(admins, targetUserID)
			adminsMutex.Unlock()

			msg := fmt.Sprintf("✅ *管理员删除成功*\n\n")
			msg += fmt.Sprintf("用户ID: `%s`", targetUserID)
			sendPrivateMessage(update.Message.From.ID, msg, nil)

			log.Printf("Admin %s removed admin: %s", username, targetUserID)
		} else {
			adminsMutex.Unlock()
			sendPrivateMessage(update.Message.From.ID, "❌ 该用户不是管理员", nil)
		}

	case "/pending":
		// Check if user is admin
		adminsMutex.RLock()
		_, isCallerAdmin := admins[userID]
		adminsMutex.RUnlock()

		if !isCallerAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权查看待处理请求", nil)
			return
		}

		if jellyseerrClient == nil {
			sendPrivateMessage(update.Message.From.ID, "❌ Jellyseerr API 未配置", nil)
			return
		}

		requests, err := jellyseerrClient.GetPendingRequests()
		if err != nil {
			// 检查是否是403权限错误
			if strings.Contains(err.Error(), "403") {
				msg := "❌ 无法访问 Jellyseerr API\n\n"
				msg += "可能原因：\n"
				msg += "• API Key 权限不足\n"
				msg += "• API Key 配置错误\n\n"
				msg += "请检查 Jellyseerr 设置中的 API Key 权限"
				sendPrivateMessage(update.Message.From.ID, msg, nil)
				return
			}
			sendPrivateMessage(update.Message.From.ID, "❌ 获取失败: "+err.Error(), nil)
			log.Printf("Error getting pending requests: %v", err)
			return
		}

		msg := FormatPendingRequests(requests)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/approve":
		// Check if user is admin
		adminsMutex.RLock()
		_, isCallerAdmin := admins[userID]
		adminsMutex.RUnlock()

		if !isCallerAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权批准请求", nil)
			return
		}

		if jellyseerrClient == nil {
			sendPrivateMessage(update.Message.From.ID, "❌ Jellyseerr API 未配置", nil)
			return
		}

		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法:\n\n示例: /approve 123", nil)
			return
		}

		requestID, err := strconv.Atoi(parts[1])
		if err != nil {
			sendPrivateMessage(update.Message.From.ID, "❌ 无效的请求ID", nil)
			return
		}

		if err := jellyseerrClient.ApproveRequest(requestID); err != nil {
			sendPrivateMessage(update.Message.From.ID, "❌ 批准失败: "+err.Error(), nil)
			return
		}

		msg := fmt.Sprintf("✅ *请求已批准*\n\n请求ID: `%d`\n\nJellyseerr 将自动下载此内容。", requestID)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/decline":
		// Check if user is admin
		adminsMutex.RLock()
		_, isCallerAdmin := admins[userID]
		adminsMutex.RUnlock()

		if !isCallerAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权拒绝请求", nil)
			return
		}

		if jellyseerrClient == nil {
			sendPrivateMessage(update.Message.From.ID, "❌ Jellyseerr API 未配置", nil)
			return
		}

		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法:\n\n示例: /decline 123", nil)
			return
		}

		requestID, err := strconv.Atoi(parts[1])
		if err != nil {
			sendPrivateMessage(update.Message.From.ID, "❌ 无效的请求ID", nil)
			return
		}

		if err := jellyseerrClient.DeclineRequest(requestID); err != nil {
			sendPrivateMessage(update.Message.From.ID, "❌ 拒绝失败: "+err.Error(), nil)
			return
		}

		msg := fmt.Sprintf("❌ *请求已拒绝*\n\n请求ID: `%d`", requestID)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/top":
		// Get top media
		topMedia := GetTopMedia(10)
		msg := FormatTopMedia(topMedia)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/activity":
		// Get top users
		topUsers := GetTopUsers(10)
		msg := FormatTopUsers(topUsers)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/trends":
		// Get trends for last 7 days
		trends := GetTrends(7)
		msg := FormatTrends(trends)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/prefs", "/preferences":
		// Show user preferences
		prefs := prefManager.GetPreferences(userID, username)
		msg := FormatPreferences(prefs)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/setprefs":
		// Parse preference command
		// Usage: /setprefs  
		parts := strings.Fields(text)
		if len(parts) < 3 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法: \n\n"+
				"*可用选项:*\n"+
				"• movies on/off - 电影通知\n"+
				"• series on/off - 剧集通知\n"+
				"• issues on/off - 问题通知\n"+
				"• quiet on/off - 勿扰模式\n"+
				"• quietstart HH:MM - 勿扰开始时间\n"+
				"• quietend HH:MM - 勿扰结束时间\n"+
				"• whitelist  - 添加白名单关键词\n"+
				"• blacklist  - 添加黑名单关键词\n\n"+
				"示例:\n"+
				"/setprefs movies on\n"+
				"/setprefs quiet on\n"+
				"/setprefs quietstart 22:00\n"+
				"/setprefs whitelist 4K", nil)
			return
		}

		prefs := prefManager.GetPreferences(userID, username)
		key := strings.ToLower(parts[1])
		value := strings.Join(parts[2:], " ")

		switch key {
		case "movies":
			prefs.NotifyMovies = (value == "on" || value == "yes" || value == "true")
		case "series":
			prefs.NotifySeries = (value == "on" || value == "yes" || value == "true")
		case "issues":
			prefs.NotifyIssues = (value == "on" || value == "yes" || value == "true")
		case "approved":
			prefs.NotifyApproved = (value == "on" || value == "yes" || value == "true")
		case "available":
			prefs.NotifyAvailable = (value == "on" || value == "yes" || value == "true")
		case "quiet":
			prefs.QuietHoursEnabled = (value == "on" || value == "yes" || value == "true")
		case "quietstart":
			prefs.QuietHoursStart = value
		case "quietend":
			prefs.QuietHoursEnd = value
		case "whitelist":
			prefs.KeywordsWhitelist = append(prefs.KeywordsWhitelist, value)
		case "blacklist":
			prefs.KeywordsBlacklist = append(prefs.KeywordsBlacklist, value)
		default:
			sendPrivateMessage(update.Message.From.ID, "❌ 未知选项: "+key, nil)
			return
		}

		prefManager.SetPreferences(userID, prefs)
		msg := "✅ *设置已更新*\n\n" + FormatPreferences(prefs)
		sendPrivateMessage(update.Message.From.ID, msg, nil)

	case "/resetprefs":
		// Reset preferences to default
		prefs := &UserPreferences{
			UserID:            userID,
			Username:          username,
			NotifyMovies:      true,
			NotifySeries:      true,
			NotifyIssues:      true,
			NotifyApproved:    true,
			NotifyAvailable:   true,
			MinVoteAverage:    0,
			QuietHoursEnabled: false,
			QuietHoursStart:   "22:00",
			QuietHoursEnd:     "08:00",
			KeywordsWhitelist: []string{},
			KeywordsBlacklist: []string{},
		}
		prefManager.SetPreferences(userID, prefs)
		sendPrivateMessage(update.Message.From.ID, "✅ 偏好已重置为默认设置", nil)

	case "/quota":
		// Show user's request quota
		if smartSearchMgr != nil {
			quotaInfo := smartSearchMgr.GetUserQuotaInfo(update.Message.From.ID)
			sendPrivateMessage(update.Message.From.ID, quotaInfo, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 配额功能暂不可用", nil)
		}

	case "/link":
		// Link Jellyseerr account with verification code
		handleLinkCommand(update.Message.From.ID, username, text)

	case "/verify":
		// Generate verification code for linking
		handleVerifyCommand(update.Message.From.ID, username)

	case "/users":
		// Show all user mappings (admin only)
		userIDStr := fmt.Sprintf("%d", update.Message.From.ID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权查看用户列表", nil)
			return
		}

		if userSyncMgr != nil {
			msg := userSyncMgr.FormatUserMappings()
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 用户同步功能暂不可用", nil)
		}

	case "/unlink":
		// Unlink Jellyseerr account
		handleUnlinkCommand(update.Message.From.ID)

	case "/bindrequests":
		// Show all pending binding requests (admin only)
		userIDStr := fmt.Sprintf("%d", update.Message.From.ID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权查看绑定请求", nil)
			return
		}

		if userSyncMgr != nil {
			msg := userSyncMgr.FormatBindingRequests()
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 用户同步功能暂不可用", nil)
		}

	case "/approvebind":
		// Approve a binding request (admin only)
		userIDStr := fmt.Sprintf("%d", update.Message.From.ID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权批准绑定", nil)
			return
		}

		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法:\n\n使用 /bindrequests 查看待处理的请求", nil)
			return
		}

		requestID := parts[1]
		if userSyncMgr != nil {
			if err := userSyncMgr.ApproveBindingRequest(requestID, update.Message.From.ID); err != nil {
				sendPrivateMessage(update.Message.From.ID, "❌ 批准失败: "+err.Error(), nil)
			} else {
				// Get the request details to notify the user
				req := userSyncMgr.GetBindingRequestByID(requestID)
				if req != nil {
					msg := "✅ *绑定请求已批准*\n\n"
					msg += fmt.Sprintf("👤 用户: %s (ID: `%d`)\n", req.TelegramName, req.TelegramID)
					msg += fmt.Sprintf("🎬 Jellyseerr: %s (@%s)\n\n", req.JellyseerrName, req.JellyseerrUsername)
					msg += "账号绑定成功！"

					sendPrivateMessage(update.Message.From.ID, msg, nil)

					// Notify the user
					userMsg := "🎉 *账号绑定成功*\n\n"
					userMsg += fmt.Sprintf("你的账号已成功绑定到: *%s*\n\n", req.JellyseerrName)
					userMsg += "现在你可以使用求片功能了！\n\n"
					userMsg += "💡 使用 /search 搜索媒体"
					sendPrivateMessage(req.TelegramID, userMsg, nil)
				} else {
					sendPrivateMessage(update.Message.From.ID, "✅ 绑定请求已批准", nil)
				}
			}
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 用户同步功能暂不可用", nil)
		}

	case "/rejectbind":
		// Reject a binding request (admin only)
		userIDStr := fmt.Sprintf("%d", update.Message.From.ID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权拒绝绑定", nil)
			return
		}

		parts := strings.Fields(text)
		if len(parts) < 2 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法:\n\n使用 /bindrequests 查看待处理的请求", nil)
			return
		}

		requestID := parts[1]
		if userSyncMgr != nil {
			if err := userSyncMgr.RejectBindingRequest(requestID, update.Message.From.ID); err != nil {
				sendPrivateMessage(update.Message.From.ID, "❌ 拒绝失败: "+err.Error(), nil)
			} else {
				// Get the request details to notify the user
				req := userSyncMgr.GetBindingRequestByID(requestID)
				msg := "❌ *绑定请求已拒绝*\n\n"
				if req != nil {
					msg += fmt.Sprintf("👤 用户: %s\n", req.TelegramName)
					msg += fmt.Sprintf("🎬 Jellyseerr: %s\n\n", req.JellyseerrName)

					// Notify the user
					userMsg := "❌ *绑定请求被拒绝*\n\n"
					userMsg += "你的绑定请求已被管理员拒绝。\n\n"
					userMsg += "如果你认为这是错误，请联系管理员。"
					sendPrivateMessage(req.TelegramID, userMsg, nil)
				}
				sendPrivateMessage(update.Message.From.ID, msg, nil)
			}
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 用户同步功能暂不可用", nil)
		}

	case "/mapuser":
		// Admin command to manually map users
		userIDStr := fmt.Sprintf("%d", update.Message.From.ID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 你不是管理员，无权映射用户", nil)
			return
		}

		parts := strings.Fields(text)
		if len(parts) < 3 {
			sendPrivateMessage(update.Message.From.ID, "❓ 用法: \n\n示例: /mapuser 123456 789", nil)
			return
		}

		telegramID, err1 := strconv.ParseInt(parts[1], 10, 64)
		jellyseerrID, err2 := strconv.ParseInt(parts[2], 10, 64)
		if err1 != nil || err2 != nil {
			sendPrivateMessage(update.Message.From.ID, "❌ 无效的用户 ID", nil)
			return
		}

		if userSyncMgr != nil {
			if err := userSyncMgr.SetUserMapping(telegramID, jellyseerrID); err != nil {
				sendPrivateMessage(update.Message.From.ID, "❌ 映射失败: "+err.Error(), nil)
			} else {
				msg := fmt.Sprintf("✅ *用户映射成功*\n\n")
				msg += fmt.Sprintf("Telegram ID: %d\n", telegramID)
				msg += fmt.Sprintf("Jellyseerr ID: %d\n", jellyseerrID)
				sendPrivateMessage(update.Message.From.ID, msg, nil)
			}
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 用户同步功能暂不可用", nil)
		}

	// === 新增命令处理 ===

	case "/profile", "/card":
		// Show user profile card
		if engagementSys != nil {
			displayName := update.Message.From.FirstName
			if update.Message.From.LastName != "" {
				displayName += " " + update.Message.From.LastName
			}
			if update.Message.From.Username != "" {
				displayName += " @" + update.Message.From.Username
			}
			msg := engagementSys.FormatUserCard(update.Message.From.ID, displayName)
			sendPrivateMessage(update.Message.From.ID, msg, nil)

			// Record activity
			engagementSys.RecordActivity(update.Message.From.ID, "login", 1)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 用户功能暂不可用", nil)
		}

	case "/daily", "/checkin", "/bonus", "/signin":
		// Claim daily bonus
		if engagementSys != nil {
			msg, _, err := engagementSys.ClaimDailyBonus(update.Message.From.ID)
			if err != nil {
				sendPrivateMessage(update.Message.From.ID, "❌ "+err.Error(), nil)
			} else {
				sendPrivateMessage(update.Message.From.ID, msg, nil)
				// Try drop reward
				TryDropReward(update.Message.From.ID, "每日签到")
			}
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 签到功能暂不可用", nil)
		}

	case "/leaderboard", "/lb":
		// Show leaderboard
		if engagementSys != nil {
			msg := engagementSys.FormatLeaderboard(10)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 排行榜功能暂不可用", nil)
		}

	case "/challenges", "/tasks", "/dailies":
		// Show daily challenges
		if engagementSys != nil {
			msg := engagementSys.FormatChallenges(update.Message.From.ID)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 挑战功能暂不可用", nil)
		}

	case "/badges", "/achievements", "/trophies":
		// Show user badges
		if engagementSys != nil {
			user := engagementSys.GetUserData(update.Message.From.ID)
			if len(user.Badges) == 0 {
				msg := "🏅 *我的成就*\n\n你还没有获得任何成就\n\n"
				msg += "💡 多使用机器人可以获得成就！"
				sendPrivateMessage(update.Message.From.ID, msg, nil)
			} else {
				msg := "🏅 *我的成就*\n\n"
				for _, badge := range user.Badges {
					msg += fmt.Sprintf("  %s\n", engagementSys.getBadgeEmoji(badge))
				}
				msg += fmt.Sprintf("\n共 %d 个成就", len(user.Badges))
				sendPrivateMessage(update.Message.From.ID, msg, nil)
			}
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 成就功能暂不可用", nil)
		}

	case "/recommend", "/rec", "/suggest":
		// Show recommendations
		sendPrivateMessage(update.Message.From.ID, "🎯 *智能推荐*\n\n功能开发中，敬请期待！", nil)

	case "/trending", "/hot":
		// Show trending searches
		if searchHistoryMgr != nil {
			trending := searchHistoryMgr.GetTrendingSearches(10)
			msg := "🔥 *热门搜索*\n\n"
			if len(trending) == 0 {
				msg += "暂无热门搜索"
			} else {
				for i, item := range trending {
					msg += fmt.Sprintf("%d. %s (%d次)\n", i+1, item.Query, item.Count)
				}
			}
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 热门搜索功能暂不可用", nil)
		}

	case "/history", "/hist":
		// Show search history
		if searchHistoryMgr != nil {
			history := searchHistoryMgr.GetUserHistory(update.Message.From.ID, 20)
			msg := "📜 *搜索历史*\n\n"
			if len(history) == 0 {
				msg += "暂无搜索历史"
			} else {
				for i, item := range history {
					msg += fmt.Sprintf("%d. %s\n", i+1, item.Query)
				}
			}
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 搜索历史功能暂不可用", nil)
		}

	case "/quicklink", "/fastbind":
		// Quick link command
		handleQuickLinkCommand(update.Message.From.ID, text)

	default:
		// Try NLP parsing for natural language input
		if nlpParser != nil {
			intent, params, err := ParseNLP(text)
			if err == nil && intent != IntentUnknown {
				log.Printf("NLP parsed: intent=%s, query=%s", intent, params.Query)
				handleNaturalLanguageIntent(update.Message.From.ID, intent, params)
				return
			}
		}
		sendPrivateMessage(update.Message.From.ID, "❓ 未知命令。请发送 /help 查看帮助。", nil)
	}
}

// handleNaturalLanguageIntent handles NLP-parsed intents
func handleNaturalLanguageIntent(userID int64, intent Intent, params *SearchParams) {
	switch intent {
	case IntentSearch, IntentRequest, IntentMovie, IntentTV:
		// Handle search with smart search manager
		handleSmartSearch(userID, params)
	case IntentStatus:
		// Show user's status
		handleMyRequestsPrivate(userID)
	case IntentHelp:
		// Show help
		msg := GetHelpMessage(LevelNormal)
		sendPrivateMessage(userID, msg, nil)
	case IntentStats:
		// Show statistics if admin
		userIDStr := fmt.Sprintf("%d", userID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()
		if isAdmin {
			// Get today's stats from analytics system
			today := time.Now().Format("2006-01-02")
			weekday := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[time.Now().Weekday()]

			analytics.mutex.RLock()
			var dailyStats *DailyCount
			if ds, exists := analytics.DailyStats[today]; exists {
				dailyStats = ds
			} else {
				dailyStats = &DailyCount{Date: today}
			}

			// Count pending requests for today
			pendingCount := 0
			for _, req := range analytics.Requests {
				if req.CreatedAt.Format("2006-01-02") == today && req.Status == "pending" {
					pendingCount++
				}
			}
			analytics.mutex.RUnlock()

			// Get current stats for issue count and media added
			statsMutex.Lock()
			issueCount := stats.IssueCount
			mediaAdded := stats.MediaAdded
			statsMutex.Unlock()

			totalRequests := pendingCount + dailyStats.ApprovedCount + dailyStats.DeclinedCount

			statsMsg := "┌──────────────────┐\n"
			statsMsg += "│  📊 每日数据看板 │\n"
			statsMsg += "└──────────────────┘\n\n"
			statsMsg += fmt.Sprintf("📅 %s · %s\n\n", today, weekday)

			statsMsg += "┌─ 求片统计 ─────────┐\n"
			statsMsg += fmt.Sprintf("│ 总计    │%12d │\n", totalRequests)
			statsMsg += "├─────────────────────┤\n"
			statsMsg += fmt.Sprintf("│ ⏳ 待处理 │%11d │\n", pendingCount)
			statsMsg += fmt.Sprintf("│ ✅ 已批准 │%11d │\n", dailyStats.ApprovedCount)
			statsMsg += fmt.Sprintf("│ ❌ 已拒绝 │%11d │\n", dailyStats.DeclinedCount)
			statsMsg += fmt.Sprintf("│ 🎉 已可用 │%11d │\n", dailyStats.AvailableCount)
			statsMsg += "└─────────────────────┘\n"

			statsMsg += "┌─ 今日概览 ─────────┐\n"
			statsMsg += fmt.Sprintf("│ 🐛 问题报告 │%9d │\n", issueCount)
			statsMsg += fmt.Sprintf("│ 📀 新增媒体 │%9d │\n", mediaAdded)
			statsMsg += "└─────────────────────┘"

			sendPrivateMessage(userID, statsMsg, nil)
		} else {
			sendPrivateMessage(userID, "❌ 只有管理员可以查看统计数据", nil)
		}
	case IntentAdmin:
		// Check if admin and show admin panel
		if IsAdmin(userID) {
			msg, keyboard, _ := SendAdminPanel(userID)
			sendPrivateMessage(userID, msg, keyboard)
		} else {
			sendPrivateMessage(userID, "❌ 你不是管理员", nil)
		}
	case IntentQuota:
		// Show user's quota information
		log.Printf("[DEBUG] Handling quota intent for user %d", userID)
		if smartSearchMgr != nil {
			quotaInfo := smartSearchMgr.GetUserQuotaInfo(userID)
			log.Printf("[DEBUG] Quota info: %s", quotaInfo)
			err := sendPrivateMessage(userID, quotaInfo, nil)
			if err != nil {
				log.Printf("[ERROR] Error sending quota info: %v", err)
			} else {
				log.Printf("[DEBUG] Quota info sent successfully")
			}
		} else {
			log.Printf("[DEBUG] SmartSearchMgr is nil")
			sendPrivateMessage(userID, "❌ 配额功能暂不可用", nil)
		}
	case IntentLink:
		// Link Jellyseerr account
		linkCmd := "/link"
		if params.Query != "" {
			linkCmd = "/link " + params.Query
		}
		handleLinkCommand(userID, "", linkCmd)
	case IntentVerify:
		// Generate verification code
		handleVerifyCommand(userID, "")
	case IntentUnlink:
		// Unlink account
		handleUnlinkCommand(userID)
	case IntentUsers:
		// Show user list (admin only)
		userIDStr := fmt.Sprintf("%d", userID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(userID, "❌ 你不是管理员，无权查看用户列表", nil)
		} else if userSyncMgr != nil {
			msg := userSyncMgr.FormatUserMappings()
			sendPrivateMessage(userID, msg, nil)
		} else {
			sendPrivateMessage(userID, "❌ 用户同步功能暂不可用", nil)
		}
	default:
		// Fallback to help
		msg := GetHelpMessage(LevelSimple)
		sendPrivateMessage(userID, msg, nil)
	}
}

// handleSmartSearch handles smart search with NLP params
func handleSmartSearch(userID int64, params *SearchParams) {
	if smartSearchMgr == nil {
		sendPrivateMessage(userID, "❌ 搜索功能暂不可用", nil)
		return
	}

	query := params.Query
	if query == "" {
		// Show quick search menu
		msg, keyboard := FormatQuickSearchMenu(userID)
		sendPrivateMessage(userID, msg, keyboard)
		return
	}

	// Check for special keywords that should show quota info instead of searching
	if strings.Contains(query, "配额") || strings.Contains(query, "限额") || strings.Contains(query, "quota") || strings.Contains(query, "limit") {
		if smartSearchMgr != nil {
			quotaInfo := smartSearchMgr.GetUserQuotaInfo(userID)
			sendPrivateMessage(userID, quotaInfo, nil)
		} else {
			sendPrivateMessage(userID, "❌ 配额功能暂不可用", nil)
		}
		return
	}

	// Create search context
	ctx := &SearchContext{
		UserID: userID,
		Query:  query,
		Params: params,
	}

	// Perform search
	if err := smartSearchMgr.Search(ctx); err != nil {
		log.Printf("Search error: %v", err)
		sendPrivateMessage(userID, "❌ 搜索失败，请稍后再试", nil)
		return
	}

	// Format and send results
	msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
	sendPrivateMessage(userID, msg, keyboard)
}

// telegramWebhookHandler handles updates from Telegram
func telegramWebhookHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("[DEBUG] telegramWebhookHandler ENTRY")
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[PANIC] telegramWebhookHandler recovered: %v", err)
			debug.PrintStack()
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}()

	var update TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Error decoding update: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] telegramWebhookHandler called, callback=%v", update.CallbackQuery != nil)

	// Handle callback queries (button presses)
	if update.CallbackQuery != nil {
		callbackID := update.CallbackQuery.ID
		data := update.CallbackQuery.Data
		userID := update.CallbackQuery.From.ID
		username := update.CallbackQuery.From.FirstName

		// Save Telegram username for issue reporting
		if userSyncMgr != nil && update.CallbackQuery.From.Username != "" {
			userSyncMgr.SetTelegramUsername(userID, update.CallbackQuery.From.Username)
		}

		log.Printf("Callback query from user %d: %s", userID, data)

		// Parse callback data
		// Support both formats: "action:args" (new) and "action_args" (legacy)
		var action string
		var args string
		var parts []string // Keep for legacy compatibility

		if strings.Contains(data, ":") {
			colonParts := strings.SplitN(data, ":", 2)
			action = colonParts[0]
			if len(colonParts) > 1 {
				args = colonParts[1]
				// Also split args by underscore for parts
				parts = strings.Split(args, "_")
			}
		} else {
			parts = strings.Split(data, "_")
			action = parts[0]
			if len(parts) > 1 {
				args = strings.Join(parts[1:], "_")
			}
		}

		if action != "" {
			var responseText string
			var editMessage bool
			var newMsg string
			var newKeyboard *TelegramInlineKeyboard

			switch action {
			case "approve", "decline":
				// Admin only actions (legacy)
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					answerCallbackQuery(callbackID, "❌ 你不是管理员")
					w.WriteHeader(http.StatusOK)
					return
				}
				responseText = handleRequestAction(action, args)

			case "approve_bind":
				// Approve binding request (admin only)
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					answerCallbackQuery(callbackID, "❌ 你不是管理员")
					w.WriteHeader(http.StatusOK)
					return
				}

				if userSyncMgr != nil {
					if err := userSyncMgr.ApproveBindingRequest(args, userID); err != nil {
						responseText = "❌ " + err.Error()
					} else {
						// Get request details for notification
						req := userSyncMgr.GetBindingRequestByID(args)
						editMessage = true
						newMsg = "✅ *绑定请求已批准*\n\n"
						if req != nil {
							newMsg += fmt.Sprintf("👤 用户: %s (ID: `%d`)\n", req.TelegramName, req.TelegramID)
							newMsg += fmt.Sprintf("🎬 Jellyseerr: %s (@%s)\n\n", req.JellyseerrName, req.JellyseerrUsername)
							newMsg += "账号绑定成功！"

							// Notify the user
							userMsg := "🎉 *账号绑定成功*\n\n"
							userMsg += fmt.Sprintf("你的账号已成功绑定到: *%s*\n\n", req.JellyseerrName)
							userMsg += "现在你可以使用求片功能了！\n\n"
							userMsg += "💡 使用 /search 搜索媒体"
							sendPrivateMessage(req.TelegramID, userMsg, nil)
						} else {
							newMsg += "绑定请求已批准"
						}
						newKeyboard = nil // Remove buttons
						responseText = "✅ 已批准"
					}
				} else {
					responseText = "❌ 用户同步功能暂不可用"
				}

			case "reject_bind":
				// Reject binding request (admin only)
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					answerCallbackQuery(callbackID, "❌ 你不是管理员")
					w.WriteHeader(http.StatusOK)
					return
				}

				if userSyncMgr != nil {
					if err := userSyncMgr.RejectBindingRequest(args, userID); err != nil {
						responseText = "❌ " + err.Error()
					} else {
						// Get request details for notification
						req := userSyncMgr.GetBindingRequestByID(args)
						editMessage = true
						newMsg = "❌ *绑定请求已拒绝*\n\n"
						if req != nil {
							newMsg += fmt.Sprintf("👤 用户: %s\n", req.TelegramName)
							newMsg += fmt.Sprintf("🎬 Jellyseerr: %s\n\n", req.JellyseerrName)

							// Notify the user
							userMsg := "❌ *绑定请求被拒绝*\n\n"
							userMsg += "你的绑定请求已被管理员拒绝。\n\n"
							userMsg += "如果你认为这是错误，请联系管理员。"
							sendPrivateMessage(req.TelegramID, userMsg, nil)
						}
						newKeyboard = nil // Remove buttons
						responseText = "❌ 已拒绝"
					}
				} else {
					responseText = "❌ 用户同步功能暂不可用"
				}

			case "list_bind_requests":
				// Show all pending binding requests (admin only)
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					answerCallbackQuery(callbackID, "❌ 你不是管理员")
					w.WriteHeader(http.StatusOK)
					return
				}

				if userSyncMgr != nil {
					newMsg = userSyncMgr.FormatBindingRequestsWithButtons()
					editMessage = true
					responseText = "📋 已刷新"
				} else {
					responseText = "❌ 用户同步功能暂不可用"
				}

			case "request":
				// User self-service request - one-click request
				if len(parts) >= 3 {
					tmdbID, _ := strconv.Atoi(parts[1])
					mediaType := parts[2]

					// Try to create request
					if smartSearchMgr != nil {
						msg, err := smartSearchMgr.CreateOneClickRequest(userID, tmdbID, mediaType)
						if err != nil {
							responseText = "❌ 请求失败: " + err.Error()
						} else {
							// Edit the message to show request details
							newMsg = msg
							editMessage = true
							responseText = "✅ 已生成请求链接"
						}
					} else {
						newMsg = HandleQuickRequest(userID, tmdbID, mediaType)
						editMessage = true
						responseText = "✅ 已生成请求链接"
					}
				} else {
					responseText = "❌ 无效的请求"
				}

			case "search":
				// Quick search from button
				query := strings.Join(parts[1:], "_")
				if smartSearchMgr != nil {
					newMsg, newKeyboard, _ = HandleQuickSearchCallback(userID, query)
					editMessage = true
				} else {
					responseText = "❌ 搜索功能暂不可用"
				}

			case "onboard":
				// Onboarding flow
				onboardAction := strings.Join(parts[1:], "_")
				newMsg, newKeyboard, _ = HandleOnboardingCallback(userID, username, onboardAction)
				editMessage = true

			case "action":
				// Quick action buttons
				if len(parts) >= 2 {
					subAction := parts[1]
					switch subAction {
					case "search":
						newMsg, newKeyboard = FormatQuickSearchMenu(userID)
						editMessage = true
					case "myrequests":
						handleMyRequestsPrivate(userID)
						responseText = "✅ 已显示你的请求"
					case "help":
						newMsg = GetHelpMessage(LevelNormal)
						editMessage = true
					case "settings":
						newMsg = "⚙️ *设置*\n\n使用 /prefs 查看和修改设置"
						editMessage = true
					default:
						responseText = "✅ 已操作"
					}
				} else {
					responseText = "✅ 已操作"
				}

			case "admin":
				// Admin panel actions
				adminAction := strings.Join(parts[1:], "_")
				params := map[string]string{}
				var err error
				newMsg, newKeyboard, err = HandleAdminCallback(userID, adminAction, params)
				if err != nil {
					responseText = "❌ " + err.Error()
				} else {
					editMessage = true
				}

			case "page":
				// Pagination for search results
				// Format: page__
				if len(parts) >= 3 {
					query := strings.Join(parts[1:len(parts)-1], "_")
					pageNum, _ := strconv.Atoi(parts[len(parts)-1])

					if smartSearchMgr != nil {
						newMsg, newKeyboard = smartSearchMgr.FormatPageResults(userID, query, pageNum)
						if newMsg != "" {
							editMessage = true
							responseText = "✅ 已翻页"
						} else {
							responseText = "❌ 翻页失败，搜索结果已过期"
						}
					} else {
						responseText = "❌ 搜索功能暂不可用"
					}
				} else {
					responseText = "❌ 无效的分页"
				}

			case "request_link":
				// User clicked to request linking an account
				// Support both "request_link:1" (colon) and "request_link_1" (underscore) formats
				var jellyseerrID int64
				var parseErr error

				// Try to get the ID from args (colon format) or parts[1] (underscore format)
				if args != "" {
					jellyseerrID, parseErr = strconv.ParseInt(args, 10, 64)
				} else if len(parts) >= 2 {
					jellyseerrID, parseErr = strconv.ParseInt(parts[1], 10, 64)
				}

				if parseErr == nil && jellyseerrID > 0 {
					if userSyncMgr != nil {
						// Get user profile
						profile, err := userSyncMgr.GetUserProfile(jellyseerrID)
						if err != nil {
							responseText = "❌ 获取用户信息失败"
						} else {
							// Get display name with fallback
							displayName := profile.DisplayName
							if displayName == "" {
								displayName = profile.Username
							}
							if displayName == "" {
								displayName = profile.Email
							}
							if displayName == "" {
								displayName = profile.JellyfinUserID
							}
							if displayName == "" {
								displayName = fmt.Sprintf("用户 #%d", profile.ID)
							}

							// Get username for @ mention (use displayName as fallback since username is often null)
							usernameForMention := profile.Username
							if usernameForMention == "" {
								usernameForMention = profile.DisplayName
							}

							// Create binding request (admin approval system)
							bindingReq := userSyncMgr.CreateBindingRequest(
								int64(userID),
								username,
								jellyseerrID,
								displayName,
								usernameForMention,
							)

							// Notify admins
							notifyAdminsOfBindingRequest(bindingReq)

							// Edit message to show confirmation
							editMessage = true
							newMsg = "✅ *绑定请求已提交*\n\n"
							newMsg += fmt.Sprintf("你要绑定的账号: *%s*\n\n", displayName)
							newMsg += "📋 请求已发送给管理员审核\n\n"
							newMsg += "⏰ 请求有效期 24 小时\n\n"
							newMsg += "💡 管理员审核通过后，你将收到通知"
							newKeyboard = nil // Remove buttons

							responseText = "✅ 绑定请求已提交"
						}
					} else {
						responseText = "❌ 绑定功能暂不可用"
					}
				} else {
					responseText = "❌ 无效的绑定请求"
				}

			case "ignore":
				// Ignore button clicks (like page indicator)
				responseText = ""

			case "suggest":
				// Suggestion button clicked
				responseText = "💡 " + strings.Join(SuggestNextActions("search_empty"), "\n")

			case "more":
				// Show more search results
				responseText = "📋 更多功能开发中..."

			case "issue_reply":
				// Admin wants to reply to an issue
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					rtext, shouldEdit, msgForEdit, kbForEdit := handleIssueReplyCallback(issueID)
					// Store the pending reply state
					if pendingIssueReplies == nil {
						pendingIssueReplies = make(map[int64]int64)
					}
					pendingIssueReplies[userID] = issueID
					responseText = rtext
					// Set edit variables if callback wants to edit
					if shouldEdit {
						editMessage = shouldEdit
						newMsg = msgForEdit
						newKeyboard = kbForEdit
					}
				}

			case "issue_fixed", "issue_fix":
				// Quick reply: issue fixed (issue_fix is legacy alias)
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					responseText = handleIssueFixedCallback(issueID)
					// Update the message to show action completed
					editMessage = true
					newMsg = "✅ 已回复: 问题已修复\n\n问题将保持打开状态，直到用户确认或关闭。"
					newKeyboard = nil
				}

			case "issue_processing":
				// Quick reply: processing
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					responseText = handleIssueProcessingCallback(issueID)
					editMessage = true
					newMsg = "✅ 已回复: 正在处理中"
					newKeyboard = nil
				}

			case "issue_close":
				// Close/delete issue
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					responseText = handleIssueCloseCallback(issueID)
					editMessage = true
					newMsg = "✅ 问题已关闭"
					newKeyboard = nil
				}

			case "issue_template":
				// Template reply (format: issue_template:template_type:issue_id)
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					// Parse template type and issue ID from args
					// args format is "template_type:issue_id"
					templateParts := strings.Split(args, ":")
					if len(templateParts) == 2 {
						templateType := templateParts[0]
						issueID, _ := strconv.ParseInt(templateParts[1], 10, 64)
						rtext, shouldEdit, msgForEdit, kbForEdit := handleIssueTemplateCallback(templateType, issueID)
						responseText = rtext
						if shouldEdit {
							editMessage = shouldEdit
							newMsg = msgForEdit
							newKeyboard = kbForEdit
						}
					} else {
						responseText = "❌ 无效的模板参数"
					}
				}

			case "issue_custom":
				// Custom reply - enter text input mode
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					rtext, shouldEdit, msgForEdit, kbForEdit := handleIssueCustomReplyCallback(issueID, false)
					responseText = rtext
					if shouldEdit {
						editMessage = shouldEdit
						newMsg = msgForEdit
						newKeyboard = kbForEdit
					}
					// Send private chat reminder
					go sendPrivateMessage(userID, "💬 请发送回复内容:\n\n直接输入消息即可发送到 Jellyseerr", nil)
				}

			case "issue_custom_close":
				// Custom reply and close - enter text input mode, will close after reply
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					rtext, shouldEdit, msgForEdit, kbForEdit := handleIssueCustomReplyCallback(issueID, true)
					responseText = rtext
					if shouldEdit {
						editMessage = shouldEdit
						newMsg = msgForEdit
						newKeyboard = kbForEdit
					}
					// Send private chat reminder
					go sendPrivateMessage(userID, "💬 请发送回复内容:\n\n直接输入消息即可发送到 Jellyseerr\n\n完成后将自动关闭问题", nil)
				}

			case "issue_cancel":
				// Cancel reply operation
				userIDStr := fmt.Sprintf("%d", userID)
				adminsMutex.RLock()
				_, isAdmin := admins[userIDStr]
				adminsMutex.RUnlock()

				if !isAdmin {
					responseText = "❌ 你不是管理员"
				} else {
					issueID, _ := strconv.ParseInt(args, 10, 64)
					rtext, shouldEdit, msgForEdit, kbForEdit := handleIssueCancelCallback(issueID)
					responseText = rtext
					if shouldEdit {
						editMessage = shouldEdit
						newMsg = msgForEdit
						newKeyboard = kbForEdit
					}
				}

			default:
				responseText = "✅ 已操作"
			}

			// Answer the callback query
			if responseText != "" {
				answerCallbackQuery(callbackID, responseText)
			} else {
				answerCallbackQuery(callbackID, "")
			}

			// Edit the message if needed (check if Message exists)
			if update.CallbackQuery.Message != nil {
				log.Printf("[DEBUG] editMessage=%v, newMsg=%q, messageID=%d", editMessage, newMsg, update.CallbackQuery.Message.MessageID)
				if editMessage && newMsg != "" && update.CallbackQuery.Message.MessageID != 0 {
					log.Printf("[DEBUG] Editing message: chatID=%d, messageID=%d, newMsg=%q", update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, newMsg)
					if err := editMessageText(userID, update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID, newMsg, newKeyboard); err != nil {
						log.Printf("[DEBUG] Error editing message: %v", err)
					} else {
						log.Printf("[DEBUG] Message edited successfully")
					}
				}
			}

			w.WriteHeader(http.StatusOK)
			return
		} else {
			answerCallbackQuery(callbackID, "✅ 已操作")
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle private messages
	if update.Message != nil && update.Message.Chat.Type == "private" {
		handlePrivateMessage(&update)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle group messages with mentions
	if update.Message != nil {
		// Check for @bot mentions or commands
		if HandleMentionCommand(&update) {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// answerCallbackQuery answers a callback query
func answerCallbackQuery(callbackID string, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", botToken)

	payload := map[string]string{
		"callback_query_id": callbackID,
		"text":             text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// handleMyRequestsPrivate shows user's requests in private chat
func handleMyRequestsPrivate(userID int64) {
	userIDStr := fmt.Sprintf("%d", userID)

	// Get user's requests from analytics
	if analytics == nil {
		sendPrivateMessage(userID, "📋 *我的请求*\n\n⚠️ 分析功能暂不可用", nil)
		return
	}

	analytics.mutex.RLock()
	var userRequests []RequestRecord
	for _, req := range analytics.Requests {
		if req.UserID == userIDStr {
			userRequests = append(userRequests, req)
		}
	}
	analytics.mutex.RUnlock()

	if len(userRequests) == 0 {
		msg := "📋 *我的请求*\n\n"
		msg += "暂无请求记录\n\n"
		msg += "💡 使用 `/search ` 搜索并请求媒体"
		sendPrivateMessage(userID, msg, nil)
		return
	}

	msg := "📋 *我的请求*\n\n"

	// Group by status
	pending := []RequestRecord{}
	approved := []RequestRecord{}
	available := []RequestRecord{}
	declined := []RequestRecord{}

	for _, req := range userRequests {
		switch req.Status {
		case "pending":
			pending = append(pending, req)
		case "approved":
			approved = append(approved, req)
		case "available":
			available = append(available, req)
		case "declined":
			declined = append(declined, req)
		}
	}

	msg += fmt.Sprintf("⏳ 待处理: %d\n", len(pending))
	msg += fmt.Sprintf("✅ 已批准: %d\n", len(approved))
	msg += fmt.Sprintf("🎉 已可用: %d\n", len(available))
	msg += fmt.Sprintf("❌ 已拒绝: %d\n", len(declined))
	msg += fmt.Sprintf("\n📊 总计: %d 个请求\n\n", len(userRequests))

	// Show recent pending requests
	if len(pending) > 0 {
		msg += "*🕐 最近待处理:*\n"
		for i, req := range pending {
			if i >= 3 {
				break
			}
			msg += fmt.Sprintf("• %s\n", req.MediaTitle)
		}
		if len(pending) > 3 {
			msg += fmt.Sprintf("... 还有 %d 个\n", len(pending)-3)
		}
		msg += "\n"
	}

	// Show recent available
	if len(available) > 0 {
		msg += "*🎉 最近可用:*\n"
		for i, req := range available {
			if i >= 3 {
				break
			}
			msg += fmt.Sprintf("• %s", req.MediaTitle)
			if req.AvailableAt != nil {
				msg += fmt.Sprintf(" (%s)", req.AvailableAt.Format("01-02"))
			}
			msg += "\n"
		}
		if len(available) > 3 {
			msg += fmt.Sprintf("... 还有 %d 个\n", len(available)-3)
		}
	}

	sendPrivateMessage(userID, msg, nil)
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body to determine webhook type
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Debug: log raw payload
	log.Printf("[DEBUG] Raw payload: %s", string(body))

	// Try to parse as Jellyseerr webhook first (check for notification_type or subject field)
	var jellyseerrPayload JellyseerrWebhookPayload
	if err := json.Unmarshal(body, &jellyseerrPayload); err == nil {
		// Normalize request_id field (support both formats)
		if jellyseerrPayload.RequestID2 != "" && jellyseerrPayload.RequestID == "" {
			jellyseerrPayload.RequestID = jellyseerrPayload.RequestID2
		}
		// Check if it has Jellyseerr-specific fields (notification_type or subject or request_id)
		if jellyseerrPayload.NotificationType != "" || jellyseerrPayload.Subject != "" || jellyseerrPayload.RequestID != "" {
			log.Printf("Received Jellyseerr webhook: NotificationType=%s, Event=%s, Subject=%s",
				jellyseerrPayload.NotificationType, jellyseerrPayload.Event, jellyseerrPayload.Subject)
			handleJellyseerrWebhook(jellyseerrPayload)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "OK")
			return
		}
	}

	// Try to parse as Emby webhook (must have Event field)
	var embyPayload EmbyWebhookPayload
	if err := json.Unmarshal(body, &embyPayload); err == nil && embyPayload.Event != "" {
		log.Printf("Received Emby webhook: Event=%s, ItemType=%s, ItemName=%s",
			embyPayload.Event, embyPayload.ItemType, embyPayload.ItemName)
		handleEmbyWebhook(embyPayload)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
		return
	}

	log.Printf("Error decoding payload: unknown format")
	http.Error(w, "Bad request", http.StatusBadRequest)
}

func handleEmbyWebhook(payload EmbyWebhookPayload) {
	// Record statistics
	recordStats(payload.Event)

	var message string
	if payload.Event == "item.added" {
		message = formatEmbyNotification(payload)
	} else if payload.Event == "system.notificationtest" {
		message = "🔔 *Emby 测试通知*\n\n✅ Webhook 连接成功！"
	} else {
		message = formatEmbyNotification(payload)
	}

	// Skip sending if message is empty (filtered event)
	if message == "" {
		log.Printf("Event %s filtered, not sending notification", payload.Event)
		return
	}

	if err := sendTelegramMessage(message); err != nil {
		log.Printf("Error sending telegram message: %v", err)
		return
	}
	log.Println("Telegram notification sent successfully")
}

func handleJellyseerrWebhook(payload JellyseerrWebhookPayload) {
	log.Printf("[DEBUG] handleJellyseerrWebhook called, notification_type=%s", payload.NotificationType)

	// Record statistics
	eventType := payload.NotificationType
	if eventType == "" {
		eventType = payload.Event
	}
	recordStats(eventType)

	// Handle issue events with special buttons
	if eventType == "ISSUE_CREATED" || eventType == "issue_created" {
		handleIssueCreatedWebhook(payload)
		return
	}

	message := formatJellyseerrNotification(payload)

	log.Printf("[DEBUG] Formatted Jellyseerr message: %q", message)

	if message == "" {
		log.Printf("[DEBUG] Empty message, not sending")
		return
	}

	if err := sendTelegramMessage(message); err != nil {
		log.Printf("Error sending telegram message: %v", err)
		return
	}
	log.Println("Jellyseerr notification sent successfully")
}

// handleIssueCreatedWebhook handles ISSUE_CREATED webhook with reply buttons
func handleIssueCreatedWebhook(payload JellyseerrWebhookPayload) {
	log.Printf("[DEBUG] handleIssueCreatedWebhook called")

	// Get issue ID - try multiple fields
	issueID := int64(0)
	if payload.Issue != nil && payload.Issue.ID > 0 {
		issueID = int64(payload.Issue.ID)
	}

	// If no issue ID in payload, try to fetch the latest issue
	username := "用户"
	userID := ""

	if issueID == 0 && issueMgr != nil {
		log.Printf("[DEBUG] No issue ID in payload, trying to fetch latest issue")
		// Try to find the latest issue that matches the subject
		if payload.Subject != "" {
			latestIssue, err := issueMgr.FindIssueBySubjectAndTime(payload.Subject, 5)
			if err == nil && latestIssue != nil {
				issueID = int64(latestIssue.ID)
				username = latestIssue.CreatedBy.DisplayName
				userID = fmt.Sprintf("%d", latestIssue.CreatedBy.ID)
				log.Printf("[DEBUG] Found matching issue: ID=%d, User=%s", issueID, username)
			} else {
				log.Printf("[DEBUG] Could not find matching issue: %v", err)
			}
		}
	} else if payload.User != nil && payload.User.ID > 0 {
		username = payload.User.Username
		if username == "" {
			username = payload.User.Email
		}
		userID = fmt.Sprintf("%d", payload.User.ID)
	}

	// Log the full payload for debugging
	log.Printf("[DEBUG] Issue webhook payload: subject=%q, userId=%q, username=%q, issueID=%d",
		payload.Subject, payload.UserID, payload.Username, issueID)

	// Determine issue type
	issueEmoji := "🐛"
	issueType := "问题报告"
	issuePriority := "🟡 普通"

	eventType := payload.Event
	if eventType == "" {
		eventType = payload.NotificationType
	}

	if strings.Contains(eventType, "Subtitle") || strings.Contains(payload.Subject, "字幕") {
		issueEmoji = "💬"
		issueType = "字幕问题"
	} else if strings.Contains(eventType, "Video") {
		issueEmoji = "🎬"
		issueType = "视频问题"
		issuePriority = "🟠 重要"
	} else if strings.Contains(eventType, "Audio") {
		issueEmoji = "🔊"
		issueType = "音频问题"
		issuePriority = "🟠 重要"
	}

	// Get username from payload if not already set
	if username == "用户" || username == "" {
		username = payload.Username
		if strings.Contains(username, "{{") || strings.Contains(username, "}}") {
			// Username is a template variable
			if payload.User != nil && payload.User.Username != "" {
				username = payload.User.Username
			} else if payload.User != nil && payload.User.Email != "" {
				username = payload.User.Email
			} else {
				username = "用户"
			}
		}
		if username == "" && payload.User != nil {
			username = payload.User.Username
			if username == "" && payload.User.Email != "" {
				username = payload.User.Email
			}
		}
	}

	// Get userID from payload if not already set
	if userID == "" {
		userID = payload.UserID
		if strings.Contains(userID, "{{") || strings.Contains(userID, "}}") {
			// UserID is a template variable, try to get from User object
			if payload.User != nil && payload.User.ID > 0 {
				userID = fmt.Sprintf("%d", payload.User.ID)
			} else {
				userID = ""
			}
		}
	}

	// Build message text - use plain text without Markdown to avoid parsing errors
	text := fmt.Sprintf("%s 新%s\n\n", issueEmoji, issueType)
	text += fmt.Sprintf("%s 优先级\n\n", issuePriority)
	if payload.Subject != "" {
		text += fmt.Sprintf("📦 媒体: %s\n", payload.Subject)
	}
	if payload.Message != "" && !strings.Contains(payload.Message, "{{") {
		text += fmt.Sprintf("📝 问题描述: %s\n", payload.Message)
	}

	// Add reporter info
	reporterInfo := ""
	if userID != "" && userSyncMgr != nil {
		jellyseerrID, err := strconv.ParseInt(userID, 10, 64)
		if err == nil {
			telegramID, tgUsername, ok := userSyncMgr.GetTelegramUserInfo(jellyseerrID)
			if ok {
				reporterInfo = fmt.Sprintf("\n👉 %s (@%s) (%d)", username, tgUsername, telegramID)
			}
		}
	}
	if reporterInfo == "" {
		// Don't show template variables
		if !strings.Contains(username, "{{") && username != "" && username != "用户" {
			reporterInfo = fmt.Sprintf("\n👉 %s", username)
		}
	}
	text += reporterInfo

	// Add Jellyseerr URL if no issue ID (so admin can manually check)
	if issueID == 0 {
		text += fmt.Sprintf("\n\n⚠️ 请前往 Jellyseerr 管理面板处理")

		// Send without buttons since we don't have issue ID
		if err := sendTelegramMessage(text); err != nil {
			log.Printf("Error sending issue notification: %v", err)
		}
		log.Printf("Issue notification sent (without buttons - no issue ID)")
		return
	}

	// Create reply keyboard with issue ID
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "💬 回复", "callback_data": fmt.Sprintf("issue_reply:%d", issueID)},
				{"text": "✅ 已修复", "callback_data": fmt.Sprintf("issue_fixed:%d", issueID)},
			},
			{
				{"text": "ℹ️ 处理中", "callback_data": fmt.Sprintf("issue_processing:%d", issueID)},
				{"text": "🔗 详情", "url": fmt.Sprintf("%s/issues/%d", jellyseerrURL, issueID)},
			},
			{
				{"text": "❌ 关闭问题", "callback_data": fmt.Sprintf("issue_close:%d", issueID)},
			},
		},
	}

	// Send to main chat
	if err := sendTelegramMessageWithKeyboard(text, keyboard); err != nil {
		log.Printf("Error sending issue notification: %v", err)
		return
	}

	// Also send private notification to admins with buttons
	notifyAdminsIssue(issueID, text, keyboard)

	log.Printf("Issue %d notification sent with buttons", issueID)
}

// sendTelegramMessageWithKeyboard sends a message with inline keyboard to main chat
func sendTelegramMessageWithKeyboard(text string, keyboard *TelegramInlineKeyboard) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	msg := TelegramMessage{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "",
		ReplyMarkup: keyboard,
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// notifyAdminsIssue sends issue notification to all admins with buttons
func notifyAdminsIssue(issueID int64, text string, keyboard *TelegramInlineKeyboard) {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	if len(admins) == 0 {
		log.Printf("No admins to notify about issue %d", issueID)
		return
	}

	for userIDStr := range admins {
		userIDInt, _ := strconv.ParseInt(userIDStr, 10, 64)
		if err := sendPrivateMessage(userIDInt, text, keyboard); err != nil {
			log.Printf("Error sending issue notification to admin %s: %v", userIDStr, err)
		}
	}
}

// handleIssueReplyCallback handles issue reply button clicks - shows reply options menu
func handleIssueReplyCallback(issueID int64) (responseText string, editMessage bool, newMsg string, newKeyboard *TelegramInlineKeyboard) {
	// Show reply options menu instead of waiting for input
	editMessage = true

	// Create a nice menu with reply options
	newMsg = fmt.Sprintf("💬 *回复问题* `#%d`\n\n请选择回复方式:", issueID)

	// Create keyboard with reply options
	newKeyboard = &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			// Row 1: Quick templates
			{
				{"text": "✅ 已修复", "callback_data": fmt.Sprintf("issue_template:fixed:%d", issueID)},
				{"text": "🔧 处理中", "callback_data": fmt.Sprintf("issue_template:processing:%d", issueID)},
			},
			// Row 2: More templates
			{
				{"text": "🔄 重试一下", "callback_data": fmt.Sprintf("issue_template:retry:%d", issueID)},
				{"text": "ℹ️ 需要更多信息", "callback_data": fmt.Sprintf("issue_template:info:%d", issueID)},
			},
			// Row 3: More templates
			{
				{"text": "❌ 无法重现", "callback_data": fmt.Sprintf("issue_template:unreproducible:%d", issueID)},
				{"text": "🚫 按预期工作", "callback_data": fmt.Sprintf("issue_template:wontfix:%d", issueID)},
			},
			// Row 4: Custom and close options
			{
				{"text": "✏️ 自定义回复", "callback_data": fmt.Sprintf("issue_custom:%d", issueID)},
				{"text": "✏️ 回复并关闭", "callback_data": fmt.Sprintf("issue_custom_close:%d", issueID)},
			},
			// Row 5: Cancel
			{
				{"text": "❌ 取消", "callback_data": fmt.Sprintf("issue_cancel:%d", issueID)},
			},
		},
	}

	responseText = "💬 选择回复方式"
	return
}

// Reply templates for quick responses
var replyTemplates = map[string]string{
	"fixed":           "✅ 问题已修复，请再试一下。如果还有问题请留言。",
	"processing":      "🔧 管理员已收到，正在处理中，请耐心等待。",
	"retry":           "🔄 请尝试刷新或重新播放，如果问题依然存在请提供更多细节。",
	"info":            "ℹ️ 需要更多信息来定位问题，请提供：\n1. 出问题的具体剧集/电影名称\n2. 大约发生时间\n3. 错误提示（如有）\n4. 播放设备信息",
	"unreproducible":  "❌ 无法重现问题，请提供更详细的复现步骤。",
	"wontfix":         "🚫 这是正常行为，不是问题。如有其他疑问请说明。",
}

// handleIssueTemplateCallback handles template reply selection
func handleIssueTemplateCallback(templateType string, issueID int64) (responseText string, editMessage bool, newMsg string, newKeyboard *TelegramInlineKeyboard) {
	editMessage = true

	// Check if issue manager is initialized
	if issueMgr == nil {
		newMsg = "❌ 问题管理器未初始化"
		responseText = "❌ 失败"
		newKeyboard = nil
		return
	}

	// Get template message
	template, exists := replyTemplates[templateType]
	if !exists {
		template = "✅ 已处理"
	}

	// Add comment to Jellyseerr
	if err := issueMgr.AddComment(issueID, template); err != nil {
		log.Printf("Error adding template comment to issue %d: %v", issueID, err)
		newMsg = fmt.Sprintf("❌ 添加评论失败: %v", err)
		responseText = "❌ 失败"
		newKeyboard = nil
		return
	}

	// Format confirmation message
	var statusIcon string
	switch templateType {
	case "fixed":
		statusIcon = "✅"
	case "processing":
		statusIcon = "🔧"
	case "retry":
		statusIcon = "🔄"
	case "info":
		statusIcon = "ℹ️"
	case "unreproducible":
		statusIcon = "❓"
	case "wontfix":
		statusIcon = "🚫"
	default:
		statusIcon = "✅"
	}

	newMsg = fmt.Sprintf("%s *已发送回复*\n\nIssue #%d\n\n`%s`\n\n_问题仍然保持打开状态，直到用户确认或关闭。_", statusIcon, issueID, template)
	newKeyboard = nil // Remove buttons after sending
	responseText = "✅ 回复已发送"
	return
}

// handleIssueCustomReplyCallback handles custom reply request - enters text input mode
func handleIssueCustomReplyCallback(issueID int64, closeAfter bool) (responseText string, editMessage bool, newMsg string, newKeyboard *TelegramInlineKeyboard) {
	editMessage = true

	// Store the pending reply state with close flag
	if pendingIssueReplies == nil {
		pendingIssueReplies = make(map[int64]int64)
	}
	// Use negative issue ID to indicate "close after reply" mode
	if closeAfter {
		pendingIssueReplies[-issueID] = -issueID
	} else {
		pendingIssueReplies[issueID] = issueID
	}

	newMsg = fmt.Sprintf("✏️ *自定义回复* `#%d`\n\n%s\n\n👉 请到*私聊*发送回复内容\n（直接给机器人发消息）", issueID,
		map[bool]string{true: "回复后将自动关闭问题", false: "回复后问题保持打开"}[closeAfter])
	newKeyboard = &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{{"text": "❌ 取消", "callback_data": fmt.Sprintf("issue_cancel:%d", issueID)}},
		},
	}
	responseText = "💬 请输入回复内容"
	return
}

// handleIssueCancelCallback handles cancel reply operation
func handleIssueCancelCallback(issueID int64) (responseText string, editMessage bool, newMsg string, newKeyboard *TelegramInlineKeyboard) {
	editMessage = true

	// Clear pending reply state
	if pendingIssueReplies != nil {
		delete(pendingIssueReplies, issueID)
		delete(pendingIssueReplies, -issueID)
	}

	// Restore original issue buttons
	newMsg = "❌ *已取消回复*\n\n请重新选择操作"
	newKeyboard = &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "💬 回复", "callback_data": fmt.Sprintf("issue_reply:%d", issueID)},
				{"text": "✅ 已修复", "callback_data": fmt.Sprintf("issue_fix:%d", issueID)},
			},
			{
				{"text": "ℹ️ 处理中", "callback_data": fmt.Sprintf("issue_processing:%d", issueID)},
				{"text": "🔗 详情", "url": fmt.Sprintf("https://embyrequest.oceancloud.asia/issues/%d", issueID)},
			},
			{
				{"text": "❌ 关闭问题", "callback_data": fmt.Sprintf("issue_close:%d", issueID)},
			},
		},
	}
	responseText = "❌ 已取消"
	return
}

// handleIssueFixedCallback handles "fixed" quick reply
func handleIssueFixedCallback(issueID int64) string {
	if issueMgr == nil {
		return "❌ 问题管理器未初始化"
	}

	message := "✅ 问题已修复，请再试一下。如果还有问题请留言。"
	if err := issueMgr.AddComment(issueID, message); err != nil {
		log.Printf("Error adding comment to issue %d: %v", issueID, err)
		return fmt.Sprintf("❌ 添加评论失败: %v", err)
	}

	return "✅ 已回复: 问题已修复"
}

// handleIssueProcessingCallback handles "processing" quick reply
func handleIssueProcessingCallback(issueID int64) string {
	if issueMgr == nil {
		return "❌ 问题管理器未初始化"
	}

	message := "ℹ️ 管理员已看到，正在处理中，请耐心等待。"
	if err := issueMgr.AddComment(issueID, message); err != nil {
		log.Printf("Error adding comment to issue %d: %v", issueID, err)
		return fmt.Sprintf("❌ 添加评论失败: %v", err)
	}

	return "✅ 已回复: 正在处理中"
}

// handleIssueCloseCallback handles issue close/delete
func handleIssueCloseCallback(issueID int64) string {
	if issueMgr == nil {
		return "❌ 问题管理器未初始化"
	}

	if err := issueMgr.DeleteIssue(issueID); err != nil {
		log.Printf("Error deleting issue %d: %v", issueID, err)
		return fmt.Sprintf("❌ 删除问题失败: %v", err)
	}

	return "✅ 问题已关闭"
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Emby Telegram Bot is running")
}

// getAdminMentions returns formatted admin mentions
func getAdminMentions(urgentOnly bool) string {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	if len(admins) == 0 {
		return ""
	}

	var mentions []string
	for userID, name := range admins {
		mentions = append(mentions, fmt.Sprintf("[%s](tg://user?id=%s)", name, userID))
	}

	if urgentOnly {
		return fmt.Sprintf("\n\n👉 %s", strings.Join(mentions, " "))
	}
	return fmt.Sprintf("\n\n📢 通知: %s", strings.Join(mentions, " "))
}

// recordStats records notification statistics
func recordStats(eventType string) {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	today := time.Now().Format("2006-01-02")
	if stats.Date != today {
		// New day, send summary and reset
		sendDailySummary()
		stats = Statistics{
			Date:           today,
			LastUpdateTime: time.Now(),
		}
	}

	switch eventType {
	case "REQUEST_CREATED", "request_created", "MEDIA_PENDING":
		stats.RequestCount++
	case "ISSUE_CREATED", "issue_created":
		stats.IssueCount++
	case "REQUEST_APPROVED", "request_approved", "MEDIA_APPROVED":
		stats.ApprovedCount++
	case "REQUEST_DECLINED", "request_declined", "MEDIA_DECLINED":
		stats.DeclinedCount++
	case "REQUEST_AVAILABLE", "request_available", "MEDIA_AVAILABLE":
		stats.AvailableCount++
	case "item.added":
		stats.MediaAdded++
	}

	stats.LastUpdateTime = time.Now()
}

// sendDailySummary sends daily statistics summary
func sendDailySummary() {
	today := time.Now().Format("2006-01-02")

	// Get today's stats from analytics system (which has persistent data)
	analytics.mutex.RLock()
	var dailyStats *DailyCount
	if ds, exists := analytics.DailyStats[today]; exists {
		dailyStats = ds
	} else {
		// No stats for today, create empty one
		dailyStats = &DailyCount{Date: today}
	}

	// Also count pending requests for today
	pendingCount := 0
	approvedCount := dailyStats.ApprovedCount
	declinedCount := dailyStats.DeclinedCount
	availableCount := dailyStats.AvailableCount

	// Count today's requests by status
	for _, req := range analytics.Requests {
		if req.CreatedAt.Format("2006-01-02") == today {
			if req.Status == "pending" {
				pendingCount++
			}
		}
	}
	analytics.mutex.RUnlock()

	// Get current stats for issue count and media added (these are tracked separately)
	statsMutex.Lock()
	issueCount := stats.IssueCount
	mediaAdded := stats.MediaAdded
	statsMutex.Unlock()

	totalRequests := pendingCount + approvedCount + declinedCount

	// Beautiful card-style format optimized for mobile
	text := "┌──────────────────┐\n"
	text += "│  📊 每日数据看板 │\n"
	text += "└──────────────────┘\n\n"

	// Date in a compact style
	weekday := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[time.Now().Weekday()]
	text += fmt.Sprintf("📅 %s · %s\n\n", today, weekday)

	// Request stats in a clean layout
	text += "┌─ 求片统计 ─────────┐\n"
	text += fmt.Sprintf("│ 总计    │%12d │\n", totalRequests)
	text += "├─────────────────────┤\n"
	text += fmt.Sprintf("│ ⏳ 待处理 │%11d │\n", pendingCount)
	text += fmt.Sprintf("│ ✅ 已批准 │%11d │\n", approvedCount)
	text += fmt.Sprintf("│ ❌ 已拒绝 │%11d │\n", declinedCount)
	text += fmt.Sprintf("│ 🎉 已可用 │%11d │\n", availableCount)
	text += "└─────────────────────┘\n"

	// Other stats in horizontal cards
	text += "┌─ 今日概览 ─────────┐\n"
	text += fmt.Sprintf("│ 🐛 问题报告 │%9d │\n", issueCount)
	text += fmt.Sprintf("│ 📀 新增媒体 │%9d │\n", mediaAdded)
	text += "└─────────────────────┘"

	if err := sendTelegramMessage(text); err != nil {
		log.Printf("Error sending daily summary: %v", err)
	}
}

// startDailySummary runs the daily summary scheduler
func startDailySummary() {
	// Send summary at 23:59 every day
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}

		duration := next.Sub(now)
		log.Printf("Next daily summary at %s (in %v)", next.Format("2006-01-02 15:04:05"), duration)

		time.Sleep(duration)
		sendDailySummary()
	}
}

// statsHandler returns current statistics
func statsHandler(w http.ResponseWriter, r *http.Request) {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// adminHandler adds/removes admins
func adminHandler(w http.ResponseWriter, r *http.Request) {
	// Simple admin management via API
	// GET /api/admins - list admins
	// POST /api/admins - add admin (userID, name)
	// DELETE /api/admins/:userID - remove admin

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		adminsMutex.RLock()
		defer adminsMutex.RUnlock()
		json.NewEncoder(w).Encode(admins)

	case "POST":
		// Check if requester is admin (via X-Admin-User-ID header)
		requesterUserID := r.Header.Get("X-Admin-User-ID")
		if requesterUserID == "" {
			// Also check query parameter for backward compatibility
			requesterUserID = r.URL.Query().Get("admin_user_id")
		}

		adminsMutex.RLock()
		_, isRequesterAdmin := admins[requesterUserID]
		hasExistingAdmins := len(admins) > 0
		adminsMutex.RUnlock()

		// Allow adding first admin if no admins exist
		if !hasExistingAdmins {
			// First admin registration is allowed
		} else if !isRequesterAdmin {
			http.Error(w, "Forbidden: Only existing admins can add new admins", http.StatusForbidden)
			return
		}

		var admin struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&admin); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		if admin.UserID == "" {
			http.Error(w, "user_id is required", http.StatusBadRequest)
			return
		}

		adminsMutex.Lock()
		admins[admin.UserID] = admin.Name
		adminsMutex.Unlock()

		log.Printf("Admin added: %s (%s) by %s", admin.Name, admin.UserID, requesterUserID)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Admin added successfully"})

	case "DELETE":
		// Check if requester is admin (via X-Admin-User-ID header)
		requesterUserID := r.Header.Get("X-Admin-User-ID")
		if requesterUserID == "" {
			// Also check query parameter for backward compatibility
			requesterUserID = r.URL.Query().Get("admin_user_id")
		}

		adminsMutex.RLock()
		_, isRequesterAdmin := admins[requesterUserID]
		adminsMutex.RUnlock()

		if !isRequesterAdmin {
			http.Error(w, "Forbidden: Only admins can remove other admins", http.StatusForbidden)
			return
		}

		targetUserID := r.URL.Path[len("/api/admins/"):]
		if targetUserID == "" {
			http.Error(w, "User ID required", http.StatusBadRequest)
			return
		}

		// Prevent self-deletion via API
		if targetUserID == requesterUserID {
			http.Error(w, "Cannot remove yourself via API. Use /unregister command instead", http.StatusBadRequest)
			return
		}

		adminsMutex.Lock()
		if _, exists := admins[targetUserID]; exists {
			delete(admins, targetUserID)
			adminsMutex.Unlock()
			log.Printf("Admin removed: %s by %s", targetUserID, requesterUserID)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Admin removed successfully"})
		} else {
			adminsMutex.Unlock()
			http.Error(w, "Admin not found", http.StatusNotFound)
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// summaryHandler sends manual summary
func summaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		sendDailySummary()
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Summary sent")
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func main() {
	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/telegram-webhook", telegramWebhookHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/api/stats", statsHandler)
	http.HandleFunc("/api/admins", adminHandler)
	http.HandleFunc("/api/admins/", adminHandler)
	http.HandleFunc("/api/summary", summaryHandler)

	addr := ":" + serverPort
	log.Printf("Starting Emby Telegram Bot server on %s", addr)
	log.Printf("Webhook URL: http://your-server-ip:%s/webhook", serverPort)
	log.Printf("Telegram Webhook URL: https://unchromatic-nonparasitically-antoinette.ngrok-free.dev/telegram-webhook")

	// Set Telegram webhook
	if err := setTelegramWebhook(); err != nil {
		log.Printf("Warning: Failed to set Telegram webhook: %v", err)
		log.Println("You can manually set it with:")
		log.Printf("curl https://api.telegram.org/bot%s/setWebhook?url=https://unchromatic-nonparasitically-antoinette.ngrok-free.dev/telegram-webhook", botToken)
	}

	// Set menu button
	if err := setTelegramMenuButton(); err != nil {
		log.Printf("Warning: Failed to set menu button: %v", err)
	}

	// Set commands
	if err := setTelegramCommands(); err != nil {
		log.Printf("Warning: Failed to set commands: %v", err)
	}

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// setTelegramWebhook sets the webhook for Telegram
func setTelegramWebhook() error {
	webhookURL := fmt.Sprintf("https://unchromatic-nonparasitically-antoinette.ngrok-free.dev/telegram-webhook")
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", botToken)

	payload := map[string]string{
		"url": webhookURL,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set webhook: %d", resp.StatusCode)
	}

	log.Printf("Telegram webhook set to: %s", webhookURL)
	return nil
}

// setTelegramMenuButton sets the menu button for the bot
func setTelegramMenuButton() error {
	type MenuButton struct {
		Type string `json:"type"`
	}

	type MenuButtonRequest struct {
		MenuButton MenuButton `json:"menu_button"`
	}

	payload := MenuButtonRequest{
		MenuButton: MenuButton{
			Type: "commands",
		},
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/setChatMenuButton", botToken)
	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set menu button: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	log.Println("Telegram menu button set to: commands")
	return nil
}

// BotCommand represents a bot command
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// setTelegramCommands sets the command list for the bot
func setTelegramCommands() error {
	commands := []BotCommand{
		// ========== 基础功能 ==========
		{Command: "start", Description: "👋 开始使用"},
		{Command: "help", Description: "❓ 帮助"},

		// ========== 搜索与请求 ==========
		{Command: "search", Description: "🔍 搜索媒体"},
		{Command: "request", Description: "📋 发起请求"},
		{Command: "my", Description: "📋 我的请求"},
		{Command: "status", Description: "📊 我的状态"},

		// ========== 设置 ==========
		{Command: "prefs", Description: "⚙️ 通知设置"},
		{Command: "link", Description: "🔗 绑定账号"},

		// ========== 统计 ==========
		{Command: "top", Description: "🔥 热门排行"},
		{Command: "activity", Description: "👥 活跃用户"},

		// ========== 管理员 ==========
		{Command: "pending", Description: "⏳ 待处理"},
		{Command: "approve", Description: "✅ 批准"},
		{Command: "decline", Description: "❌ 拒绝"},
		{Command: "addadmin", Description: "➕ 添加管理员"},
		{Command: "deladmin", Description: "➖ 删除管理员"},
		{Command: "users", Description: "👥 用户列表"},
	}

	type CommandsRequest struct {
		Commands []BotCommand `json:"commands"`
	}

	payload := CommandsRequest{Commands: commands}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", botToken)
	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set commands: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	log.Printf("Telegram commands set: %d commands", len(commands))
	return nil
}

// editMessageText edits a message's text and keyboard
func editMessageText(userID int64, chatID int64, messageID int64, text string, keyboard *TelegramInlineKeyboard) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", botToken)

	msg := TelegramMessage{
		ChatID:      fmt.Sprintf("%d", chatID),
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	}

	// Add message_id for editing
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Add message_id to the payload
	var payload map[string]interface{}
	json.Unmarshal(jsonData, &payload)
	payload["message_id"] = messageID

	jsonData, _ = json.Marshal(payload)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Telegram API error editing message: %s", string(bodyBytes))
		return fmt.Errorf("telegram API error: %d", resp.StatusCode)
	}

	return nil
}

// saveAdminsToFile 保存管理员数据到文件
func saveAdminsToFile() error {
	adminsMutex.RLock()
	defer adminsMutex.RUnlock()

	data, err := json.MarshalIndent(admins, "", "  ")
	if err != nil {
		log.Printf("保存管理员数据失败: %v", err)
		return err
	}

	err = os.WriteFile(adminsFile, data, 0644)
	if err != nil {
		log.Printf("写入管理员文件失败: %v", err)
		return err
	}

	log.Printf("管理员数据已保存，共 %d 位管理员", len(admins))
	return nil
}

// loadAdminsFromFile 从文件加载管理员数据
func loadAdminsFromFile() {
	data, err := os.ReadFile(adminsFile)
	if err != nil {
		log.Println("没有找到管理员数据文件，将创建新的")
		return
	}

	err = json.Unmarshal(data, &admins)
	if err != nil {
		log.Printf("加载管理员数据失败: %v", err)
		return
	}

	log.Printf("已加载 %d 位管理员", len(admins))
}

// handleLinkCommand handles the /link command for user account linking
func handleLinkCommand(telegramID int64, username string, text string) {
	if userSyncMgr == nil {
		sendPrivateMessage(telegramID, "❌ 链接功能暂不可用", nil)
		return
	}

	parts := strings.Fields(text)

	// Check if already linked
	if jellyseerrID, exists := userSyncMgr.GetJellyseerrUserID(telegramID); exists {
		msg := "⚠️ 你已经绑定过账号了\n\n"
		msg += "当前绑定的账号: "
		if profile, err := userSyncMgr.GetUserProfile(jellyseerrID); err == nil {
			displayName := profile.DisplayName
			if displayName == "" {
				displayName = profile.Username
			}
			msg += displayName
		}
		msg += "\n\n如需更换，请先使用 /unlink 解绑"
		sendPrivateMessage(telegramID, msg, nil)
		return
	}

	// Check for existing pending request
	if pendingReq := userSyncMgr.GetBindingRequestByTelegramID(telegramID); pendingReq != nil {
		msg := "⏳ *绑定请求审核中*\n\n"
		msg += fmt.Sprintf("你已提交绑定请求: *%s*\n\n", pendingReq.JellyseerrName)
		msg += "请求正在等待管理员审核\n\n"
		msg += fmt.Sprintf("📝 请求ID: `%s`\n", pendingReq.RequestID)
		msg += "⏰ 请求有效期 24 小时\n\n"
		msg += "💡 使用 /link 取消此请求并重新绑定"
		sendPrivateMessage(telegramID, msg, nil)
		return
	}

	// If no argument, show instructions
	if len(parts) < 2 {
		msg := "🔗 *绑定 Jellyseerr 账号*\n\n"
		msg += "请使用你的 Jellyfin 账号密码绑定:\n\n"
		msg += "用法: `/link 账号 密码`\n\n"
		msg += "示例:\n"
		msg += "`/link xia xia123`\n"
		msg += "`/link myname mypass456`\n\n"
		msg += "💡 这是绑定账号最安全的方式，不会误绑他人账号"
		sendPrivateMessage(telegramID, msg, nil)
		return
	}

	// Try password authentication (username + password)
	if len(parts) >= 3 {
		username := parts[1]
		password := parts[2]

		msg := "🔐 *验证账号中*\n\n"
		msg += "正在验证你的 Jellyfin 账号..."
		sendPrivateMessage(telegramID, msg, nil)

		jellyseerrID, displayName, err := userSyncMgr.VerifyJellyfinCredentials(username, password)
		log.Printf("Link command: telegramID=%d, username=%s, jellyseerrID=%d, displayName=%s, err=%v", telegramID, username, jellyseerrID, displayName, err)

		if err != nil {
			// Check if it's invalid credentials
			if strings.Contains(err.Error(), "INVALID_CREDENTIALS") || strings.Contains(err.Error(), "authentication failed") {
				msg := "❌ *验证失败*\n\n"
				msg += "账号或密码错误，请检查:\n"
				msg += "• 用户名是否正确\n"
				msg += "• 密码是否正确\n"
				msg += "• 账号是否已在 Jellyseerr 注册\n\n"
				msg += "💡 提示: 使用的是 Jellyfin 账号密码"
				sendPrivateMessage(telegramID, msg, nil)
				return
			}
			msg := "❌ 验证失败: " + err.Error()
			sendPrivateMessage(telegramID, msg, nil)
			return
		}

		// Success! Create binding request
		log.Printf("Creating binding request: telegramID=%d, jellyseerrID=%d", telegramID, jellyseerrID)
		if bindingReq := userSyncMgr.CreateBindingRequest(
			telegramID,
			username,
			jellyseerrID,
			displayName,
			username,
		); bindingReq != nil {
			log.Printf("Binding request created: %+v", bindingReq)
			notifyAdminsOfBindingRequest(bindingReq)

			msg := "✅ *验证成功*\n\n"
			msg += fmt.Sprintf("你的账号: *%s*\n\n", displayName)
			msg += "📋 绑定请求已发送给管理员审核\n\n"
			msg += "⏰ 请求有效期 24 小时\n\n"
			msg += "💡 管理员审核通过后，你将收到通知"
			sendPrivateMessage(telegramID, msg, nil)
			return
		}
	}

	// If only one argument (username only), show error
	msg := "❓ *请提供账号和密码*\n\n"
	msg += "用法: `/link 账号 密码`\n\n"
	msg += "示例: `/link xia xia123`"
	sendPrivateMessage(telegramID, msg, nil)
}

// linkUserAccount links a Telegram user to a Jellyseerr user
func linkUserAccount(telegramID int64, user *JellyseerrUserProfile) {
	if err := userSyncMgr.SetUserMapping(telegramID, int64(user.ID)); err != nil {
		sendPrivateMessage(telegramID, "❌ 绑定失败: "+err.Error(), nil)
		return
	}

	// Update smart search manager's mapping too
	if smartSearchMgr != nil {
		smartSearchMgr.SetUserMapping(telegramID, int64(user.ID))
	}

	displayName := user.DisplayName
	if displayName == "" {
		displayName = user.Username
	}

	msg := "✅ *账号绑定成功*\n\n"
	msg += fmt.Sprintf("👤 %s\n", displayName)
	if user.Email != "" {
		msg += fmt.Sprintf("📧 %s\n", user.Email)
	}
	msg += "\n现在你可以直接使用求片功能了！\n\n"
	msg += "💡 使用 /search 搜索媒体"

	sendPrivateMessage(telegramID, msg, nil)
}

// handleVerifyCommand handles the /verify command for generating verification codes
func handleVerifyCommand(telegramID int64, username string) {
	if userSyncMgr == nil {
		sendPrivateMessage(telegramID, "❌ 验证功能暂不可用", nil)
		return
	}

	// Check if already linked
	if _, exists := userSyncMgr.GetJellyseerrUserID(telegramID); exists {
		sendPrivateMessage(telegramID, "⚠️ 你已经链接过账号了。如需更换，请先使用 /unlink 解绑。", nil)
		return
	}

	// Generate verification code
	code := userSyncMgr.GenerateVerificationCode(telegramID, username)

	msg := "🔐 *账号验证码*\n\n"
	msg += fmt.Sprintf("你的验证码是: *`%s`*\n\n", code)
	msg += "请按以下步骤完成绑定:\n\n"
	msg += "1️⃣ 访问 Jellyseerr 网站\n"
	msg += fmt.Sprintf("👉 %s\n\n", jellyseerrURL)
	msg += "2️⃣ 登录你的账号\n"
	msg += "3️⃣ 进入 \"设置\" -> \"账号\" -> \"Telegram 绑定\"\n"
	msg += "4️⃣ 输入验证码完成绑定\n\n"
	msg += "⏰ 验证码有效期 10 分钟"

	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "🌐 打开 Jellyseerr", "url": jellyseerrURL},
			},
		},
	}

	sendPrivateMessage(telegramID, msg, keyboard)
}

// handleUnlinkCommand handles the /unlink command
func handleUnlinkCommand(telegramID int64) {
	if userSyncMgr == nil {
		sendPrivateMessage(telegramID, "❌ 解绑功能暂不可用", nil)
		return
	}

	// Check if linked
	jellyseerrID, exists := userSyncMgr.GetJellyseerrUserID(telegramID)
	if !exists {
		sendPrivateMessage(telegramID, "⚠️ 你还没有链接任何账号", nil)
		return
	}

	// Remove mapping
	userSyncMgr.RemoveUserMapping(telegramID)

	// Also update smart search manager
	if smartSearchMgr != nil {
		mappingMutex := &smartSearchMgr.mappingMutex
		mappingMutex.Lock()
		delete(smartSearchMgr.userMapping, telegramID)
		mappingMutex.Unlock()
		smartSearchMgr.saveUserMapping()
	}

	msg := "✅ *账号已解绑*\n\n"
	msg += fmt.Sprintf("Jellyseerr ID: %d\n\n", jellyseerrID)
	msg += "你可以随时使用 /link 重新绑定"
	sendPrivateMessage(telegramID, msg, nil)
}

// handleQuickLinkCommand handles the /quicklink command
func handleQuickLinkCommand(telegramID int64, text string) {
	if userSyncMgr == nil {
		sendPrivateMessage(telegramID, "❌ 快速绑定功能暂不可用", nil)
		return
	}

	parts := strings.Fields(text)
	if len(parts) < 3 {
		msg := "🚀 *快速绑定*\n\n"
		msg += "用法: /quicklink <账号名> <密码>\n\n"
		msg += "示例: /quicklink myuser mypassword123\n\n"
		msg += "💡 密码即验证码，请确保密码正确"
		sendPrivateMessage(telegramID, msg, nil)
		return
	}

	username := parts[1]
	password := strings.Join(parts[2:], " ")

	// Verify credentials with Jellyseerr
	jellyseerrID, displayName, err := userSyncMgr.VerifyJellyfinCredentials(username, password)
	if err != nil {
		msg := "❌ *验证失败*\n\n"
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			msg += "账号名或密码错误\n\n"
			msg += "💡 请检查你的 Jellyfin 账号信息"
		} else {
			msg += err.Error() + "\n\n"
			msg += "💡 请稍后再试或联系管理员"
		}
		sendPrivateMessage(telegramID, msg, nil)
		return
	}

	// Create binding request
	bindingReq := userSyncMgr.CreateBindingRequest(telegramID, "", jellyseerrID, displayName, username)

	// Auto-approve quick link requests
	err = userSyncMgr.ApproveBindingRequest(bindingReq.RequestID, telegramID)
	if err != nil {
		msg := "❌ *绑定失败*\n\n" + err.Error()
		sendPrivateMessage(telegramID, msg, nil)
		return
	}

	msg := "🎉 *快速绑定成功*\n\n"
	msg += fmt.Sprintf("账号: %s\n", displayName)
	msg += "\n现在你可以使用求片功能了！\n\n"
	msg += "💡 使用 /search 搜索媒体"
	sendPrivateMessage(telegramID, msg, nil)
}
