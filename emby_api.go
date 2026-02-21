package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EmbyItemInfo represents detailed item information from Emby API
type EmbyItemInfo struct {
	ID               string  `json:"Id"`
	Name             string  `json:"Name"`
	OriginalTitle    string  `json:"OriginalTitle"`
	Type             string  `json:"Type"`
	ProductionYear   int     `json:"ProductionYear"`
	CommunityRating  float64 `json:"CommunityRating"`
	Overview         string  `json:"Overview"`
	Genres           []string `json:"Genres"`
	RunTimeTicks     int64   `json:"RunTimeTicks"`

	// Image tags for constructing image URLs
	ImageTags        map[string]string `json:"ImageTags"`
	BackdropImageTags []string         `json:"BackdropImageTags"`

	// Primary image info
	PrimaryImageItemId string           `json:"PrimaryImageItemId"`
	HasPrimaryImage   bool              `json:"HasPrimaryImage"`
	HasBackdrop       bool              `json:"HasBackdrop"`

	// Series/Season parent info
	SeriesId         string  `json:"SeriesId"`
	SeasonId         string  `json:"SeasonId"`
	ParentBackdropItemId string  `json:"ParentBackdropItemId"`

	// Media info
	MediaSources     []struct {
		Path             string `json:"Path"`
		Size             int64  `json:"Size"`
		Width            int    `json:"Width"`
		Height           int    `json:"Height"`
		MediaStreams     []struct {
			Type            string `json:"Type"`
			Codec           string `json:"Codec"`
			Width           int    `json:"Width"`
			Height          int    `json:"Height"`
			BitRate         int    `json:"BitRate"`
			Channels        float64 `json:"Channels"`
		} `json:"MediaStreams"`
	} `json:"MediaSources"`

	// For series/seasons
	IndexNumber      int     `json:"IndexNumber"`
	ParentIndexNumber int    `json:"ParentIndexNumber"`
	SeriesName       string  `json:"SeriesName"`
	SeasonName       string  `json:"SeasonName"`

	// Child count (for seasons/series)
	ChildCount       int     `json:"ChildCount"`
}

// EmbyCache for caching item info
var embyCache = struct {
	sync.RWMutex
	data map[string]*EmbyItemInfo
	ttl  map[string]time.Time
}{data: make(map[string]*EmbyItemInfo), ttl: make(map[string]time.Time)}

// getEmbyURL returns the Emby server URL from environment
func getEmbyURL() string {
	if url := os.Getenv("EMBY_URL"); url != "" {
		return strings.TrimSuffix(url, "/")
	}
	return ""
}

// getEmbyAPIKey returns the Emby API key from environment
func getEmbyAPIKey() string {
	return os.Getenv("EMBY_API_KEY")
}

// GetEmbyItemInfo fetches detailed item info from Emby API
func GetEmbyItemInfo(itemID string) (*EmbyItemInfo, error) {
	if itemID == "" {
		return nil, fmt.Errorf("empty item ID")
	}

	embyURL := getEmbyURL()
	apiKey := getEmbyAPIKey()

	if embyURL == "" || apiKey == "" {
		return nil, fmt.Errorf("EMBY_URL or EMBY_API_KEY not configured")
	}

	// Check cache - use read lock and check both maps atomically
	embyCache.RLock()
	info, infoExists := embyCache.data[itemID]
	ttl, ttlExists := embyCache.ttl[itemID]
	embyCache.RUnlock()

	// Return cached value if it exists and is not expired
	if infoExists && ttlExists && time.Now().Before(ttl) {
		return info, nil
	}

	// Build API URL with MediaSources field
	apiURL := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=MediaSources,MediaStreams,ChildCount,ImageTags,BackdropImageTags", embyURL, getEmbyUserID(), itemID)

	// Create request
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch item: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result EmbyItemInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Cache result (5 minutes TTL)
	embyCache.Lock()
	embyCache.data[itemID] = &result
	embyCache.ttl[itemID] = time.Now().Add(5 * time.Minute)
	embyCache.Unlock()

	return &result, nil
}

// getEmbyUserID returns the admin user ID for Emby API calls
func getEmbyUserID() string {
	// Try to get from environment
	if uid := os.Getenv("EMBY_USER_ID"); uid != "" {
		return uid
	}
	// Default to a common admin user ID
	// In production, this should be configured properly
	return "2c6134866fd445839513642df0418103"
}

// GetEmbySeasonInfo fetches season info with all episodes
func GetEmbySeasonInfo(itemID string) (*EmbyItemInfo, error) {
	embyURL := getEmbyURL()
	apiKey := getEmbyAPIKey()

	if embyURL == "" || apiKey == "" {
		return nil, fmt.Errorf("EMBY_URL or EMBY_API_KEY not configured")
	}

	// Build API URL for season with children
	apiURL := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=ChildCount,ImageTags,BackdropImageTags,SeriesId,ParentBackdropItemId", embyURL, getEmbyUserID(), itemID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch season: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var info EmbyItemInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// If ChildCount is 0, try to get it from parent items query
	if info.ChildCount == 0 {
		childURL := fmt.Sprintf("%s/Users/%s/Items?ParentId=%s&Limit=1", embyURL, getEmbyUserID(), itemID)
		req2, err := http.NewRequest("GET", childURL, nil)
		if err == nil {
			req2.Header.Set("X-Emby-Token", apiKey)
			resp2, err := client.Do(req2)
			if err == nil && resp2.StatusCode == http.StatusOK {
				defer resp2.Body.Close()
				var result struct {
					TotalRecordCount int `json:"TotalRecordCount"`
				}
				if json.NewDecoder(resp2.Body).Decode(&result) == nil {
					info.ChildCount = result.TotalRecordCount
				}
			}
		}
	}

	return &info, nil
}

// FormatMediaSize formats bytes to human readable size
func FormatMediaSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2fT", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2fG", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2fM", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2fK", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// GetMediaQuality extracts quality info from video stream
func GetMediaQuality(info *EmbyItemInfo) string {
	if len(info.MediaSources) == 0 {
		return "未知"
	}

	source := info.MediaSources[0]
	width := 0
	height := 0

	// Get resolution from MediaStreams
	for _, stream := range source.MediaStreams {
		if stream.Type == "Video" {
			if stream.Width > 0 {
				width = stream.Width
			}
			if stream.Height > 0 {
				height = stream.Height
			}
			break // Use first video stream
		}
	}

	// Also check Width/Height fields if no stream info
	if width == 0 && source.Width > 0 {
		width = source.Width
	}
	if height == 0 && source.Height > 0 {
		height = source.Height
	}

	// If still no resolution, return unknown
	if height == 0 {
		return "未知"
	}

	// Determine quality based on height and width
	var quality string
	switch {
	case width >= 3800 || height >= 2160:  // 4K or wider
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

	// Check if it's WEB-DL by examining the path
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

// ParseSeasonEpisode parses season/episode info from filename
func ParseSeasonEpisode(filename string) (season, episode int, title string) {
	// Try to extract S01E01 pattern
	re := regexp.MustCompile(`[Ss](\d+)[Ee](\d+)`)
	if matches := re.FindStringSubmatch(filename); len(matches) >= 3 {
		s, _ := strconv.Atoi(matches[1])
		e, _ := strconv.Atoi(matches[2])
		return s, e, ""
	}

	// Try E01 pattern (standalone episode)
	re = regexp.MustCompile(`[Ee](\d+)`)
	if matches := re.FindStringSubmatch(filename); len(matches) >= 2 {
		e, _ := strconv.Atoi(matches[1])
		return 0, e, ""
	}

	return 0, 0, ""
}

// FormatEpisodeRange formats episode range like "E01-E08"
func FormatEpisodeRange(info *EmbyItemInfo) string {
	if info.Type != "Season" {
		return ""
	}

	// For seasons, we need to get children count
	if info.ChildCount > 0 {
		// Return episode range
		return fmt.Sprintf("E01-E%02d", info.ChildCount)
	}

	// Try to get from parent info
	if info.IndexNumber > 0 {
		return fmt.Sprintf("S%02d", info.IndexNumber)
	}

	return ""
}

// GetMediaCategory returns the media category (e.g., "国产剧", "美剧", etc.)
func GetMediaCategory(info *EmbyItemInfo) string {
	if len(info.Genres) == 0 {
		return "未分类"
	}

	// Check for specific genres to determine category
	genreLower := strings.ToLower(strings.Join(info.Genres, " "))

	switch {
	case strings.Contains(genreLower, "chinese") || strings.Contains(genreLower, "国语"):
		return "国产剧"
	case strings.Contains(genreLower, "korean"):
		return "韩剧"
	case strings.Contains(genreLower, "japanese"):
		return "日剧"
	case strings.Contains(genreLower, "american") || strings.Contains(genreLower, "english"):
		return "美剧"
	case strings.Contains(genreLower, "thai"):
		return "泰剧"
	default:
		// Return first genre
		return info.Genres[0]
	}
}

// GetTotalSize calculates total size of all media sources
func GetTotalSize(info *EmbyItemInfo) int64 {
	var total int64
	for _, source := range info.MediaSources {
		total += source.Size
	}
	return total
}

// GetFileCount returns the number of media sources
func GetFileCount(info *EmbyItemInfo) int {
	return len(info.MediaSources)
}

// GetBackdropURL returns the backdrop image URL (horizontal, landscape)
// Perfect for mobile messaging, 16:9 aspect ratio
func GetBackdropURL(info *EmbyItemInfo) string {
	embyURL := getEmbyURL()
	if embyURL == "" || info.ID == "" {
		return ""
	}

	// Try different sources for backdrop image
	var itemID, tag string
	maxWidth := 800  // Optimized for mobile

	// Priority 1: Use item's own backdrop (check BackdropImageTags directly)
	if len(info.BackdropImageTags) > 0 {
		itemID = info.ID
		tag = info.BackdropImageTags[0]
	} else if info.ParentBackdropItemId != "" {
		// Priority 2: Use parent's backdrop (for episodes/seasons)
		itemID = info.ParentBackdropItemId
		// Fetch the parent item to get its backdrop tag
		tag = fetchBackdropTag(itemID)
	} else if info.SeriesId != "" {
		// Priority 3: Use series backdrop (for episodes/seasons)
		itemID = info.SeriesId
		tag = fetchBackdropTag(itemID)
	}

	if itemID == "" {
		return ""
	}

	// Construct backdrop URL
	// Format: {embyURL}/Items/{itemId}/Images/Backdrop/{index}?tag={tag}&maxWidth={width}
	if tag != "" {
		return fmt.Sprintf("%s/Items/%s/Images/Backdrop/0?tag=%s&maxWidth=%d&quality=90",
			embyURL, itemID, tag, maxWidth)
	}
	return fmt.Sprintf("%s/Items/%s/Images/Backdrop/0?maxWidth=%d&quality=90",
		embyURL, itemID, maxWidth)
}

// fetchBackdropTag fetches the backdrop image tag for an item
func fetchBackdropTag(itemID string) string {
	embyURL := getEmbyURL()
	apiKey := getEmbyAPIKey()

	if embyURL == "" || apiKey == "" || itemID == "" {
		return ""
	}

	apiURL := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=BackdropImageTags", embyURL, getEmbyUserID(), itemID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result struct {
		BackdropImageTags []string `json:"BackdropImageTags"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	if len(result.BackdropImageTags) > 0 {
		return result.BackdropImageTags[0]
	}

	return ""
}

// GetPrimaryImageURL returns the primary poster URL
// This is vertical poster, use only if no backdrop available
func GetPrimaryImageURL(info *EmbyItemInfo) string {
	embyURL := getEmbyURL()
	if embyURL == "" || info.ID == "" {
		return ""
	}

	var itemID string
	var tag string

	// Use primary image tag
	if info.ImageTags != nil {
		if t, ok := info.ImageTags["Primary"]; ok {
			tag = t
		}
	}

	itemID = info.ID

	if itemID == "" {
		return ""
	}

	// For mobile, use landscape aspect ratio from primary image
	// by requesting a crop or using banner type
	maxWidth := 600
	maxHeight := 340

	if tag != "" {
		return fmt.Sprintf("%s/Items/%s/Images/Primary?tag=%s&maxWidth=%d&maxHeight=%d&quality=90",
			embyURL, itemID, tag, maxWidth, maxHeight)
	}
	return fmt.Sprintf("%s/Items/%s/Images/Primary?maxWidth=%d&maxHeight=%d&quality=90",
		embyURL, itemID, maxWidth, maxHeight)
}

// GetBestImageURL returns the best image URL for the item
// Prefers backdrop (landscape) over primary (portrait)
func GetBestImageURL(info *EmbyItemInfo) string {
	// Try backdrop first (horizontal, perfect for mobile)
	if backdropURL := GetBackdropURL(info); backdropURL != "" {
		return backdropURL
	}
	// Fallback to primary image
	return GetPrimaryImageURL(info)
}

// FetchSeriesBackdrop attempts to fetch series backdrop for episodes/seasons
func FetchSeriesBackdrop(seriesID string) string {
	embyURL := getEmbyURL()
	apiKey := getEmbyAPIKey()

	if embyURL == "" || apiKey == "" || seriesID == "" {
		return ""
	}

	// Build API URL to get series info with image tags
	apiURL := fmt.Sprintf("%s/Users/%s/Items/%s?Fields=ImageTags,BackdropImageTags",
		embyURL, getEmbyUserID(), seriesID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var seriesInfo struct {
		ID               string            `json:"Id"`
		HasBackdrop      bool              `json:"HasBackdrop"`
		BackdropImageTags []string         `json:"BackdropImageTags"`
		ImageTags        map[string]string `json:"ImageTags"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&seriesInfo); err != nil {
		return ""
	}

	if !seriesInfo.HasBackdrop || len(seriesInfo.BackdropImageTags) == 0 {
		return ""
	}

	tag := seriesInfo.BackdropImageTags[0]
	return fmt.Sprintf("%s/Items/%s/Images/Backdrop/0?tag=%s&maxWidth=800&quality=90",
		embyURL, seriesInfo.ID, tag)
}

// EpisodeFile represents a single episode with file info
type EpisodeFile struct {
	IndexNumber int    `json:"IndexNumber"`    // Episode number
	Name        string `json:"Name"`           // Episode name
	Size        int64  `json:"Size"`           // File size in bytes
	Width       int    `json:"Width"`          // Video width
	Height      int    `json:"Height"`         // Video height
	Codec       string `json:"Codec"`          // Video codec
}

// GetSeasonEpisodesInfo fetches all episodes in a season with their file details
func GetSeasonEpisodesInfo(seasonID string) ([]EpisodeFile, error) {
	embyURL := getEmbyURL()
	apiKey := getEmbyAPIKey()

	if embyURL == "" || apiKey == "" {
		return nil, fmt.Errorf("EMBY_URL or EMBY_API_KEY not configured")
	}

	// Build API URL to get all episodes in the season
	// Sort by IndexNumber to get episodes in order
	apiURL := fmt.Sprintf("%s/Users/%s/Items?ParentId=%s&SortBy=SortName&SortOrder=Ascending&Fields=MediaSources,MediaStreams,IndexNumber",
		embyURL, getEmbyUserID(), seasonID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch episodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			IndexNumber  int `json:"IndexNumber"`
			Name         string `json:"Name"`
			MediaSources []struct {
				Path         string `json:"Path"`
				Size         int64  `json:"Size"`
				Width        int    `json:"Width"`
				Height       int    `json:"Height"`
				MediaStreams []struct {
					Type  string `json:"Type"`
					Codec string `json:"Codec"`
					Width int    `json:"Width"`
					Height int   `json:"Height"`
				} `json:"MediaStreams"`
			} `json:"MediaSources"`
		} `json:"Items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	episodes := make([]EpisodeFile, 0, len(result.Items))
	for _, item := range result.Items {
		if len(item.MediaSources) == 0 {
			continue
		}

		source := item.MediaSources[0]
		ep := EpisodeFile{
			IndexNumber: item.IndexNumber,
			Name:        item.Name,
			Size:        source.Size,
			Width:       source.Width,
			Height:      source.Height,
		}

		// Get video codec from MediaStreams
		for _, stream := range source.MediaStreams {
			if stream.Type == "Video" {
				if stream.Codec != "" {
					ep.Codec = strings.ToUpper(stream.Codec)
				}
				if ep.Width == 0 && stream.Width > 0 {
					ep.Width = stream.Width
				}
				if ep.Height == 0 && stream.Height > 0 {
					ep.Height = stream.Height
				}
				break
			}
		}

		episodes = append(episodes, ep)
	}

	return episodes, nil
}

// FormatEpisodesFileList formats a list of episodes with file info into a readable string
func FormatEpisodesFileList(episodes []EpisodeFile) string {
	if len(episodes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n📄 文件详情：\n")
	sb.WriteString("───────────────────\n")

	for _, ep := range episodes {
		// Episode number (E01, E02, etc.)
		epNum := fmt.Sprintf("E%02d", ep.IndexNumber)

		// Format size
		sizeStr := FormatMediaSize(ep.Size)

		// Format quality
		quality := ""
		if ep.Height > 0 {
			if ep.Height >= 2160 {
				quality = "4K"
			} else if ep.Height >= 1080 {
				quality = "1080p"
			} else if ep.Height >= 720 {
				quality = "720p"
			} else {
				quality = fmt.Sprintf("%dp", ep.Height)
			}
		}

		// Format codec
		codec := ""
		if ep.Codec != "" {
			codec = fmt.Sprintf(" [%s]", ep.Codec)
		}

		// Build the line
		sb.WriteString(fmt.Sprintf("  %s · %s", epNum, sizeStr))
		if quality != "" {
			sb.WriteString(fmt.Sprintf(" · %s%s", quality, codec))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
