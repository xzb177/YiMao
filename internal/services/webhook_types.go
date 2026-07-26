package services

import (
	"net/http"
	"sync"
	"time"
)

// EmbyWebhookPayload represents an Emby webhook payload
// Emby uses camelCase starting with lowercase
type EmbyWebhookPayload struct {
	Event      string `json:"NotificationType"` // Emby uses NotificationType
	EventField string `json:"Event"`            // Alternative event field
	ItemID     string `json:"ItemId"`
	ItemName   string `json:"ItemName"`
	ItemType   string `json:"ItemType"`
	Library    string `json:"LibraryName"`
	SeriesName string `json:"SeriesName"`
	Season     int    `json:"SeasonNumber"` // Deprecated: use ParentIndexNumber
	Episode    int    `json:"IndexNumber"`  // Deprecated: use IndexNumber in Item
	Overview   string `json:"Overview"`
	Timestamp  string `json:"Timestamp"`
	UserID     string `json:"UserId"`
	UserName   string `json:"UserName"`
	Year       *int   `json:"Year"` // ProductionYear
	// Nested Item object (some Emby versions use this)
	Item *EmbyItem `json:"Item"`
}

// EmbyItem represents a nested item in Emby webhook
type EmbyItem struct {
	Id              string            `json:"Id"` // Item ID
	Name            string            `json:"Name"`
	Type            string            `json:"Type"`
	Year            *int              `json:"Year"`
	Overview        string            `json:"Overview"`
	Genres          []string          `json:"Genres"`
	CommunityRating float64           `json:"CommunityRating"`
	Path            string            `json:"Path"`         // File path
	FileName        string            `json:"FileName"`     // File name
	ProviderIds     map[string]string `json:"ProviderIds"`  // TMDB, IMDb, TVDB IDs
	MediaSources    []EmbyMediaSource `json:"MediaSources"` // Media sources with file size
	// Parent/ Series info for episodes
	SeriesId                string            `json:"SeriesId"`
	SeriesName              string            `json:"SeriesName"`        // Series name for episodes
	SeasonName              string            `json:"SeasonName"`        // Season name
	ParentIndexNumber       *int              `json:"ParentIndexNumber"` // Season number (correct field for episodes)
	IndexNumber             *int              `json:"IndexNumber"`       // Episode number (correct field for episodes)
	ParentBackdropItemId    string            `json:"ParentBackdropItemId"`
	ParentBackdropImageTags []string          `json:"ParentBackdropImageTags"`
	SeriesPrimaryImageTag   string            `json:"SeriesPrimaryImageTag"`
	ParentThumbItemId       string            `json:"ParentThumbItemId"`
	ParentThumbImageTag     string            `json:"ParentThumbImageTag"`
	PrimaryImageAspectRatio float64           `json:"PrimaryImageAspectRatio"`
	ImageTags               map[string]string `json:"ImageTags"`
	BackdropImageTags       []string          `json:"BackdropImageTags"`
}

// EmbyMediaSource represents a media source with file information
type EmbyMediaSource struct {
	Size int64  `json:"Size"` // File size in bytes
	Path string `json:"Path"` // File path
}

// JellyseerrWebhookPayload represents a Jellyseerr webhook payload
type JellyseerrWebhookPayload struct {
	Event     string                 `json:"event"`
	Subject   string                 `json:"subject"`
	Message   string                 `json:"message"`
	Issue     *JellyseerrIssue       `json:"issue,omitempty"`
	Media     *JellyseerrMedia       `json:"media,omitempty"`
	Request   *JellyseerrRequest     `json:"request,omitempty"`
	User      *JellyseerrUserWebhook `json:"user,omitempty"`
	CreatedAt string                 `json:"created_at"`
}

// JellyseerrIssue represents an issue in Jellyseerr
type JellyseerrIssue struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Problem  string `json:"problem"`
	MediaID  int    `json:"mediaId"`
	Provider string `json:"provider"`
}

// JellyseerrMedia represents media in Jellyseerr
type JellyseerrMedia struct {
	MediaType string `json:"mediaType"`
	TmdbID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

// JellyseerrRequest represents a request in Jellyseerr
type JellyseerrRequest struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	MediaID   int    `json:"mediaId"`
	MediaType string `json:"mediaType"`
	CreatedAt string `json:"createdAt"`
}

// JellyseerrUserWebhook represents a user in webhook
type JellyseerrUserWebhook struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// MoviePilotWebhookPayload represents a MoviePilot webhook payload
type MoviePilotWebhookPayload struct {
	Event string `json:"event"` // subscribe, download, complete
	Data  struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Year         string `json:"year"`
		Type         string `json:"type"` // 电影, 电视剧
		Season       int    `json:"season"`
		TotalEpisode int    `json:"total_episode"`
		State        string `json:"state"` // P, S, D, C, F, X
		StatusText   string `json:"status_text"`
		Username     string `json:"username"`
		MediaID      int    `json:"media_id"`
		Poster       string `json:"poster"`
		Overview     string `json:"overview"`
	} `json:"data"`
}

// WebhookService handles webhook processing
type WebhookService struct {
	telegram             *TelegramClient
	moviepilot           *MoviePilotClient
	userMapping          UserMappingStore
	adminService         *AdminService
	preferences          *PreferencesService
	chatID               int64
	embyURL              string
	embyAPIKey           string
	embyUserID           string // Emby user ID for API calls
	embySkipTLSVerify    bool
	mediaNotificationSvc *MediaNotificationService
	messageCache         *MessageCache
	notificationFormat   string       // "simple" or "detailed"
	tmdbAPIKey           string       // TMDB API key for fetching images
	tmdbClient           *http.Client // 共享 TMDB HTTP 客户端
	// embyClient 共享 Emby HTTP 客户端：复用连接池与 TLS 会话，
	// 每个调用方用 context/deadline 控制自己的超时，不再各建 client。
	embyClient *http.Client
	// Episode aggregation - 每个剧集独立的防抖动机制
	epAggregation    map[string]*EpisodeAggregation // key: seriesName_season
	epAggregationMu  sync.RWMutex
	aggregationDelay time.Duration // 聚合延迟时间 (默认60秒)
	// 文件信息缓存 - 避免频繁调用 Emby API
	fileInfoCache    map[string]*cachedFileInfo // key: itemID
	fileInfoCacheMu  sync.RWMutex
	fileInfoCacheTTL time.Duration // 缓存过期时间 (默认1小时)
	// #3 拼车服务（可选注入）：入库通知时 @ 拼车用户。允许为 nil。
	carpool *CarpoolService
	// 审核服务（可选注入）：入库时通知求片用户。允许为 nil。
	review *ReviewService
	// 播放结束推送频率限制：userID → lastPushTime
	playbackPushThrottle   map[int64]time.Time
	playbackPushThrottleMu sync.Mutex
	// goroutine lifecycle
	stopCleanup chan struct{}
}

// SetCarpoolService 注入拼车服务（#3 拼车 +1）。采用 setter 注入避免改动构造函数签名。
func (s *WebhookService) SetCarpoolService(c *CarpoolService) {
	s.carpool = c
}

// SetReviewService 注入审核服务（入库时通知求片用户）。
func (s *WebhookService) SetReviewService(r *ReviewService) {
	s.review = r
}

// cachedFileInfo 缓存的文件信息
type cachedFileInfo struct {
	fileSize  int64
	fileCount int
	cachedAt  time.Time
}

// EpisodeAggregation holds aggregated episode info
type EpisodeAggregation struct {
	SeriesName   string
	SeriesID     string // Series ID for fetching images
	Year         int
	Season       int
	Episodes     []int // episode numbers
	FirstAdded   time.Time
	Quality      string
	FileSize     int64
	FileCount    int
	ImageURL     string
	EnhancedInfo *EmbyEnhancedInfo
	LibraryName  string      // Library name for category detection
	timer        *time.Timer // Independent timer for this aggregation
	mu           sync.Mutex  // Mutex for this specific aggregation
}

// EmbyEnhancedInfo holds enhanced media information from Emby API
type EmbyEnhancedInfo struct {
	Title        string
	Year         int
	Rating       float64
	Genres       []string
	Overview     string
	RunTimeTicks int64
	ImageURL     string
	Quality      string // Resolution (1080p, 2160p, etc.)
	Format       string // Release format (BluRay, WEB-DL, WEBRip, HDTV, etc.)
	FileSize     int64
	FileCount    int
	IsWEBDL      bool   // Deprecated: kept for compatibility, use Format instead
	Container    string // Container format (mkv, mp4, etc.)
	TMDBID       string // TMDB ID for fetching images
	Type         string // Item type (Movie, Series, Episode) for TMDB API
}

type EmbySearchResult struct {
	ID        string
	Title     string
	Year      int
	Type      string
	PosterURL string
	Overview  string
	RunTime   int64 // in ticks
	TMDBID    string
}
