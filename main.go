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
	"unicode"

	"emby-telegram-bot/ai"
	"emby-telegram-bot/bot"
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
		Id              string `json:"Id"`
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
		Text            string `json:"text"`
		ReplyToMessage  *struct {
			MessageID int64 `json:"message_id"`
			From      struct {
				ID        int64  `json:"id"`
				IsBot     bool   `json:"is_bot"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"from"`
		} `json:"reply_to_message"`
	} `json:"message"`
	CallbackQuery *TelegramCallbackQuery `json:"callback_query"`
}

var (
	botToken    string
	chatID      string
	serverPort  string

	// HTTP client with timeout for all requests
	httpClient *http.Client

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

// Global bot module instance
var botModule *bot.BotModule

// Global AI trending manager
var trendingAIManager *ai.TrendingAIManager

// Media security checker
var mediaSecurityChecker *MediaSecurityChecker

// Chat system for group conversations
var chatSystem *bot.ChatSystem
var knowledgeBase *bot.KnowledgeBase

// InitBotModule initializes the new modular bot system
func InitBotModule() error {
	botModule = bot.NewBotModule()
	return botModule.Init(botToken, chatID, jellyseerrURL, jellyseerrAPIKey)
}

// InitChatSystem initializes the chat system and knowledge base
func InitChatSystem() {
	// Initialize knowledge base
	knowledgeBase = bot.NewKnowledgeBase(".")
	log.Println("[ChatSystem] Knowledge base initialized")

	// Initialize chat system
	chatSystem = bot.NewChatSystem(knowledgeBase)

	// Set admin checker and Jellyseerr config
	if chatSystem != nil {
		chatSystem.SetAdminChecker(isUserAdmin)
		chatSystem.SetJellyseerrConfig(jellyseerrURL, jellyseerrAPIKey)
		log.Println("[ChatSystem] Chat system initialized with admin checker and Jellyseerr config")
	}
}

// convertToBotUpdate converts main.TelegramUpdate to bot.TelegramUpdate
func convertToBotUpdate(update *TelegramUpdate) *bot.TelegramUpdate {
	if update == nil {
		return nil
	}

	botUpdate := &bot.TelegramUpdate{
		UpdateID: update.UpdateID,
	}

	if update.Message != nil {
		botUpdate.Message = &struct {
			MessageID int64  `json:"message_id"`
			From      struct {
				ID        int64  `json:"id"`
				IsBot     bool   `json:"is_bot"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
				Username  string `json:"username"`
			} `json:"from"`
			Chat struct {
				ID   int64 `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			Date            int64 `json:"date"`
			Text            string `json:"text"`
			ReplyToMessage  *struct {
				MessageID int64 `json:"message_id"`
				From      struct {
					ID        int64  `json:"id"`
					IsBot     bool   `json:"is_bot"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
				} `json:"from"`
			} `json:"reply_to_message"`
		}{
			MessageID: update.Message.MessageID,
			From: struct {
				ID        int64  `json:"id"`
				IsBot     bool   `json:"is_bot"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
				Username  string `json:"username"`
			}{
				ID:        update.Message.From.ID,
				IsBot:     false,
				FirstName: update.Message.From.FirstName,
				LastName:  update.Message.From.LastName,
				Username:  update.Message.From.Username,
			},
			Chat: struct {
				ID   int64 `json:"id"`
				Type string `json:"type"`
			}{
				ID:   update.Message.Chat.ID,
				Type: update.Message.Chat.Type,
			},
			Text: update.Message.Text,
		}

		// 复制 ReplyToMessage 字段
		if update.Message.ReplyToMessage != nil {
			botUpdate.Message.ReplyToMessage = &struct {
				MessageID int64 `json:"message_id"`
				From      struct {
					ID        int64  `json:"id"`
					IsBot     bool   `json:"is_bot"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
				} `json:"from"`
			}{
				MessageID: update.Message.ReplyToMessage.MessageID,
				From: struct {
					ID        int64  `json:"id"`
					IsBot     bool   `json:"is_bot"`
					FirstName string `json:"first_name"`
					Username  string `json:"username"`
				}{
					ID:        update.Message.ReplyToMessage.From.ID,
					IsBot:     update.Message.ReplyToMessage.From.IsBot,
					FirstName: update.Message.ReplyToMessage.From.FirstName,
					Username:  update.Message.ReplyToMessage.From.Username,
				},
			}
		}
	}

	if update.CallbackQuery != nil {
		botUpdate.CallbackQuery = &struct {
			ID      string `json:"id"`
			From    struct {
				ID        int64  `json:"id"`
				IsBot     bool   `json:"is_bot"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"from"`
			Message struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
			Data string `json:"callback_data"`
		}{
			ID: update.CallbackQuery.ID,
			From: struct {
				ID        int64  `json:"id"`
				IsBot     bool   `json:"is_bot"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			}{
				ID:        update.CallbackQuery.From.ID,
				IsBot:     false,
				FirstName: update.CallbackQuery.From.FirstName,
				Username:  update.CallbackQuery.From.Username,
			},
			Message: struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			}{
				MessageID: update.CallbackQuery.Message.MessageID,
				Chat:      struct{ ID int64 `json:"id"` }{ID: update.CallbackQuery.Message.Chat.ID},
			},
			Data: update.CallbackQuery.Data,
		}
	}

	return botUpdate
}

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

	// Initialize AI trending manager (2026-02-20)
	InitAITrending()

	// Initialize API security system (2026-02-20)
	InitAPISecurity()
	log.Println("[Init] API Security system initialized")

	// Start daily summary routine
	go startDailySummary()
}

// InitAITrending initializes the AI trending manager
func InitAITrending() {
	log.Println("[Init] InitAITrending called")
	// Get ZHIPU_API_KEY from environment
	apiKey := os.Getenv("ZHIPU_API_KEY")
	log.Printf("[Init] ZHIPU_API_KEY length: %d", len(apiKey))
	if apiKey == "" {
		log.Println("[Init] ZHIPU_API_KEY not set, AI trending disabled")
		return
	}

	// Import ai package and create trending manager
	zhipuClient := ai.NewZhipuClient(apiKey)
	log.Printf("[Init] ZhipuClient enabled: %v", zhipuClient.IsEnabled())
	if zhipuClient.IsEnabled() {
		trendingAIManager = ai.NewTrendingAIManager(zhipuClient)
		log.Println("[Init] AI trending manager initialized")
	} else {
		log.Println("[Init] Zhipu AI client not enabled, AI trending disabled")
	}
}

func formatEmbyNotificationWithPhoto(payload EmbyWebhookPayload) (string, string) {
	// For library.new, extract info from nested Item structure
	itemType := payload.ItemType
	itemName := payload.ItemName
	seasonName := payload.SeasonName
	year := payload.Year
	genres := payload.Genres
	itemID := payload.ItemID

	// If library.new with nested Item, extract from there
	if payload.Event == "library.new" && payload.Item != nil {
		itemType = payload.Item.Type
		itemName = payload.Item.Name
		seasonName = payload.Item.SeasonName
		if payload.Item.Id != "" {
			itemID = payload.Item.Id
		}
		if payload.Item.ProductionYear > 0 {
			year = &payload.Item.ProductionYear
		}
		genres = payload.Item.Genres
	}

	var text strings.Builder
	var photoURL string

	// Header separator
	text.WriteString("✅ 入库成功")

	switch payload.Event {
	case "item.updated":
		// Ignore item.updated as it's too frequent
		return "", ""
	case "library.new", "item.added":
		if itemType == "Season" || (payload.Item != nil && payload.Item.Type == "Season") {
			// Season format like: 盐水大饭店 (2024) S01 E01-E08
			movYear := 0
			if year != nil {
				movYear = *year
			}

			// Get detailed info from Emby API with retry
			var childCount int
			var quality string
			var totalSize int64
			var fileCount int
			var seriesID string

			for attempt := 0; attempt <= 2; attempt++ {
				if info, err := GetEmbyItemInfo(itemID); err == nil {
					childCount = info.ChildCount
					quality = GetMediaQuality(info)
					totalSize = GetTotalSize(info)
					fileCount = GetFileCount(info)
					seriesID = info.SeriesId
					// Get backdrop image URL (horizontal, perfect for mobile)
					photoURL = GetBestImageURL(info)
					// If no backdrop, try to fetch from series
					if photoURL == "" && info.SeriesId != "" {
						photoURL = FetchSeriesBackdrop(info.SeriesId)
					}

					// If we got meaningful data, break
					if (quality != "" && quality != "未知") || totalSize > 0 {
						break
					}
					// Wait before retry
					if attempt < 2 {
						time.Sleep(500 * time.Millisecond)
					}
				} else {
					if attempt < 2 {
						time.Sleep(500 * time.Millisecond)
					}
				}
			}

			// If quality is still unknown and childCount > 0, try to get from first episode
			if quality == "未知" || quality == "" {
				quality = getQualityFromFirstEpisode(itemID)
			}
			// If ChildCount is 0, try to get from items query
			if childCount == 0 && seriesID != "" {
				embyURL := os.Getenv("EMBY_URL")
				apiKey := os.Getenv("EMBY_API_KEY")
				userID := os.Getenv("EMBY_USER_ID")
				if embyURL != "" && apiKey != "" {
					childURL := fmt.Sprintf("%s/Users/%s/Items?ParentId=%s&Limit=1", embyURL, userID, itemID)
					req, err := http.NewRequest("GET", childURL, nil)
					if err == nil {
						req.Header.Set("X-Emby-Token", apiKey)
						resp, err := httpClient.Do(req)
						if err == nil && resp.StatusCode == 200 {
							defer resp.Body.Close()
							var result struct {
								TotalRecordCount int `json:"TotalRecordCount"`
							}
							body, _ := io.ReadAll(resp.Body)
							if json.Unmarshal(body, &result) == nil {
								childCount = result.TotalRecordCount
							}
						}
					}
				}
			}

			// Build season info line
			text.WriteString(fmt.Sprintf("：%s", itemName))
			if movYear > 0 {
				text.WriteString(fmt.Sprintf(" (%d)", movYear))
			}

			// Add season number and episode range
			seasonNum := ""
			if seasonName != "" {
				if strings.Contains(seasonName, "Season") {
					parts := strings.Split(seasonName, " ")
					if len(parts) > 1 {
						seasonNum = fmt.Sprintf("S%02d", parseSeasonNumber(parts[len(parts)-1]))
					}
				}
			}

			if seasonNum != "" {
				text.WriteString(fmt.Sprintf(" %s", seasonNum))
			}

			if childCount > 0 {
				text.WriteString(fmt.Sprintf(" E01-E%02d", childCount))
			} else {
				text.WriteString(" E01")
			}

			// Separator line
			text.WriteString("\n")
			text.WriteString("───────────────────\n\n")

			// Format details
			text.WriteString(fmt.Sprintf("🎬 名称：%s", itemName))
			if movYear > 0 {
				text.WriteString(fmt.Sprintf(" (%d)", movYear))
			}
			if seasonNum != "" {
				text.WriteString(fmt.Sprintf(" %s", seasonNum))
			}
			if childCount > 0 {
				text.WriteString(fmt.Sprintf(" E01-E%02d", childCount))
			}
			text.WriteString("\n\n")

			// Category - determine from genres
			category := "剧集"
			if len(genres) > 0 {
				// Check for specific categories
				genreLower := strings.ToLower(strings.Join(genres, " "))
				switch {
				case strings.Contains(genreLower, "chinese") || strings.Contains(genreLower, "国语") || strings.Contains(genreLower, "国产"):
					category = "国产剧"
				case strings.Contains(genreLower, "korean") || strings.Contains(genreLower, "韩剧"):
					category = "韩剧"
				case strings.Contains(genreLower, "japanese") || strings.Contains(genreLower, "日剧"):
					category = "日剧"
				case strings.Contains(genreLower, "american") || strings.Contains(genreLower, "美剧"):
					category = "美剧"
				}
			}
			text.WriteString(fmt.Sprintf("🏷️ 类别：%s\n\n", category))

			// Quality
			if quality != "" {
				text.WriteString(fmt.Sprintf("💎 质量：%s\n\n", quality))
			} else {
				text.WriteString("💎 质量：未知\n\n")
			}

			// Size
			if totalSize > 0 {
				text.WriteString(fmt.Sprintf("📦 总大小：%s\n\n", FormatMediaSize(totalSize)))
			}

			// File count
			if fileCount > 0 {
				text.WriteString(fmt.Sprintf("📁 文件数量：%d 个\n", fileCount))
			}

			// Add episode file details (with size and quality for each episode)
			if episodes, err := GetSeasonEpisodesInfo(itemID); err == nil && len(episodes) > 0 {
				text.WriteString(FormatEpisodesFileList(episodes))
			}

		} else if itemType == "Episode" || (payload.Item != nil && payload.Item.Type == "Episode") {
			// Episode added - 动态通知：每次有新剧集都通知
			// Get series ID from API
			seriesID := ""
			if itemID != "" {
				if info, err := GetEmbyItemInfo(itemID); err == nil {
					seriesID = info.SeriesId
				}
			}

			// Get current episode count in the series
			currentCount := 0
			if seriesID != "" {
				embyURL := os.Getenv("EMBY_URL")
				apiKey := os.Getenv("EMBY_API_KEY")
				userID := os.Getenv("EMBY_USER_ID")
				if embyURL != "" && apiKey != "" {
					seriesURL := fmt.Sprintf("%s/Users/%s/Items?ParentId=%s&Fields=ChildCount", embyURL, userID, seriesID)
					req, _ := http.NewRequest("GET", seriesURL, nil)
					req.Header.Set("X-Emby-Token", apiKey)
					resp, err := httpClient.Do(req)
					if err == nil && resp.StatusCode == 200 {
						defer resp.Body.Close()
						var seasonInfo struct {
							ChildCount int `json:"ChildCount"`
						}
						body, _ := io.ReadAll(resp.Body)
						if json.Unmarshal(body, &seasonInfo) == nil {
							currentCount = seasonInfo.ChildCount
						}
					}
				}
			}

			// Build notification for new episode
			seriesName := payload.ParentName // Series name is in ParentName for episodes
			if seriesName == "" && payload.Item != nil {
				seriesName = payload.Item.SeriesName
			}

			// Get season number
			seasonNum := "S01"
			if seasonName != "" && strings.Contains(seasonName, "Season") {
				parts := strings.Split(seasonName, " ")
				if len(parts) > 1 {
					seasonNum = fmt.Sprintf("S%02d", parseSeasonNumber(parts[len(parts)-1]))
				}
			}

			// Get episode index
			epIndex := 0
			if payload.IndexNumber != nil && *payload.IndexNumber > 0 {
				epIndex = *payload.IndexNumber
			} else if payload.Item != nil && payload.Item.IndexNumber > 0 {
				epIndex = payload.Item.IndexNumber
			}

			notifyMilestone := fmt.Sprintf("新增第%d集", epIndex)
			text.WriteString(fmt.Sprintf("✅ %s %s %sE%02d\n\n",
				notifyMilestone, seriesName, seasonNum, epIndex))
			text.WriteString("───────────────────\n\n")
			text.WriteString(fmt.Sprintf("📊 当前进度：共%d集\n\n", currentCount))

			// Try to get quality from the latest episode
			if info, err := GetEmbyItemInfo(itemID); err == nil {
				quality := GetMediaQuality(info)
				if quality != "" && quality != "未知" {
					text.WriteString(fmt.Sprintf("💎 质量：%s\n\n", quality))
				}
			}

			text.WriteString("📺 前往观看：https://emby.oceancloud.asia")

		} else if itemType == "Movie" || (payload.Item != nil && payload.Item.Type == "Movie") {
			// Movie format
			movYear := 0
			if year != nil {
				movYear = *year
			}

			// Get detailed info from Emby API with retry for better data
			var quality string
			var totalSize int64
			var fileCount int

			// Try multiple times to get complete media info (webhook might fire before media is fully scanned)
			for attempt := 0; attempt <= 2; attempt++ {
				if info, err := GetEmbyItemInfo(itemID); err == nil {
					quality = GetMediaQuality(info)
					totalSize = GetTotalSize(info)
					fileCount = GetFileCount(info)
					// Get backdrop image URL (horizontal, perfect for mobile)
					photoURL = GetBestImageURL(info)

					// If we got meaningful data, break
					if quality != "" && quality != "未知" && totalSize > 0 {
						break
					}
					// Wait before retry (only if not last attempt)
					if attempt < 2 {
						time.Sleep(500 * time.Millisecond)
					}
				} else {
					// Wait before retry if API call failed
					if attempt < 2 {
						time.Sleep(500 * time.Millisecond)
					}
				}
			}

			// Get rating info (includes Chinese name and genres)
			mediaRating := getMediaRating(itemName, movYear, "movie")

			// Use Chinese name if available, otherwise use original
			displayTitle := itemName
			if mediaRating.CNName != "" && mediaRating.CNName != itemName {
				displayTitle = mediaRating.CNName
			}

			// Build title line
			text.WriteString(fmt.Sprintf("：%s", displayTitle))
			if movYear > 0 {
				text.WriteString(fmt.Sprintf(" (%d)", movYear))
			}
			text.WriteString("\n")
			text.WriteString("───────────────────\n\n")

			// Name line
			text.WriteString(fmt.Sprintf("🎬 名称：%s", displayTitle))
			if movYear > 0 {
				text.WriteString(fmt.Sprintf(" (%d)", movYear))
			}
			text.WriteString("\n\n")

			// Category - use genres
			category := "电影"
			if mediaRating.GenreCN != "" {
				category = mediaRating.GenreCN
			} else if len(genres) > 0 {
				category = genres[0]
			}
			text.WriteString(fmt.Sprintf("🏷️ 类别：%s\n\n", category))

			// Quality
			if quality != "" {
				text.WriteString(fmt.Sprintf("💎 质量：%s\n\n", quality))
			} else {
				text.WriteString("💎 质量：未知\n\n")
			}

			// Size
			if totalSize > 0 {
				text.WriteString(fmt.Sprintf("📦 总大小：%s\n\n", FormatMediaSize(totalSize)))
			}

			// File count
			if fileCount > 0 {
				text.WriteString(fmt.Sprintf("📁 文件数量：%d 个", fileCount))
			}

		} else {
			text.WriteString("\n\n")
			text.WriteString(fmt.Sprintf("🎬 名称：%s\n\n", itemName))
			text.WriteString(fmt.Sprintf("🏷️ 类别：%s", itemType))
		}

	case "system.notificationtest":
		return "🔔 Emby 测试通知\n\n✅ Webhook 连接成功！", ""

	default:
		text.WriteString("\n\n")
		if payload.ItemName != "" {
			text.WriteString(fmt.Sprintf("🎬 名称：%s\n", payload.ItemName))
		}
		if payload.ItemType != "" {
			text.WriteString(fmt.Sprintf("🏷️ 类型：%s\n", payload.ItemType))
		}
	}

	return text.String(), photoURL
}

// getQualityFromFirstEpisode gets quality info from the first episode of a season
func getQualityFromFirstEpisode(seasonID string) string {
	embyURL := os.Getenv("EMBY_URL")
	apiKey := os.Getenv("EMBY_API_KEY")
	userID := os.Getenv("EMBY_USER_ID")

	if embyURL == "" || apiKey == "" || seasonID == "" {
		return ""
	}

	// Get first episode
	epURL := fmt.Sprintf("%s/Users/%s/Items?ParentId=%s&Limit=1&Fields=MediaSources,MediaStreams", embyURL, userID, seasonID)

	req, err := http.NewRequest("GET", epURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result struct {
		Items []struct {
			MediaSources []struct {
				Path         string `json:"Path"`
				Size         int64  `json:"Size"`
				MediaStreams []struct {
					Type   string `json:"Type"`
					Width  int    `json:"Width"`
					Height int    `json:"Height"`
					Codec  string `json:"Codec"`
				} `json:"MediaStreams"`
			} `json:"MediaSources"`
		} `json:"Items"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return ""
	}

	if len(result.Items) == 0 || len(result.Items[0].MediaSources) == 0 {
		return ""
	}

	// Extract quality from first episode
	source := result.Items[0].MediaSources[0]
	width := 0
	height := 0

	for _, stream := range source.MediaStreams {
		if stream.Type == "Video" {
			if stream.Width > 0 {
				width = stream.Width
			}
			if stream.Height > 0 {
				height = stream.Height
			}
			break
		}
	}

	if height == 0 {
		return ""
	}

	// Determine quality based on height and width
	var quality string
	switch {
	case width >= 3800 || height >= 2160:
		quality = "4K"
	case height >= 1080:
		quality = "1080p"
	case height >= 720:
		quality = "720p"
	case height >= 480:
		quality = "480p"
	default:
		quality = fmt.Sprintf("%dp", height)
	}

	// Check source type from path
	path := strings.ToLower(source.Path)
	sourceType := "WEB-DL"
	if strings.Contains(path, "bluray") || strings.Contains(path, "bdrip") || strings.Contains(path, "brrip") {
		sourceType = "BluRay"
	} else if strings.Contains(path, "webrip") {
		sourceType = "WEBRip"
	} else if strings.Contains(path, "hdtv") {
		sourceType = "HDTV"
	} else if strings.Contains(path, "dvd") {
		sourceType = "DVD"
	}

	return fmt.Sprintf("%s %s", sourceType, quality)
}

// parseSeasonNumber extracts season number from string
func parseSeasonNumber(s string) int {
	re := regexp.MustCompile(`\d+`)
	if matches := re.FindStringSubmatch(s); len(matches) > 0 {
		if num, err := strconv.Atoi(matches[0]); err == nil {
			return num
		}
	}
	return 1
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

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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

// sendTelegramPhoto sends a photo to Telegram
// Photo is sent as URL (Telegram will fetch it)
func sendTelegramPhoto(photoURL, caption string) error {
	if photoURL == "" {
		return fmt.Errorf("empty photo URL")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", botToken)

	// Build photo message payload
	payload := map[string]interface{}{
		"chat_id": chatID,
		"photo":   photoURL,
	}

	// Add caption if provided (max 1024 chars)
	if caption != "" {
		if len(caption) > 1024 {
			caption = caption[:1020] + "..."
		}
		payload["caption"] = caption
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal photo message: %w", err)
	}

	fmt.Printf("[DEBUG] Sending photo to Telegram: %s\n", photoURL)

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("[DEBUG] Photo request error: %v\n", err)
		return fmt.Errorf("send photo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("[DEBUG] Photo API error: status=%d, response=%s\n", resp.StatusCode, string(body))
		return fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(body))
	}

	fmt.Println("[DEBUG] Photo sent successfully")
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

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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

	// Extract command if text contains space (for arguments)
	command := text
	if idx := strings.Index(text, " "); idx > 0 {
		command = text[:idx]
	}

	// ========== 聊天系统优先：非命令消息先用聊天系统处理 ==========
	// 只有明确的搜索请求才跳过聊天系统
	if !strings.HasPrefix(command, "/") && !isExplicitSearchQuery(text) {
		log.Printf("[CHAT] Using chat system for message: %s", text)
		if chatSystem != nil {
			displayName := update.Message.From.FirstName
			if update.Message.From.Username != "" {
				displayName = update.Message.From.Username
			}

			response := chatSystem.GetChatResponse(text, displayName, update.Message.From.ID)
			if response != "" {
				log.Printf("[CHAT] Chat system response: %s", response)
				sendPrivateMessage(update.Message.From.ID, response, nil)
				return
			}
			log.Printf("[CHAT] Chat system returned empty response")
		}
		// 聊天系统没有响应，继续下面的处理
	}
	// ============================================================

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
			if err := addIssueComment(actualIssueID, text); err != nil {
				sendPrivateMessage(update.Message.From.ID, fmt.Sprintf("❌ 回复失败: %v", err), nil)
			} else {
				log.Printf("Admin %d replied to issue %d: %s", update.Message.From.ID, actualIssueID, text)

				// Close issue if requested
				if shouldClose {
					if err := deleteIssue(actualIssueID); err != nil {
						log.Printf("Error closing issue %d: %v", actualIssueID, err)
						sendPrivateMessage(update.Message.From.ID, "✅ 回复已发送，但关闭问题失败", nil)
					} else {
						sendPrivateMessage(update.Message.From.ID, "✅ 回复已发送，问题已关闭", nil)
					}
				} else {
					sendPrivateMessage(update.Message.From.ID, "✅ 回复已发送", nil)
				}
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
	case "/start":
		// /start command - Welcome message with keyboard buttons
		log.Printf("[COMMAND] /start from user %d (%s)", update.Message.From.ID, username)
		isNewUser := ShouldShowOnboarding(update.Message.From.ID)

		var startMsg string
		var keyboard *TelegramInlineKeyboard

		if isNewUser {
			// New user - use GetWelcomeForNewUser which includes keyboard
			startMsg, keyboard = GetWelcomeForNewUser(update.Message.From.ID, username)
		} else {
			// Returning user - greeting with quick actions keyboard
			displayName := update.Message.From.FirstName
			if displayName == "" {
				displayName = username
				if displayName == "" {
					displayName = "朋友"
				}
			}

			startMsg = fmt.Sprintf("👋 *欢迎回来，%s！*\n\n", displayName)
			startMsg += "我可以帮你搜索和请求影视内容\n\n"
			startMsg += "💡 点击下方按钮快速开始"

			// Use QuickStartKeyboard for returning users
			keyboard = GetQuickStartKeyboard()
		}

		sendPrivateMessage(update.Message.From.ID, startMsg, keyboard)

		// Complete onboarding after first interaction
		if isNewUser && onboardingMgr != nil {
			onboardingMgr.CompleteOnboarding(update.Message.From.ID)
		}

	case "/help":
		// /help command - Comprehensive help guide
		log.Printf("[COMMAND] /help from user %d (%s)", update.Message.From.ID, username)
		// Get help message from command_center
		helpMsg := FormatHelpMessage(isAdminUser(update.Message.From.ID))
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

	case "/perf":
		// Performance monitoring command (admin only)
		if update.Message != nil {
			if isAdminUser(update.Message.From.ID) {
				sendPrivateMessage(update.Message.From.ID, GetRuntimeInfo(), nil)
			} else {
				sendPrivateMessage(update.Message.From.ID, "❌ 此命令仅管理员可用", nil)
			}
		}

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
				sendPrivateMessage(update.Message.From.ID, formatAPIError(err, "搜索"), nil)
				log.Printf("Error searching with filter: %v", err)
				return
			}

			msg := FormatSearchResultsWithDetails(results, query)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else if jellyseerrClient != nil {
			// Fallback to basic search
			results, err := jellyseerrClient.SearchMedia(query)
			if err != nil {
				sendPrivateMessage(update.Message.From.ID, formatAPIError(err, "搜索"), nil)
				log.Printf("Error searching media: %v", err)
				return
			}

			msg := FormatSearchResults(results, query)
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, "❌ 搜索功能暂不可用", nil)
		}

	case "/refresh_trending":
		// Refresh AI trending recommendations (admin only)
		userIDStr := fmt.Sprintf("%d", update.Message.From.ID)
		adminsMutex.RLock()
		_, isAdmin := admins[userIDStr]
		adminsMutex.RUnlock()

		if !isAdmin {
			sendPrivateMessage(update.Message.From.ID, "❌ 此命令仅管理员可用", nil)
			return
		}

		if trendingAIManager == nil || !trendingAIManager.IsEnabled() {
			sendPrivateMessage(update.Message.From.ID, "❌ AI 推荐功能未启用", nil)
			return
		}

		// Send loading message
		loadingMsg := "🔄 正在刷新 AI 推荐内容...\n\n这可能需要 15-20 秒"
		sendPrivateMessage(update.Message.From.ID, loadingMsg, nil)

		// Refresh in background
		go func(userID int64) {
			if err := trendingAIManager.RefreshCache(); err != nil {
				sendPrivateMessage(userID, fmt.Sprintf("❌ 刷新失败: %v", err), nil)
			} else {
				sendPrivateMessage(userID, "✅ AI 推荐内容已刷新！\n\n💡 重新点击热门推荐按钮查看新内容", nil)
			}
		}(update.Message.From.ID)

	case "/random", "/推荐":
		// Get random recommendations from AI
		if trendingAIManager == nil || !trendingAIManager.IsEnabled() {
			sendPrivateMessage(update.Message.From.ID, "❌ AI 推荐功能未启用\n\n💡 请联系管理员配置 ZHIPU_API_KEY", nil)
			return
		}

		// Determine media type from command
		mediaType := "movie" // default
		if strings.Contains(text, "剧集") || strings.Contains(text, "tv") {
			mediaType = "tv"
		}

		// Send loading message
		loadingMsg := "🎲 正在获取随机推荐...\n\n这可能需要 15-20 秒"
		sendPrivateMessage(update.Message.From.ID, loadingMsg, nil)

		// Get recommendations in background
		go func(userID int64, mType string) {
			results, err := trendingAIManager.GetRandomRecommendation(8, mType)
			if err != nil {
				sendPrivateMessage(userID, fmt.Sprintf("❌ 获取推荐失败: %v", err), nil)
				return
			}

			// Format results with buttons
			var sb strings.Builder
			title := "🎲 随机推荐"
			if mType == "tv" {
				title = "🎲 随机剧集推荐"
			}
			sb.WriteString(fmt.Sprintf("┌─── %s ────┐\n\n", title))
			sb.WriteString(fmt.Sprintf("  📅 更新时间: 刚刚\n\n"))
			sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

			// Create keyboard
			var keyboard [][]map[string]string

			for i, item := range results {
				if i >= 8 {
					break
				}

				emoji := "🎬"
				if mType == "tv" {
					emoji = "📺"
				}

				sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, item.Title))
				if item.Year > 0 {
					sb.WriteString(fmt.Sprintf(" (%d)", item.Year))
				}
				if item.Rating > 0 {
					sb.WriteString(fmt.Sprintf(" ⭐%.1f", item.Rating))
				}
				sb.WriteString("\n")

				if item.Reason != "" {
					reason := item.Reason
					if len(reason) > 20 {
						reason = reason[:17] + "..."
					}
					sb.WriteString(fmt.Sprintf("     💡 %s\n", reason))
				}

				// Add button
				if i%4 == 0 {
					keyboard = append(keyboard, []map[string]string{})
				}
				buttonLabel := fmt.Sprintf("%d", i+1)
				callbackData := fmt.Sprintf("ai_random_%d_%s", item.TmdbID, mType)
				keyboard[len(keyboard)-1] = append(keyboard[len(keyboard)-1], map[string]string{
					"text":         buttonLabel,
					"callback_data": callbackData,
				})
			}

			sb.WriteString("\n└──────────────────────┘")

			sendPrivateMessage(userID, sb.String(), &TelegramInlineKeyboard{InlineKeyboard: keyboard})
		}(update.Message.From.ID, mediaType)

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
			sendPrivateMessage(update.Message.From.ID, formatAPIError(err, "获取待处理请求"), nil)
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
			sendPrivateMessage(update.Message.From.ID, formatAPIError(err, "批准请求"), nil)
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
			sendPrivateMessage(update.Message.From.ID, formatAPIError(err, "拒绝请求"), nil)
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
		// Show user preferences with interactive keyboard
		prefs := prefManager.GetPreferences(userID, username)
		msg, keyboard := FormatPreferencesWithKeyboard(prefs)
		sendPrivateMessage(update.Message.From.ID, msg, keyboard)

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

	case "/ai":
		// AI assistant command
		log.Printf("[COMMAND] /ai from user %d (%s)", update.Message.From.ID, username)
		parts := strings.Fields(text)
		var args string
		if len(parts) > 1 {
			args = strings.Join(parts[1:], " ")
		}
		response, err := ai.HandleAICommand(update.Message.From.ID, args)
		if err != nil {
			msg := "🤖 *AI 助手错误*\n\n"
			if strings.Contains(err.Error(), "not enabled") {
				msg += "AI 功能暂未启用\n\n"
				msg += "💡 请联系管理员配置 ZHIPU_API_KEY 或 CLAUDE_API_KEY"
			} else {
				msg += "抱歉，AI 服务暂时不可用\n\n"
				msg += "💡 请稍后再试"
			}
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			sendPrivateMessage(update.Message.From.ID, response, nil)
		}

	case "/recommend", "/rec", "/suggest":
		// Smart recommendations - integrate with AI
		log.Printf("[COMMAND] /recommend from user %d (%s)", update.Message.From.ID, username)
		parts := strings.Fields(text)
		var mood string
		if len(parts) > 1 {
			mood = strings.Join(parts[1:], " ")
		}

		if mood == "" {
			// Show mood options
			msg := "🎯 *智能推荐*\n\n"
			msg += "请告诉我你的心情或偏好：\n\n"
			msg += "• 开心/放松 - 轻松喜剧\n"
			msg += "• 紧张/刺激 - 悬疑惊悚\n"
			msg += "• 感动/温情 - 爱情剧情\n"
			msg += "• 好奇/探索 - 科幻纪录片\n\n"
			msg += "用法: /recommend 心情"
			sendPrivateMessage(update.Message.From.ID, msg, nil)
		} else {
			// Get AI recommendations
			result, err := ai.GetAIRecommendations(mood, 5)
			if err != nil {
				// Provide user-friendly error message for AI failures
				msg := "🤖 *推荐失败*\n\n"
				if strings.Contains(err.Error(), "AI is not enabled") {
					msg += "AI 功能暂未启用\n\n"
					msg += "💡 请联系管理员配置 ZHIPU_API_KEY 或 CLAUDE_API_KEY"
				} else {
					msg += "抱歉，AI 服务暂时不可用\n\n"
					msg += "💡 请稍后再试或联系管理员"
				}
				sendPrivateMessage(update.Message.From.ID, msg, nil)
			} else {
				sendPrivateMessage(update.Message.From.ID, result, nil)
			}
		}

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
	// 记录 webhook 调用（用于调试）
	go func() {
		f, _ := os.OpenFile("/tmp/webhook-calls.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		defer f.Close()
		f.WriteString(fmt.Sprintf("[%s] Webhook called from %s\n", time.Now().Format("2006-01-02 15:04:05"), r.RemoteAddr))
	}()

	log.Printf("[DEBUG] telegramWebhookHandler ENTRY")
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[PANIC] telegramWebhookHandler recovered: %v", err)
			debug.PrintStack()
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}()

	// 🔒 安全检查：先读取原始 JSON 用于媒体安全检查
	// 必须在 Decode 之前读取，因为 body 只能读一次
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 🔒 安全检查：检测媒体内容中的 Emby 链接泄露（使用原始 JSON）
	if mediaSecurityChecker != nil {
		shouldDelete, reason := mediaSecurityChecker.CheckUpdate(rawBody)
		if shouldDelete {
			log.Printf("[Security] Message deleted due to: %s", reason)
			// 消息已被安全检查器处理（删除或警告已发送）
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "OK")
			return
		}
	}

	// 解析 update 结构
	var update TelegramUpdate
	if err := json.Unmarshal(rawBody, &update); err != nil {
		log.Printf("Error decoding update: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 尝试使用新模块处理消息
	if botModule != nil {
		// 检查是否是新格式的回调 (以 search:subscribe:download:page:cancel 开头)
		if update.CallbackQuery != nil {
			data := update.CallbackQuery.Data
			if isNewFormatCallback(data) {
				log.Printf("[BotModule] Using new module for callback: %s", data)
				botModule.HandleCallback(convertToBotUpdate(&update))
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "OK")
				return
			}
		}

		// 检查是否是新的消息格式 (直接搜索、订阅等)
		// 私聊优先让聊天系统处理，只有明确的搜索关键词才用搜索模块
		if update.Message != nil && update.Message.Text != "" {
			chatType := update.Message.Chat.Type
			text := update.Message.Text

			// 私聊：检查是否是明确的搜索请求
			if chatType == "private" {
				isSearchRequest := isExplicitSearchQuery(text)
				if isSearchRequest {
					log.Printf("[BotModule] Using new module for search (private): %s", text)
					botModule.HandleMessage(convertToBotUpdate(&update))
					w.WriteHeader(http.StatusOK)
					fmt.Fprintf(w, "OK")
					return
				}
				log.Printf("[ROUTE] Private chat message (not search), passing to chat system: %s", text)
			} else {
				// 群组：使用原来的逻辑
				if shouldUseNewModule(text) {
					log.Printf("[BotModule] Using new module for message (group): %s", text)
					botModule.HandleMessage(convertToBotUpdate(&update))
					w.WriteHeader(http.StatusOK)
					fmt.Fprintf(w, "OK")
					return
				}
			}
		}
	}

	// Handle callback queries (button presses) - legacy handler
	if update.CallbackQuery != nil {
		callbackID := update.CallbackQuery.ID
		data := update.CallbackQuery.Data
		userID := update.CallbackQuery.From.ID
		username := update.CallbackQuery.From.FirstName
		var chatID int64
		var messageID int64
		if update.CallbackQuery.Message != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			messageID = update.CallbackQuery.Message.MessageID
		}

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

			// Check for special multi-part actions first (before single-part switch)
			// This handles cases like "search_trending" where we need the full action name
			// Also handles AI result selection like "ai_trending_<tmdbID>_<type>"
			isSpecialAction := data == "search_trending" || data == "search_tv_hot" || data == "search_movie_new"
			isAISelection := strings.HasPrefix(data, "ai_trending_") || strings.HasPrefix(data, "ai_hot_tv_") || strings.HasPrefix(data, "ai_new_movie_") || strings.HasPrefix(data, "ai_random_")

			if isSpecialAction {
				log.Printf("[DEBUG] isSpecialAction=true, data=%s", data)
				switch data {
				case "search_trending":
					newMsg, newKeyboard, editMessage = handleTrendingSearchCallback(userID)
				case "search_tv_hot":
					newMsg, newKeyboard, editMessage = handleHotTVSearchCallback(userID)
				case "search_movie_new":
					newMsg, newKeyboard, editMessage = handleNewMoviesSearchCallback(userID)
				}
				log.Printf("[DEBUG] After special action handler: newMsg=%q, editMessage=%v", newMsg[:50]+"...", editMessage)
			} else if isAISelection {
				// Handle AI result button selection
				// Format: ai_<source>_<tmdbID>_<type>
				parts := strings.Split(data, "_")
				if len(parts) >= 4 {
					tmdbID, _ := strconv.Atoi(parts[2])
					mediaType := parts[3]

					// Build details message with request button
					var sb strings.Builder
					emoji := "🎬"
					if mediaType == "tv" {
						emoji = "📺"
					}
					typeLabel := "电影"
					if mediaType == "tv" {
						typeLabel = "剧集"
					}

					sb.WriteString(fmt.Sprintf("┌─ ✨ 详情 ─────────┐\n\n"))
					sb.WriteString(fmt.Sprintf("%s TMDB ID: %d\n", emoji, tmdbID))
					sb.WriteString(fmt.Sprintf("🏷️ 类型: %s\n\n", typeLabel))
					sb.WriteString("━━━━━━━━━━━━━━━\n\n")
					sb.WriteString("💡 点击下方按钮发起请求\n")

					// Create keyboard with request button
					requestButton := map[string]string{
						"text":         "📋 发起请求",
						"callback_data": fmt.Sprintf("request_%d_%s", tmdbID, mediaType),
					}
					backButton := map[string]string{
						"text":         "🔙 返回列表",
						"callback_data": "ignore", // Just ignore for now
					}
					keyboard := &TelegramInlineKeyboard{
						InlineKeyboard: [][]map[string]string{{requestButton}, {backButton}},
					}

					newMsg = sb.String()
					newKeyboard = keyboard
					editMessage = true
				}
			} else {
				// Normal switch for single-part actions
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

			case "search_trending":
				// Trending search - show popular content with enterprise-grade fallback
				newMsg, newKeyboard, editMessage = handleTrendingSearchCallback(userID)

			case "search_tv_hot":
				// Hot TV shows - search for popular TV series with enterprise-grade fallback
				newMsg, newKeyboard, editMessage = handleHotTVSearchCallback(userID)

			case "search_movie_new":
				// New movies - search for recent movies with enterprise-grade fallback
				newMsg, newKeyboard, editMessage = handleNewMoviesSearchCallback(userID)

			case "search":
				// Quick search from button (only if not the special trending actions)
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
				// Quick action buttons - enterprise-grade handling
				if len(parts) >= 2 {
					subAction := parts[1]
					switch subAction {
					case "search":
						newMsg = `🔍 *搜索内容*

直接输入影片名称即可搜索

💡 *搜索技巧*
• 输入中文名：复仇者联盟
• 输入英文名：Avatar
• 输入年份：2024年电影
• 输入类型：科幻剧

现在就可以开始搜索！`
						editMessage = true
					case "myrequests":
						go handleMyRequestsPrivate(userID)
						newMsg = `📋 *我的请求*

正在获取您的请求列表...

💡 *提示*
使用 /link 绑定账号后可查看详细信息`
						editMessage = true
					case "help":
						newMsg = `❓ *帮助中心*

📱 *常用命令*
/start - 开始使用
/search - 搜索内容
/my - 我的请求
/link - 绑定账号
/help - 显示此帮助

💡 点击左下角菜单快速访问所有功能`
						editMessage = true
					case "settings":
						quotaText := "未绑定账号"
						if smartSearchMgr != nil {
							quotaInfo := smartSearchMgr.GetUserQuotaInfo(userID)
							if quotaInfo != "" {
								quotaText = quotaInfo
							}
						}
						newMsg = fmt.Sprintf(`⚙️ *设置*

📊 *今日配额*
%s

💡 *其他设置*
/prefs - 通知设置
/link - 绑定账号
/quota - 配额详情`, quotaText)
						editMessage = true
					case "random":
						// Random recommendations - trigger like /random command
						if trendingAIManager == nil || !trendingAIManager.IsEnabled() {
							newMsg = "❌ AI 推荐功能未启用\n\n💡 请联系管理员配置 ZHIPU_API_KEY"
							editMessage = true
						} else {
							newMsg = "🎲 正在获取随机推荐...\n\n这可能需要 15-20 秒"
							editMessage = true
							// Get random recommendations in background and edit the message
							go func(uid int64, cid int64, mid int64) {
								results, err := trendingAIManager.GetRandomRecommendation(8, "movie")
								if err != nil {
									errMsg := fmt.Sprintf("❌ 获取推荐失败: %v", err)
									editMessageText(uid, cid, mid, errMsg, nil)
									log.Printf("[Callback] Random recommendation failed: %v", err)
									return
								}

								// Format results
								var sb strings.Builder
								sb.WriteString("┌─── 🎲 随机推荐 ────┐\n\n")
								sb.WriteString("  📅 更新时间: 刚刚\n\n")
								sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

								var keyboard [][]map[string]string

								for i, item := range results {
									if i >= 8 {
										break
									}

									sb.WriteString(fmt.Sprintf("  🎬 %d. %s", i+1, item.Title))
									if item.Year > 0 {
										sb.WriteString(fmt.Sprintf(" (%d)", item.Year))
									}
									if item.Rating > 0 {
										sb.WriteString(fmt.Sprintf(" ⭐%.1f", item.Rating))
									}
									sb.WriteString("\n")

									if item.Reason != "" {
										reason := item.Reason
										if len(reason) > 20 {
											reason = reason[:17] + "..."
										}
										sb.WriteString(fmt.Sprintf("     💡 %s\n", reason))
									}

									if i%4 == 0 {
										keyboard = append(keyboard, []map[string]string{})
									}
									buttonLabel := fmt.Sprintf("%d", i+1)
									callbackData := fmt.Sprintf("ai_random_%d_movie", item.TmdbID)
									keyboard[len(keyboard)-1] = append(keyboard[len(keyboard)-1], map[string]string{
										"text":         buttonLabel,
										"callback_data": callbackData,
									})
								}

								sb.WriteString("\n└──────────────────────┘")

								// Edit the original message with results
								editMessageText(uid, cid, mid, sb.String(), &TelegramInlineKeyboard{InlineKeyboard: keyboard})
								log.Printf("[Callback] Random recommendation results sent to user %d", uid)
							}(userID, chatID, messageID)
						}
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

			case "prefs_toggle_movies", "prefs_toggle_series", "prefs_toggle_issues",
			     "prefs_toggle_approved", "prefs_toggle_available", "prefs_toggle_quiet":
				// Toggle preference settings
				if prefManager != nil {
					userIDStr := fmt.Sprintf("%d", userID)
					prefs := prefManager.GetPreferences(userIDStr, username)
					switch action {
					case "prefs_toggle_movies":
						prefs.NotifyMovies = !prefs.NotifyMovies
					case "prefs_toggle_series":
						prefs.NotifySeries = !prefs.NotifySeries
					case "prefs_toggle_issues":
						prefs.NotifyIssues = !prefs.NotifyIssues
					case "prefs_toggle_approved":
						prefs.NotifyApproved = !prefs.NotifyApproved
					case "prefs_toggle_available":
						prefs.NotifyAvailable = !prefs.NotifyAvailable
					case "prefs_toggle_quiet":
						prefs.QuietHoursEnabled = !prefs.QuietHoursEnabled
					}
					prefManager.SetPreferences(userIDStr, prefs)
					newMsg, newKeyboard = FormatPreferencesWithKeyboard(prefs)
					editMessage = true
					responseText = "✅ 设置已更新"
				} else {
					responseText = "❌ 设置功能暂不可用"
				}

			case "prefs_reset":
				// Reset all preferences to default
				if prefManager != nil {
					userIDStr := fmt.Sprintf("%d", userID)
					prefs := &UserPreferences{
						UserID:            userIDStr,
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
					prefManager.SetPreferences(userIDStr, prefs)
					newMsg, newKeyboard = FormatPreferencesWithKeyboard(prefs)
					editMessage = true
					responseText = "✅ 设置已重置"
				} else {
					responseText = "❌ 设置功能暂不可用"
				}

			case "prefs_whitelist", "prefs_blacklist":
				// Add keyword to whitelist/blacklist
				responseText = "💡 使用 /setprefs 命令添加关键词\n\n"
				if action == "prefs_whitelist" {
					responseText += "示例: /setprefs whitelist 4K"
				} else {
					responseText += "示例: /setprefs blacklist 恐怖"
				}

			case "prefs_help":
				newMsg = "📖 *设置帮助*\n\n"
				newMsg += "• 🎬 电影 - 开关电影通知\n"
				newMsg += "• 📺 剧集 - 开关剧集通知\n"
				newMsg += "• 🐛 问题 - 开关问题报告通知\n"
				newMsg += "• ✅ 批准 - 开关批准通知\n"
				newMsg += "• 🎉 可用 - 开关可用通知\n"
				newMsg += "• 🌙 勿扰 - 开关勿扰模式\n"
				newMsg += "• 🔕 白名单 - 只通知匹配关键词的内容\n"
				newMsg += "• 🚫 黑名单 - 不通知匹配关键词的内容\n\n"
				newMsg += "💡 使用 /setprefs 命令进行详细设置"
				editMessage = true
				responseText = "✅ 已显示帮助"

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
			} // End of else block for normal switch

			// Answer the callback query (for both special and normal actions)
			if responseText != "" {
				answerCallbackQuery(callbackID, responseText)
			} else {
				answerCallbackQuery(callbackID, "")
			}

			// Edit the message if needed (check if Message exists)
			// This is now OUTSIDE the if-else, so it works for both special and normal actions
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

	// Handle group messages with mentions and chat
	if update.Message != nil {
		// Check if this is a reply to bot message
		isReplyToBot := false
		if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.From.IsBot {
			isReplyToBot = true
		}

		// First, check if this is a chat message that needs response
		// Chat system handles: @mentions, replies to bot, learning commands
		if chatSystem != nil && update.Message.Text != "" {
			userID := update.Message.From.ID
			userName := getDisplayName(update.Message.From)
			chatID := update.Message.Chat.ID
			message := update.Message.Text

			// Check if should reply (only replies to @mentions or learning commands)
			response := chatSystem.ProcessChatMessage(&bot.ChatTriggerData{
				Message:  message,
				UserName:  userName,
				UserID:    userID,
				ChatType:  update.Message.Chat.Type,
				IsReplyToBot: isReplyToBot,  // 传递是否回复机器人
			})

			if response.ShouldReply {
				// Send chat response to group
				sendGroupMessage(chatID, response.Reply)
				log.Printf("[ChatSystem] Replied to group message from user %d (isReplyToBot=%v)", userID, isReplyToBot)
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Check for @bot mentions or commands (legacy handler)
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
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// handleMyRequestsPrivate shows user's requests in private chat
func handleMyRequestsPrivate(userID int64) {
	// Get user's Jellyseerr ID
	var jellyseerrUserID int64
	if userSyncMgr != nil {
		jellyseerrUserID, _ = userSyncMgr.GetJellyseerrUserID(userID)
	}

	if jellyseerrUserID == 0 {
		// User not linked, check analytics for fallback
		if analytics == nil {
			sendPrivateMessage(userID, "📋 *我的请求*\n\n⚠️ 请先绑定账号", nil)
			return
		}

		analytics.mutex.RLock()
		var userRequests []RequestRecord
		for _, req := range analytics.Requests {
			if req.UserID == fmt.Sprintf("%d", userID) {
				userRequests = append(userRequests, req)
			}
		}
		analytics.mutex.RUnlock()

		if len(userRequests) == 0 {
			sendPrivateMessage(userID, "📋 *我的请求*\n\n暂无请求记录\n\n💡 请先绑定账号后使用 /search 搜索并请求媒体", nil)
			return
		}

		// Show analytics data (limited info)
		msg := "📋 *我的请求* (本地记录)\n\n"
		for _, req := range userRequests {
			statusIcon := map[string]string{"pending": "⏳", "approved": "✅", "available": "🎉", "declined": "❌"}[req.Status]
			msg += fmt.Sprintf("%s %s\n", statusIcon, req.MediaTitle)
		}
		sendPrivateMessage(userID, msg, nil)
		return
	}

	// User is linked - fetch from Jellyseerr
	if jellyseerrClient == nil {
		sendPrivateMessage(userID, "📋 *我的请求*\n\n⚠️ Jellyseerr API 未配置", nil)
		return
	}

	// Fetch user requests from Jellyseerr
	requests, err := jellyseerrClient.GetUserRequests(int(jellyseerrUserID))
	if err != nil {
		sendPrivateMessage(userID, "📋 *我的请求*\n\n❌ 获取失败: "+formatAPIError(err, "获取请求"), nil)
		log.Printf("Error getting user requests: %v", err)
		return
	}

	if len(requests) == 0 {
		msg := "📋 *我的请求*\n\n"
		msg += "暂无请求记录\n\n"
		msg += "💡 使用 /search 搜索并请求媒体"
		sendPrivateMessage(userID, msg, nil)
		return
	}

	// Format requests by status
	msg := "📋 *我的请求*\n\n"

	// Group by status
	pending := []JellyseerrRequest{}
	approved := []JellyseerrRequest{}
	available := []JellyseerrRequest{}
	declined := []JellyseerrRequest{}

	for _, req := range requests {
		status := req.getStatus()
		switch status {
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
	msg += fmt.Sprintf("\n📊 总计: %d 个请求\n\n", len(requests))

	// Show pending requests with buttons
	if len(pending) > 0 {
		msg += "*⏳ 待处理:*\n"
		for i, req := range pending {
			if i >= 5 {
				msg += fmt.Sprintf("... 还有 %d 个\n", len(pending)-5)
				break
			}
			mediaTitle := req.Media.Title
			if mediaTitle == "" {
				mediaTitle = fmt.Sprintf("ID: %d", req.MediaID)
			}
			msg += fmt.Sprintf("• %s\n", mediaTitle)
		}
		msg += "\n"
	}

	// Show recent available
	if len(available) > 0 {
		msg += "*🎉 最近可用:*\n"
		for i, req := range available {
			if i >= 5 {
				break
			}
			mediaTitle := req.Media.Title
			if mediaTitle == "" {
				mediaTitle = fmt.Sprintf("ID: %d", req.MediaID)
			}
			msg += fmt.Sprintf("• %s", mediaTitle)
		}
		msg += "\n"
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
	var photoURL string

	if payload.Event == "item.added" || payload.Event == "library.new" {
		message, photoURL = formatEmbyNotificationWithPhoto(payload)
	} else if payload.Event == "system.notificationtest" {
		message = "🔔 Emby 测试通知\n\n✅ Webhook 连接成功！"
	} else {
		message, _ = formatEmbyNotificationWithPhoto(payload)
	}

	// Skip sending if message is empty (filtered event)
	if message == "" {
		log.Printf("Event %s filtered, not sending notification", payload.Event)
		return
	}

	// Send photo if available, otherwise send text message
	if photoURL != "" {
		// Send photo with caption (combined in one message)
		if err := sendTelegramPhoto(photoURL, message); err != nil {
			log.Printf("Error sending telegram photo: %v", err)
			// Fallback to text only
			if err := sendTelegramMessage(message); err != nil {
				log.Printf("Error sending telegram message: %v", err)
				return
			}
		}
		log.Println("Telegram notification with photo sent successfully")
	} else {
		if err := sendTelegramMessage(message); err != nil {
			log.Printf("Error sending telegram message: %v", err)
			return
		}
		log.Println("Telegram notification sent successfully")
	}
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
	// Get issue ID
	issueID := int64(0)
	if payload.Issue != nil && payload.Issue.ID > 0 {
		issueID = int64(payload.Issue.ID)
	}

	// Get username
	username := payload.Username
	if username == "" && payload.User != nil {
		username = payload.User.Username
		if username == "" {
			username = payload.User.Email
		}
	}
	if strings.Contains(username, "{{") {
		username = "用户"
	}

	// Determine issue type
	emoji := "🐛"
	eventType := payload.Event
	if eventType == "" {
		eventType = payload.NotificationType
	}

	if strings.Contains(eventType, "Subtitle") {
		emoji = "💬"
	} else if strings.Contains(eventType, "Video") {
		emoji = "🎬"
	} else if strings.Contains(eventType, "Audio") {
		emoji = "🔊"
	}

	// Build message
	text := fmt.Sprintf("%s 新问题报告\n", emoji)
	if payload.Subject != "" {
		text += fmt.Sprintf("📦 %s\n", payload.Subject)
	}
	if payload.Message != "" && !strings.Contains(payload.Message, "{{") {
		text += fmt.Sprintf("\n%s", payload.Message)
	}
	if username != "" && username != "用户" {
		text += fmt.Sprintf("\n\n👤 %s", username)
	}

	// No issue ID - send simple message
	if issueID == 0 {
		text += "\n\n⚠️ 请前往 Jellyseerr 管理"
		sendTelegramMessage(text)
		return
	}

	// Create keyboard
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "💬 回复", "callback_data": fmt.Sprintf("issue_reply:%d", issueID)},
				{"text": "✅ 已修复", "callback_data": fmt.Sprintf("issue_fixed:%d", issueID)},
			},
			{
				{"text": "🔗 详情", "url": fmt.Sprintf("%s/issues/%d", jellyseerrURL, issueID)},
				{"text": "❌ 关闭", "callback_data": fmt.Sprintf("issue_close:%d", issueID)},
			},
		},
	}

	notifyAdminsIssue(issueID, text, keyboard)
	sendTelegramMessageWithKeyboard(text, keyboard)
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

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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

	log.Printf("[DEBUG] notifyAdminsIssue called: issueID=%d, adminsCount=%d", issueID, len(admins))

	if len(admins) == 0 {
		log.Printf("No admins to notify about issue %d", issueID)
		return
	}

	successCount := 0
	for userIDStr := range admins {
		userIDInt, _ := strconv.ParseInt(userIDStr, 10, 64)
		log.Printf("[DEBUG] Sending issue notification to admin %s (ID: %d)", userIDStr, userIDInt)
		if err := sendPrivateMessage(userIDInt, text, keyboard); err != nil {
			log.Printf("Error sending issue notification to admin %s: %v", userIDStr, err)
		} else {
			successCount++
			log.Printf("[DEBUG] Successfully sent issue notification to admin %s", userIDStr)
		}
	}
	log.Printf("Issue %d: notified %d/%d admins", issueID, successCount, len(admins))
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

	// Get template message
	template, exists := replyTemplates[templateType]
	if !exists {
		template = "✅ 已处理"
	}

	// Add comment to Jellyseerr
	if err := addIssueComment(issueID, template); err != nil {
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
	newKeyboard = nil
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
	message := "✅ 问题已修复，请再试一下。如果还有问题请留言。"
	if err := addIssueComment(issueID, message); err != nil {
		log.Printf("Error adding comment to issue %d: %v", issueID, err)
		return fmt.Sprintf("❌ 添加评论失败: %v", err)
	}
	return "✅ 已回复: 问题已修复"
}

// handleIssueProcessingCallback handles "processing" quick reply
func handleIssueProcessingCallback(issueID int64) string {
	message := "ℹ️ 管理员已看到，正在处理中，请耐心等待。"
	if err := addIssueComment(issueID, message); err != nil {
		log.Printf("Error adding comment to issue %d: %v", issueID, err)
		return fmt.Sprintf("❌ 添加评论失败: %v", err)
	}
	return "✅ 已回复: 正在处理中"
}

// handleIssueCloseCallback handles issue close/delete
func handleIssueCloseCallback(issueID int64) string {
	if err := deleteIssue(issueID); err != nil {
		log.Printf("Error deleting issue %d: %v", issueID, err)
		return fmt.Sprintf("❌ 删除问题失败: %v", err)
	}
	return "✅ 问题已关闭"
}

// addIssueComment adds a comment to an issue via Jellyseerr API
func addIssueComment(issueID int64, message string) error {
	url := fmt.Sprintf("%s/api/v1/issue/%d/comment", jellyseerrURL, issueID)
	payload := map[string]string{"message": message}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", jellyseerrAPIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return nil
}

// deleteIssue deletes an issue via Jellyseerr API
func deleteIssue(issueID int64) error {
	url := fmt.Sprintf("%s/api/v1/issue/%d", jellyseerrURL, issueID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Api-Key", jellyseerrAPIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return nil
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

// ==============================================================================
// Enterprise-grade Callback Handlers
// ==============================================================================

// handleTrendingSearchCallback handles trending search with comprehensive fallback
func handleTrendingSearchCallback(userID int64) (string, *TelegramInlineKeyboard, bool) {
	log.Printf("[Callback] handleTrendingSearchCallback for user %d", userID)

	// Check if AI is enabled and cache is fresh
	if trendingAIManager != nil && trendingAIManager.IsEnabled() {
		cacheKey := "trending_movies"

		// Check if we have fresh cached results
		cached := trendingAIManager.GetCachedResults(cacheKey)
		if cached != nil && len(cached) > 0 {
			// Fresh cache - display immediately
			msg, keyboard := buildTrendingResultsMessageWithKeyboard(cached, "🔥 热门推荐 (AI精选)", cacheKey)
			return msg, keyboard, true
		}

		// Cache is stale or empty - need to fetch from AI
		// Send loading message first and tell user to wait
		loadingMsg := "🔄 正在获取 AI 推荐内容...\n\n这可能需要 15-20 秒\n\n💡 请稍候..."
		log.Printf("[Callback] Returning loading message for trending movies")

		// Fetch in background and send update
		go func(uid int64) {
			log.Printf("[Callback] Background goroutine started for trending movies")
			results, err := trendingAIManager.GetTrendingMovies(8)
			if err == nil && len(results) > 0 {
				log.Printf("[Callback] Got %d trending results, sending to user", len(results))
				msg, keyboard := buildTrendingResultsMessageWithKeyboard(results, "🔥 热门推荐 (AI精选)", "trending_movies")
				sendPrivateMessage(uid, msg, keyboard)
			} else {
				log.Printf("[Callback] AI trending failed: %v", err)
				sendPrivateMessage(uid, "❌ 获取推荐失败，请稍后再试", nil)
			}
		}(userID)

		// Return loading message immediately
		return loadingMsg, nil, true
	}

	// Fallback 1: Try smartSearchMgr
	if smartSearchMgr != nil {
		ctx := &SearchContext{
			UserID: userID,
			Query:  "2024",
			Params: &SearchParams{},
		}
		if err := smartSearchMgr.Search(ctx); err != nil {
			log.Printf("[Callback] Trending search via smartSearchMgr failed: %v", err)
		} else {
			msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
			return "🔥 *热门推荐*\n\n" + msg, keyboard, true
		}
	}

	// Fallback 2: Try direct Jellyseerr search
	if jellyseerrClient != nil {
		results, err := jellyseerrClient.SearchMedia("2024")
		if err == nil && len(results) > 0 {
			msg := formatSimpleSearchResults("🔥 热门推荐", "2024", results)
			return msg, nil, true
		}
	}

	// Final fallback: User-friendly message with suggestions
	msg := `🔥 *热门推荐*

AI 推荐服务正在初始化中

💡 *你可以试试*
1. 直接输入「复仇者联盟」搜索
2. 直接输入「权力的游戏」搜索
3. 直接输入「2024」搜索今年内容

💡 或者直接输入任何内容名开始搜索`

	return msg, nil, true
}

// handleHotTVSearchCallback handles hot TV search with comprehensive fallback
func handleHotTVSearchCallback(userID int64) (string, *TelegramInlineKeyboard, bool) {
	log.Printf("[Callback] handleHotTVSearchCallback for user %d", userID)

	// Check if AI is enabled and cache is fresh
	if trendingAIManager != nil && trendingAIManager.IsEnabled() {
		cacheKey := "hot_tv_shows"

		// Check if we have fresh cached results
		cached := trendingAIManager.GetCachedResults(cacheKey)
		if cached != nil && len(cached) > 0 {
			// Fresh cache - display immediately
			msg, keyboard := buildTrendingResultsMessageWithKeyboard(cached, "📺 热播剧集 (AI精选)", cacheKey)
			return msg, keyboard, true
		}

		// Cache is stale or empty - need to fetch from AI
		loadingMsg := "🔄 正在获取 AI 推荐内容...\n\n这可能需要 15-20 秒\n\n💡 请稍候..."
		log.Printf("[Callback] Returning loading message for hot TV shows")

		// Fetch in background and send update
		go func(uid int64) {
			log.Printf("[Callback] Background goroutine started for hot TV shows")
			results, err := trendingAIManager.GetHotTVShows(8)
			if err == nil && len(results) > 0 {
				log.Printf("[Callback] Got %d hot TV results, sending to user", len(results))
				msg, keyboard := buildTrendingResultsMessageWithKeyboard(results, "📺 热播剧集 (AI精选)", "hot_tv_shows")
				sendPrivateMessage(uid, msg, keyboard)
			} else {
				log.Printf("[Callback] AI trending TV failed: %v", err)
				sendPrivateMessage(uid, "❌ 获取推荐失败，请稍后再试", nil)
			}
		}(userID)

		// Return loading message immediately
		return loadingMsg, nil, true
	}

	// Fallback 1: Try smartSearchMgr
	if smartSearchMgr != nil {
		ctx := &SearchContext{
			UserID: userID,
			Query:  "2024",
			Params: &SearchParams{
				MediaType: "tv",
				Year:      "2024",
			},
		}
		if err := smartSearchMgr.Search(ctx); err != nil {
			log.Printf("[Callback] Hot TV search via smartSearchMgr failed: %v", err)
		} else {
			msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
			return "📺 *热播剧集 (2024)*\n\n" + msg, keyboard, true
		}
	}

	// Fallback 2: Try direct Jellyseerr search with TV filter
	if jellyseerrClient != nil {
		results, err := jellyseerrClient.SearchMedia("2024")
		if err == nil && len(results) > 0 {
			tvResults := filterResultsByMediaType(results, "tv")
			if len(tvResults) > 0 {
				msg := formatSimpleSearchResults("📺 热播剧集", "2024 剧集", tvResults)
				return msg, nil, true
			}
		}
	}

	// Final fallback
	msg := `📺 *热播剧集*

AI 推荐服务正在初始化中

💡 *你可以试试*
1. 直接输入「繁花」搜索
2. 直接输入「三体」搜索
3. 直接输入「狂飙」搜索

💡 或者直接输入任何剧名开始搜索`

	return msg, nil, true
}

// handleNewMoviesSearchCallback handles new movies search with comprehensive fallback
func handleNewMoviesSearchCallback(userID int64) (string, *TelegramInlineKeyboard, bool) {
	log.Printf("[Callback] handleNewMoviesSearchCallback for user %d", userID)

	// Check if AI is enabled and cache is fresh
	if trendingAIManager != nil && trendingAIManager.IsEnabled() {
		cacheKey := "new_releases"

		// Check if we have fresh cached results
		cached := trendingAIManager.GetCachedResults(cacheKey)
		if cached != nil && len(cached) > 0 {
			// Fresh cache - display immediately
			msg, keyboard := buildTrendingResultsMessageWithKeyboard(cached, "🎬 最新上映 (AI精选)", cacheKey)
			return msg, keyboard, true
		}

		// Cache is stale or empty - need to fetch from AI
		loadingMsg := "🔄 正在获取 AI 推荐内容...\n\n这可能需要 15-20 秒\n\n💡 请稍候..."
		log.Printf("[Callback] Returning loading message for new releases")

		// Fetch in background and send update
		go func(uid int64) {
			log.Printf("[Callback] Background goroutine started for new releases")
			results, err := trendingAIManager.GetNewReleases(8)
			if err == nil && len(results) > 0 {
				log.Printf("[Callback] Got %d new release results, sending to user", len(results))
				msg, keyboard := buildTrendingResultsMessageWithKeyboard(results, "🎬 最新上映 (AI精选)", "new_releases")
				sendPrivateMessage(uid, msg, keyboard)
			} else {
				log.Printf("[Callback] AI new releases failed: %v", err)
				sendPrivateMessage(uid, "❌ 获取推荐失败，请稍后再试", nil)
			}
		}(userID)

		// Return loading message immediately
		return loadingMsg, nil, true
	}

	// Fallback 1: Try smartSearchMgr
	if smartSearchMgr != nil {
		ctx := &SearchContext{
			UserID: userID,
			Query:  "2024",
			Params: &SearchParams{
				MediaType: "movie",
				Year:      "2024",
			},
		}
		if err := smartSearchMgr.Search(ctx); err != nil {
			log.Printf("[Callback] New movies search via smartSearchMgr failed: %v", err)
		} else {
			msg, keyboard := FormatSearchResultsWithKeyboard(ctx)
			return "🎬 *最新电影 (2024)*\n\n" + msg, keyboard, true
		}
	}

	// Fallback 2: Try direct Jellyseerr search with movie filter
	if jellyseerrClient != nil {
		results, err := jellyseerrClient.SearchMedia("2024")
		if err == nil && len(results) > 0 {
			movieResults := filterResultsByMediaType(results, "movie")
			if len(movieResults) > 0 {
				msg := formatSimpleSearchResults("🎬 最新电影", "2024 电影", movieResults)
				return msg, nil, true
			}
		}
	}

	// Final fallback
	msg := `🎬 *最新电影*

搜索功能正在初始化中

💡 *你可以试试*
1. 直接输入「沙丘2」搜索
2. 直接输入「奥本海默」搜索
3. 直接输入「 Barbie」搜索

💡 或者直接输入任何电影名开始搜索`

	return msg, nil, true
}

// formatSimpleSearchResults formats search results in a simple way
// formatAIItemDetails formats AI item details for display
func formatAIItemDetails(tmdbID int, mediaType string) string {
	var sb strings.Builder

	emoji := "🎬"
	if mediaType == "tv" {
		emoji = "📺"
	}

	typeLabel := "电影"
	if mediaType == "tv" {
		typeLabel = "剧集"
	}

	sb.WriteString(fmt.Sprintf("┌─ ✨ 详情 ─────────┐\n\n"))
	sb.WriteString(fmt.Sprintf("%s TMDB ID: %d\n", emoji, tmdbID))
	sb.WriteString(fmt.Sprintf("🏷️ 类型: %s\n\n", typeLabel))

	sb.WriteString("━━━━━━━━━━━━━━━\n\n")

	// Create request button
	sb.WriteString("💡 点击下方按钮发起请求\n")

	return sb.String()
}

func formatSimpleSearchResults(title, query string, results []JellyseerrSearchResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("┌─── %s ────┐\n\n", title))
	sb.WriteString(fmt.Sprintf("  关键词: 「%s」\n", query))
	sb.WriteString(fmt.Sprintf("  📄 共 %d 条结果\n\n", len(results)))
	sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	for i, item := range results {
		if i >= 8 {
			break
		}

		emoji := "🎬"
		if item.MediaType == "tv" {
			emoji = "📺"
		}

		titleText := item.Title
		if titleText == "" {
			titleText = item.Name
		}

		sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, titleText))
		if item.ReleaseDate != "" && len(item.ReleaseDate) >= 4 {
			sb.WriteString(fmt.Sprintf(" (%s)", item.ReleaseDate[:4]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n└──────────────────────┘")
	sb.WriteString(fmt.Sprintf("\n\n💡 输入数字 1-%d 查看详情", len(results)))

	return sb.String()
}

// filterResultsByMediaType filters results by media type
func filterResultsByMediaType(results []JellyseerrSearchResult, mediaType string) []JellyseerrSearchResult {
	filtered := make([]JellyseerrSearchResult, 0)
	for _, item := range results {
		if item.MediaType == mediaType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// buildTrendingResultsMessage builds a trending results message (without keyboard)
func buildTrendingResultsMessage(results []*ai.TrendingResult, title, cacheKey string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("┌─── %s ────┐\n\n", title))
	sb.WriteString(fmt.Sprintf("  📅 更新时间: %s\n", trendingAIManager.FormatUpdateTime(cacheKey)))
	sb.WriteString(fmt.Sprintf("  📄 共 %d 条结果\n\n", len(results)))
	sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	for i, item := range results {
		if i >= 8 {
			break
		}

		emoji := "🎬"
		if item.MediaType == "tv" {
			emoji = "📺"
		}

		sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, item.Title))
		if item.Year > 0 {
			sb.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n└──────────────────────┘")
	sb.WriteString(fmt.Sprintf("\n\n💡 点击下方按钮快速请求"))

	return sb.String()
}

// buildTrendingResultsMessageWithKeyboard builds a trending results message with number buttons
func buildTrendingResultsMessageWithKeyboard(results []*ai.TrendingResult, title, cacheKey string) (string, *TelegramInlineKeyboard) {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("┌─── %s ────┐\n\n", title))
	sb.WriteString(fmt.Sprintf("  📅 更新时间: %s\n", trendingAIManager.FormatUpdateTime(cacheKey)))
	sb.WriteString(fmt.Sprintf("  📄 共 %d 条结果\n\n", len(results)))
	sb.WriteString("  ━━━━━━━━━━━━━━━  \n\n")

	// Create keyboard with number buttons (4 per row)
	var keyboard [][]map[string]string

	for i, item := range results {
		if i >= 8 {
			break
		}

		emoji := "🎬"
		if item.MediaType == "tv" {
			emoji = "📺"
		}

		sb.WriteString(fmt.Sprintf("  %s %d. %s", emoji, i+1, item.Title))
		if item.Year > 0 {
			sb.WriteString(fmt.Sprintf(" (%d)", item.Year))
		}
		sb.WriteString("\n")

		// Add button for this item (4 per row)
		if i%4 == 0 {
			keyboard = append(keyboard, []map[string]string{})
		}
		buttonLabel := fmt.Sprintf("%d", i+1)
		mediaType := "movie"
		if item.MediaType == "tv" {
			mediaType = "tv"
		}
		callbackData := fmt.Sprintf("ai_trending_%d_%s", item.TmdbID, mediaType)
		keyboard[len(keyboard)-1] = append(keyboard[len(keyboard)-1], map[string]string{
			"text":         buttonLabel,
			"callback_data": callbackData,
		})
	}

	sb.WriteString("\n└──────────────────────┘")

	return sb.String(), &TelegramInlineKeyboard{InlineKeyboard: keyboard}
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(os.Stdout) // 确保输出到 stdout
	log.Println("[Main] Starting application...")

	// Initialize HTTP client with timeout (security: prevent slowloris attacks)
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	log.Println("[Init] HTTP client initialized with 30s timeout")

	// Initialize AI features (must be called before BotModule)
	log.Println("[Main] Calling InitAITrending...")
	InitAITrending()

	// Initialize chat system and knowledge base
	log.Println("[Main] Initializing chat system...")
	InitChatSystem()

	// Initialize media security checker
	log.Println("[Main] Initializing media security checker...")
	mediaSecurityChecker = NewMediaSecurityChecker()
	mediaSecurityChecker.SetBotToken(botToken)

	// 初始化新的模块化 Bot 系统
	log.Println("[Main] Calling InitBotModule...")
	if err := InitBotModule(); err != nil {
		log.Printf("Warning: Failed to initialize new bot module: %v", err)
		log.Println("Continuing with legacy handler...")
	} else {
		log.Println("✅ New modular bot system initialized")
		// Set admin checker for botModule
		if botModule != nil {
			botModule.SetAdminChecker(isUserAdmin)
			log.Println("[Main] Admin checker set for botModule")
		}
	}

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

	// Set descriptions
	if err := setTelegramDescriptions(); err != nil {
		log.Printf("Warning: Failed to set descriptions: %v", err)
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
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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
		{Command: "start", Description: "👋 开始"},
		{Command: "help", Description: "❓ 帮助"},
		{Command: "search", Description: "🔍 搜索"},
		{Command: "recommend", Description: "🎯 推荐"},
		{Command: "random", Description: "🎲 随机"},
		{Command: "my", Description: "📋 我的请求"},
		{Command: "quota", Description: "📊 配额"},
		{Command: "daily", Description: "🎁 签到"},
		{Command: "link", Description: "🔗 绑定账号"},
		{Command: "prefs", Description: "⚙️ 设置"},
		{Command: "pending", Description: "⏳ 待处理"},
		{Command: "users", Description: "👥 用户"},
		{Command: "stats", Description: "📊 统计"},
	}

	type CommandsRequest struct {
		Commands []BotCommand `json:"commands"`
	}

	payload := CommandsRequest{Commands: commands}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", botToken)
	jsonData, _ := json.Marshal(payload)
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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

// setTelegramDescriptions sets the short and full descriptions for the bot
func setTelegramDescriptions() error {
	// Set short description
	shortDesc := "影视搜索·智能推荐·一键求片"
	shortDescURL := fmt.Sprintf("https://api.telegram.org/bot%s/setMyShortDescription", botToken)
	shortDescPayload := map[string]string{
		"short_description": shortDesc,
		"language_code":     "zh",
	}
	jsonData, _ := json.Marshal(shortDescPayload)
	resp, err := httpClient.Post(shortDescURL, "application/json", bytes.NewBuffer(jsonData))
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			log.Printf("Telegram short description set: %s", shortDesc)
		}
	}

	// Set full description
	fullDesc := `🎬 Emby 影视机器人

✨ 功能特色
• 🔍 智能搜索 - 支持中英文片名搜索
• 🎯 智能推荐 - AI 个性化推荐
• 📺 热门推荐 - 实时热门榜单
• 📋 一键求片 - 快速请求影视内容
• 🔔 实时通知 - 入库/批准/可用通知
• 👥 账号绑定 - 绑定 Jellyseerr 账号

📱 常用命令
/search - 搜索内容
/recommend - 智能推荐
/my - 我的请求
/link - 绑定账号`

	fullDescURL := fmt.Sprintf("https://api.telegram.org/bot%s/setMyDescription", botToken)
	fullDescPayload := map[string]string{
		"description":   fullDesc,
		"language_code": "zh",
	}
	jsonData, _ = json.Marshal(fullDescPayload)
	resp, err = httpClient.Post(fullDescURL, "application/json", bytes.NewBuffer(jsonData))
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			log.Println("Telegram full description set")
		}
	}

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

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
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

// isNewFormatCallback checks if callback data is in new format
func isNewFormatCallback(data string) bool {
	if data == "" {
		return false
	}

	// New format callbacks
	newActions := []string{"search:", "subscribe:", "download:", "page:", "cancel:", "select:", "back:"}
	for _, action := range newActions {
		if strings.HasPrefix(data, action) {
			return true
		}
	}

	return false
}

// shouldUseNewModule checks if message should be handled by new module
// 短消息（<=5字符）不使用新模块，让聊天系统处理
func shouldUseNewModule(text string) bool {
	if text == "" {
		return false
	}

	// 短消息可能是聊天，不使用新模块搜索
	if len(text) <= 5 {
		return false
	}

	// Commands that should use new module
	newCommands := []string{"/ai", "/recommend"}
	for _, cmd := range newCommands {
		if strings.HasPrefix(text, cmd) {
			return true
		}
	}

	// Direct search (no slash, not a legacy command)
	if !strings.HasPrefix(text, "/") {
		// Check if it is a search query (Chinese text or mixed)
		if hasChinese(text) || len(text) > 3 {
			return true
		}
	}

	return false
}

// isExplicitSearchQuery 检查是否是明确的搜索请求
// 只有明确是搜索的消息才返回 true，其他（聊天、闲聊）返回 false
func isExplicitSearchQuery(text string) bool {
	if text == "" {
		return false
	}

	// 命令总是搜索
	if strings.HasPrefix(text, "/") {
		return true
	}

	// 明确的搜索关键词
	searchKeywords := []string{"搜索", "search", "求片", "找", "看", "watch"}
	for _, kw := range searchKeywords {
		if strings.Contains(strings.ToLower(text), kw) {
			return true
		}
	}

	// 媒体名称格式检测（包含年份的通常是搜索）
	// 例如："复仇者联盟 2012"、"阿凡达2009"
	if containsYear(text) {
		return true
	}

	// 默认：不是搜索请求（是聊天）
	return false
}

// containsYear 检查文本中是否包含年份（用于判断是否是搜索）
func containsYear(text string) bool {
	// 检查 1900-2099 年份
	for year := 1990; year <= 2030; year++ {
		if strings.Contains(text, fmt.Sprintf("%d", year)) {
			return true
		}
	}
	return false
}

// hasChinese checks if text contains Chinese characters
func hasChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// formatAPIError formats API errors for user display with actionable guidance
// This provides enterprise-level error handling with diagnostics
func formatAPIError(err error, context string) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	// Build user-friendly error message with diagnostic information
	msg := "❌ *操作失败*\n\n"

	// Parse error type and provide specific guidance
	switch {
	case strings.Contains(errMsg, "401") || strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "ERR_UNAUTHORIZED"):
		msg += "🔑 *API 认证失败*\n\n"
		msg += "可能原因：\n"
		msg += "• API Key 无效或已过期\n"
		msg += "• API Key 配置错误\n\n"
		msg += "📋 请联系管理员检查 JELLYSEERR_API_KEY 配置"

	case strings.Contains(errMsg, "403") || strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "ERR_FORBIDDEN"):
		msg += "🚫 *权限不足*\n\n"
		msg += "可能原因：\n"
		msg += "• API Key 权限不足\n"
		msg += "• 当前操作需要更高权限\n\n"
		msg += "📋 请检查 Jellyseerr 设置中的 API Key 权限"

	case strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "ERR_NOT_FOUND"):
		msg += "🔍 *资源未找到*\n\n"
		msg += "可能原因：\n"
		msg += "• 请求的内容不存在\n"
		msg += "• ID 或路径错误\n\n"
		msg += "💡 请检查输入的 ID 是否正确"

	case strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "ERR_RATE_LIMIT"):
		msg += "⏱️ *请求过于频繁*\n\n"
		msg += "请求超出速率限制，请稍后再试"

	case strings.Contains(errMsg, "400") || strings.Contains(errMsg, "bad request") || strings.Contains(errMsg, "ERR_BAD_REQUEST"):
		msg += "📝 *请求格式错误*\n\n"
		msg += "可能原因：\n"
		msg += "• 请求参数不正确\n"
		msg += "• 数据格式不符合要求\n\n"
		// Try to extract the actual error message from Jellyseerr
		if strings.Contains(errMsg, "|") {
			parts := strings.Split(errMsg, "|")
			if len(parts) > 1 {
				actualError := strings.TrimSpace(parts[len(parts)-1])
				if strings.HasPrefix(actualError, "Error:") {
					actualError = strings.TrimPrefix(actualError, "Error:")
					actualError = strings.TrimSpace(actualError)
				}
				msg += "🔴 错误详情: " + actualError
			}
		}

	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded"):
		msg += "⏰ *请求超时*\n\n"
		msg += "服务器响应时间过长，请稍后再试"

	case strings.Contains(errMsg, "connection") || strings.Contains(errMsg, "connect") || strings.Contains(errMsg, "ERR_HTTP_CLIENT"):
		msg += "🔌 *连接失败*\n\n"
		msg += "无法连接到 Jellyseerr 服务器\n\n"
		msg += "可能原因：\n"
		msg += "• 服务器地址配置错误\n"
		msg += "• 网络连接问题\n"
		msg += "• Jellyseerr 服务未运行"

	default:
		// Generic error with context
		msg += "❌ *" + context + "失败*\n\n"
		// Only show technical details in debug mode or to admins
		if len(errMsg) < 100 {
			msg += "详情: " + errMsg
		} else {
			msg += "详情: " + errMsg[:100] + "..."
		}
	}

	// Add diagnostic info for debugging
	if context != "" {
		msg += "\n\n📊 上下文: " + context
	}

	return msg
}

// isAdminUser checks if a user is an administrator
// This is a centralized helper function for admin checks
func isAdminUser(userID int64) bool {
	userIDStr := fmt.Sprintf("%d", userID)
	adminsMutex.RLock()
	_, exists := admins[userIDStr]
	adminsMutex.RUnlock()
	return exists
}

// isUserAdmin checks if a user is an administrator (alternate naming for compatibility)
func isUserAdmin(userID int64) bool {
	return isAdminUser(userID)
}

