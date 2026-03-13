package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// SiteAdapter defines the interface for site-specific search adapters
type SiteAdapter interface {
	// Name returns the site name
	Name() string

	// Domain returns the site domain (e.g., "hdsky.me")
	Domain() string

	// Search searches for torrents with the given keyword
	// Returns a list of TorrentResources or an error
	Search(keyword string, page int) ([]TorrentResource, error)

	// RequiresAuth returns true if the site requires authentication
	RequiresAuth() bool

	// SetCredentials sets authentication credentials (cookies, passkey, etc.)
	SetCredentials(creds map[string]string)
}

// SiteRegistry manages available site adapters
type SiteRegistry struct {
	adapters      map[string]SiteAdapter // key: domain
	httpClient    *http.Client
	rssCookie     string // Optional RSS cookie for authentication
}

// NewSiteRegistry creates a new site adapter registry
func NewSiteRegistry() *SiteRegistry {
	jar, _ := cookiejar.New(nil)
	return &SiteRegistry{
		adapters: make(map[string]SiteAdapter),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

// HTTPClient returns the shared HTTP client
func (r *SiteRegistry) HTTPClient() *http.Client {
	return r.httpClient
}

// Register registers a site adapter
func (r *SiteRegistry) Register(adapter SiteAdapter) {
	r.adapters[adapter.Domain()] = adapter
}

// Get returns an adapter by domain
func (r *SiteRegistry) Get(domain string) (SiteAdapter, bool) {
	adapter, ok := r.adapters[domain]
	return adapter, ok
}

// GetBySiteID returns an adapter by MoviePilot site ID
func (r *SiteRegistry) GetBySiteID(siteID int, sites []SiteInfo) (SiteAdapter, bool) {
	var siteName string
	for _, s := range sites {
		if s.ID == siteID {
			siteName = s.Name
			break
		}
	}

	if siteName == "" {
		return nil, false
	}

	// Map site names to domains
	siteDomainMap := map[string]string{
		"天空":     "hdsky.me",
		"朱雀":     "zhuque.in",
		"馒头":     "m-team.cc",
		"HD-Sky": "hdsky.me",
		"ZhuQue": "zhuque.in",
		"M-Team": "m-team.cc",
	}

	domain, ok := siteDomainMap[siteName]
	if !ok {
		return nil, false
	}

	return r.Get(domain)
}

// SearchAll searches across all registered sites
func (r *SiteRegistry) SearchAll(keyword string, page int) ([]TorrentResource, error) {
	var allResults []TorrentResource

	for _, adapter := range r.adapters {
		results, err := adapter.Search(keyword, page)
		if err != nil {
			// Log error but continue searching other sites
			fmt.Printf("Error searching %s: %v\n", adapter.Name(), err)
			continue
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// SetRSSCookie sets a global RSS cookie for authenticated RSS feeds
func (r *SiteRegistry) SetRSSCookie(cookie string) {
	r.rssCookie = cookie
}

// ========================================
// SkyIsland (HD-Sky) Adapter
// ========================================

// SkyIslandAdapter searches HD-Sky (hdsky.me) via RSS feed
type SkyIslandAdapter struct {
	baseURL    string
	rssURL     string
	httpClient *http.Client
	passkey    string
}

// NewSkyIslandAdapter creates a new SkyIsland adapter
func NewSkyIslandAdapter(client *http.Client) *SkyIslandAdapter {
	return &SkyIslandAdapter{
		baseURL:    "https://hdsky.me",
		rssURL:     "https://hdsky.me/torrentrss.php",
		httpClient: client,
	}
}

func (a *SkyIslandAdapter) Name() string {
	return "HD-Sky"
}

func (a *SkyIslandAdapter) Domain() string {
	return "hdsky.me"
}

func (a *SkyIslandAdapter) RequiresAuth() bool {
	return true
}

func (a *SkyIslandAdapter) SetCredentials(creds map[string]string) {
	if passkey, ok := creds["passkey"]; ok {
		a.passkey = passkey
	}
}

// Search searches via RSS feed with search parameter
// HD-Sky RSS format: https://hdsky.me/torrentrss.php?passkey=xxx&rows=50&search=keyword
func (a *SkyIslandAdapter) Search(keyword string, page int) ([]TorrentResource, error) {
	// Build RSS URL with search parameter
	rssURL := fmt.Sprintf("%s?passkey=%s&rows=50&search=%s",
		a.rssURL, a.passkey, url.QueryEscape(keyword))

	// Fetch RSS feed
	req, err := http.NewRequest("GET", rssURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch rss: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}

	// Parse RSS feed
	allResults, err := a.parseRSS(resp.Body)
	if err != nil {
		return nil, err
	}

	// Filter results by keyword (in case RSS doesn't support search properly)
	return filterByKeyword(allResults, keyword), nil
}

// parseRSS parses the RSS feed response
func (a *SkyIslandAdapter) parseRSS(r io.Reader) ([]TorrentResource, error) {
	// Read RSS content
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Parse RSS XML
	items, err := parseRSSItems(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}

	var results []TorrentResource
	for _, item := range items {
		resource := TorrentResource{
			SiteName:  a.Name(),
			Title:     item.Title,
			Size:      float64(item.Size),
			Seeders:   item.Seeders,
			Peers:     item.Peers,
			PageURL:   item.Link,
			Enclosure: item.Enclosure,
			PubDate:   item.PubDate.Format("2006-01-02 15:04:05"),
		}
		results = append(results, resource)
	}

	return results, nil
}

// ========================================
// ZhuQue Adapter
// ========================================

// ZhuQueAdapter searches ZhuQue (zhuque.in) via API or RSS
type ZhuQueAdapter struct {
	baseURL    string
	apiURL     string
	rssURL     string
	httpClient *http.Client
	apiKey     string
	passkey    string
}

// NewZhuQueAdapter creates a new ZhuQue adapter
func NewZhuQueAdapter(client *http.Client) *ZhuQueAdapter {
	return &ZhuQueAdapter{
		baseURL:    "https://zhuque.in",
		apiURL:     "https://zhuque.in/api/torrent/search",
		rssURL:     "https://zhuque.in/torrentrss.php",
		httpClient: client,
	}
}

func (a *ZhuQueAdapter) Name() string {
	return "朱雀"
}

func (a *ZhuQueAdapter) Domain() string {
	return "zhuque.in"
}

func (a *ZhuQueAdapter) RequiresAuth() bool {
	return true
}

func (a *ZhuQueAdapter) SetCredentials(creds map[string]string) {
	if apiKey, ok := creds["api_key"]; ok {
		a.apiKey = apiKey
	}
	if passkey, ok := creds["passkey"]; ok {
		a.passkey = passkey
	}
}

// Search searches via API with RSS fallback
func (a *ZhuQueAdapter) Search(keyword string, page int) ([]TorrentResource, error) {
	// Try RSS first (more reliable for PT sites)
	return a.searchRSS(keyword, page)
}

// searchAPI uses ZhuQue's API endpoint
func (a *ZhuQueAdapter) searchAPI(keyword string, page int) ([]TorrentResource, error) {
	apiURL := fmt.Sprintf("%s?page=%d&search=%s",
		a.apiURL, page, url.QueryEscape(keyword))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}

	var apiResp struct {
		Data []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Size        int64  `json:"size"`
			Seeders     int    `json:"seeders"`
			Leechers    int    `json:"leechers"`
			Completed   int    `json:"completed"`
			CreatedAt   string `json:"created_at"`
			DownloadURL string `json:"download_url"`
			DetailsURL  string `json:"details_url"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []TorrentResource
	for _, item := range apiResp.Data {
		resource := TorrentResource{
			SiteName:  a.Name(),
			Title:     item.Name,
			Size:      float64(item.Size),
			Seeders:   item.Seeders,
			Peers:     item.Leechers,
			Grabs:     item.Completed,
			PageURL:   item.DetailsURL,
			Enclosure: item.DownloadURL,
			PubDate:   item.CreatedAt,
		}
		results = append(results, resource)
	}

	return results, nil
}

// searchRSS uses ZhuQue's RSS feed as fallback
func (a *ZhuQueAdapter) searchRSS(keyword string, page int) ([]TorrentResource, error) {
	rssURL := fmt.Sprintf("%s?passkey=%s&rows=50&search=%s",
		a.rssURL, a.passkey, url.QueryEscape(keyword))

	req, err := http.NewRequest("GET", rssURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch rss: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	items, err := parseRSSItems(string(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}

	var results []TorrentResource
	for _, item := range items {
		resource := TorrentResource{
			SiteName:  a.Name(),
			Title:     item.Title,
			Size:      float64(item.Size),
			Seeders:   item.Seeders,
			Peers:     item.Peers,
			PageURL:   item.Link,
			Enclosure: item.Enclosure,
			PubDate:   item.PubDate.Format("2006-01-02 15:04:05"),
		}
		results = append(results, resource)
	}

	// Filter results by keyword
	return filterByKeyword(results, keyword), nil
}

// ========================================
// M-Team Adapter
// ========================================

// MTeamAdapter searches M-Team (m-team.cc) via API or RSS
type MTeamAdapter struct {
	baseURL    string
	apiURL     string
	rssURL     string
	httpClient *http.Client
	apiKey     string
	passkey    string
}

// NewMTeamAdapter creates a new M-Team adapter
func NewMTeamAdapter(client *http.Client) *MTeamAdapter {
	return &MTeamAdapter{
		baseURL:    "https://m-team.cc",
		apiURL:     "https://m-team.cc/api/torrent/search",
		rssURL:     "https://m-team.cc/torrentrss.php",
		httpClient: client,
	}
}

func (a *MTeamAdapter) Name() string {
	return "M-Team"
}

func (a *MTeamAdapter) Domain() string {
	return "m-team.cc"
}

func (a *MTeamAdapter) RequiresAuth() bool {
	return true
}

func (a *MTeamAdapter) SetCredentials(creds map[string]string) {
	if apiKey, ok := creds["api_key"]; ok {
		a.apiKey = apiKey
	}
	if passkey, ok := creds["passkey"]; ok {
		a.passkey = passkey
	}
}

// Search searches via API with RSS fallback
func (a *MTeamAdapter) Search(keyword string, page int) ([]TorrentResource, error) {
	// Try RSS first (more reliable for PT sites)
	return a.searchRSS(keyword, page)
}

// searchAPI uses M-Team's API endpoint
func (a *MTeamAdapter) searchAPI(keyword string, page int) ([]TorrentResource, error) {
	apiURL := fmt.Sprintf("%s?page=%d&search=%s",
		a.apiURL, page, url.QueryEscape(keyword))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}

	var apiResp struct {
		Data []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Size        int64  `json:"size"`
			Seeders     int    `json:"seeders"`
			Leechers    int    `json:"leechers"`
			Completed   int    `json:"completed"`
			CreatedAt   string `json:"created_at"`
			DownloadURL string `json:"download_url"`
			DetailsURL  string `json:"details_url"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []TorrentResource
	for _, item := range apiResp.Data {
		resource := TorrentResource{
			SiteName:  a.Name(),
			Title:     item.Name,
			Size:      float64(item.Size),
			Seeders:   item.Seeders,
			Peers:     item.Leechers,
			Grabs:     item.Completed,
			PageURL:   item.DetailsURL,
			Enclosure: item.DownloadURL,
			PubDate:   item.CreatedAt,
		}
		results = append(results, resource)
	}

	return results, nil
}

// searchRSS uses M-Team's RSS feed as fallback
func (a *MTeamAdapter) searchRSS(keyword string, page int) ([]TorrentResource, error) {
	rssURL := fmt.Sprintf("%s?passkey=%s&rows=50&search=%s",
		a.rssURL, a.passkey, url.QueryEscape(keyword))

	req, err := http.NewRequest("GET", rssURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch rss: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	items, err := parseRSSItems(string(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}

	var results []TorrentResource
	for _, item := range items {
		resource := TorrentResource{
			SiteName:  a.Name(),
			Title:     item.Title,
			Size:      float64(item.Size),
			Seeders:   item.Seeders,
			Peers:     item.Peers,
			PageURL:   item.Link,
			Enclosure: item.Enclosure,
			PubDate:   item.PubDate.Format("2006-01-02 15:04:05"),
		}
		results = append(results, resource)
	}

	// Filter results by keyword
	return filterByKeyword(results, keyword), nil
}

// ========================================
// RSS Parsing Utilities
// ========================================

// RSSItem represents an item in an RSS feed
type RSSItem struct {
	Title       string
	Link        string
	Enclosure   string
	Size        int64
	Seeders     int
	Peers       int
	PubDate     time.Time
	Category    string
	Description string
}

// parseRSSItems parses RSS items from a reader
// Supports both standard RSS and Atom formats
func parseRSSItems(data string) ([]RSSItem, error) {
	var items []RSSItem

	// Simple XML parsing - look for <item> tags in RSS
	lines := strings.Split(data, "\n")
	var currentItem *RSSItem
	inItem := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for item start
		if strings.Contains(line, "<item>") {
			inItem = true
			currentItem = &RSSItem{}
			continue
		}

		// Check for item end
		if strings.Contains(line, "</item>") && currentItem != nil {
			items = append(items, *currentItem)
			inItem = false
			currentItem = nil
			continue
		}

		if !inItem || currentItem == nil {
			continue
		}

		// Parse title
		if strings.Contains(line, "<title>") {
			start := strings.Index(line, "<title>") + 7
			end := strings.Index(line, "</title>")
			if end > start {
				currentItem.Title = strings.TrimSpace(line[start:end])
				// Unescape HTML entities
				currentItem.Title = htmlUnescape(currentItem.Title)
			}
			continue
		}

		// Parse link
		if strings.Contains(line, "<link>") {
			start := strings.Index(line, "<link>") + 6
			end := strings.Index(line, "</link>")
			if end > start {
				currentItem.Link = strings.TrimSpace(line[start:end])
			}
			continue
		}

		// Parse enclosure (download URL)
		if strings.Contains(line, "<enclosure") {
			// Extract URL attribute
			urlStart := strings.Index(line, "url=\"") + 5
			if urlStart > 4 {
				urlEnd := strings.Index(line[urlStart:], "\"")
				if urlEnd > 0 {
					currentItem.Enclosure = strings.TrimSpace(line[urlStart : urlStart+urlEnd])
				}
			}
			// Extract length attribute
			lenStart := strings.Index(line, "length=\"") + 8
			if lenStart > 7 {
				lenEnd := strings.Index(line[lenStart:], "\"")
				if lenEnd > 0 {
					fmt.Sscanf(line[lenStart:lenStart+lenEnd], "%d", &currentItem.Size)
				}
			}
			continue
		}

		// Parse description (may contain seeders/leechers info)
		if strings.Contains(line, "<description>") {
			start := strings.Index(line, "<description>") + 13
			end := strings.Index(line, "</description>")
			if end > start {
				currentItem.Description = strings.TrimSpace(line[start:end])
				// Try to extract seeders/leechers from description
				currentItem.Seeders, currentItem.Peers = extractSeedersPeers(currentItem.Description)
			}
			continue
		}

		// Parse seeders/peers from description or comments (fallback)
		if currentItem.Seeders == 0 && (strings.Contains(line, "seeders") || strings.Contains(line, "做种")) {
			currentItem.Seeders = parseIntFromLine(line)
		}
		if currentItem.Peers == 0 && (strings.Contains(line, "leechers") || strings.Contains(line, "下载中")) {
			currentItem.Peers = parseIntFromLine(line)
		}

		// Parse pubDate
		if strings.Contains(line, "<pubDate>") {
			start := strings.Index(line, "<pubDate>") + 9
			end := strings.Index(line, "</pubDate>")
			if end > start {
				dateStr := strings.TrimSpace(line[start:end])
				// Try common date formats
				for _, format := range []string{
					time.RFC1123,
					time.RFC1123Z,
					"2006-01-02T15:04:05Z",
					"2006-01-02 15:04:05",
				} {
					if t, err := time.Parse(format, dateStr); err == nil {
						currentItem.PubDate = t
						break
					}
				}
			}
			continue
		}
	}

	return items, nil
}

// htmlUnescape unescapes HTML entities
func htmlUnescape(s string) string {
	replacements := map[string]string{
		"&lt;":     "<",
		"&gt;":     ">",
		"&amp;":    "&",
		"&quot;":   "\"",
		"&apos;":   "'",
		"&#39;":    "'",
		"&nbsp;":   " ",
		"&ndash;":  "-",
		"&mdash;":  "—",
		"&hellip;": "...",
		"\n":       " ",
		"\t":       " ",
		"\r":       " ",
	}

	result := s
	for entity, replacement := range replacements {
		result = strings.ReplaceAll(result, entity, replacement)
	}

	// Handle CDATA sections
	if strings.Contains(result, "[CDATA[") {
		start := strings.Index(result, "[CDATA[") + 7
		end := strings.Index(result, "]]")
		if end > start {
			result = result[start:end]
		}
	}

	return strings.TrimSpace(result)
}

// parseSizeFromDescription parses file size from description text
func parseSizeFromDescription(desc string) int64 {
	desc = strings.ToLower(desc)

	// Look for patterns like "1.5 gb", "500 mb", "2 tb"
	var size float64
	var unit string

	// Try to match "X.XX GB" pattern
	if strings.Contains(desc, "gb") {
		fmt.Sscanf(desc, "%f%s", &size, &unit)
		if strings.Contains(unit, "gb") || strings.Contains(desc, "gib") {
			return int64(size * 1024 * 1024 * 1024)
		}
	}

	if strings.Contains(desc, "mb") {
		fmt.Sscanf(desc, "%f%s", &size, &unit)
		if strings.Contains(unit, "mb") || strings.Contains(desc, "mib") {
			return int64(size * 1024 * 1024)
		}
	}

	// Extract number followed by unit
	fields := strings.Fields(desc)
	for i, field := range fields {
		if i > 0 && i < len(fields)-1 {
			if _, err := fmt.Sscanf(field, "%f", &size); err == nil {
				nextField := strings.ToLower(fields[i+1])
				if strings.Contains(nextField, "gb") || strings.Contains(nextField, "gib") {
					return int64(size * 1024 * 1024 * 1024)
				} else if strings.Contains(nextField, "mb") || strings.Contains(nextField, "mib") {
					return int64(size * 1024 * 1024)
				} else if strings.Contains(nextField, "tb") {
					return int64(size * 1024 * 1024 * 1024 * 1024)
				}
			}
		}
	}

	return 0
}

// parseIntFromLine parses an integer from a line of text
func parseIntFromLine(line string) int {
	var num int
	fields := strings.Fields(line)
	for _, field := range fields {
		if _, err := fmt.Sscanf(field, "%d", &num); err == nil {
			return num
		}
	}
	return 0
}

// extractSeedersPeers extracts seeders and peers from description text
// Supports multiple formats:
// - "Seeders: 10, Leechers: 5"
// - "做种数: 10, 下载数: 5"
// - "S: 10, L: 5"
// - "10 seeders, 5 leechers"
func extractSeedersPeers(desc string) (seeders, peers int) {
	desc = strings.ToLower(desc)

	// Try to find seeders number
	// Look for patterns like "seeders: 10", "做种数:10", "s:10", "10 seeders"
	for _, pattern := range []string{
		"seeders:", "seeders ", "seeder:", "seeder ",
		"做种数:", "做种数 ", "做种:", "做种 ",
		"s:", "s ",
		"上传:", "上传者:",
	} {
		if idx := strings.Index(desc, pattern); idx != -1 {
			remaining := desc[idx+len(pattern):]
			var num int
			if _, err := fmt.Sscanf(strings.TrimSpace(remaining), "%d", &num); err == nil {
				seeders = num
				break
			}
		}
	}

	// Try to find leechers/peers number
	for _, pattern := range []string{
		"leechers:", "leechers ", "leecher:", "leecher ",
		"下载数:", "下载数 ", "下载中:", "下载中 ",
		"l:", "l ",
		"下载:", "下载者:",
	} {
		if idx := strings.Index(desc, pattern); idx != -1 {
			remaining := desc[idx+len(pattern):]
			var num int
			if _, err := fmt.Sscanf(strings.TrimSpace(remaining), "%d", &num); err == nil {
				peers = num
				break
			}
		}
	}

	// Try format "数字/数字" like "10/5" (seeders/leechers)
	if seeders == 0 && peers == 0 {
		parts := strings.Fields(desc)
		for _, part := range parts {
			if strings.Contains(part, "/") {
				slashParts := strings.Split(part, "/")
				if len(slashParts) == 2 {
					var s, p int
					if _, err := fmt.Sscanf(strings.TrimSpace(slashParts[0]), "%d", &s); err == nil {
						if _, err := fmt.Sscanf(strings.TrimSpace(slashParts[1]), "%d", &p); err == nil {
							seeders = s
							peers = p
							break
						}
					}
				}
			}
		}
	}

	return seeders, peers
}

// filterByKeyword filters results by keyword, keeping only matching results
func filterByKeyword(results []TorrentResource, keyword string) []TorrentResource {
	if keyword == "" {
		return results
	}

	// Split keyword into parts (e.g., "绿液惊魂 2026" -> ["绿液惊魂", "2026"])
	keywordParts := strings.Fields(strings.ToLower(keyword))

	var filtered []TorrentResource
	for _, res := range results {
		titleLower := strings.ToLower(res.Title)

		// Check if all keyword parts are present in the title
		matches := true
		for _, part := range keywordParts {
			if !strings.Contains(titleLower, strings.ToLower(part)) {
				matches = false
				break
			}
		}

		if matches {
			filtered = append(filtered, res)
		}
	}

	return filtered
}
