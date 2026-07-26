package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ============================================================
//  观影历史服务 — Emby API 采集 + 本地缓存
// ============================================================

// ViewingRecord 单条观影记录
type ViewingRecord struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Year      int       `json:"year"`
	Genres    []string  `json:"genres"`
	Rating    float64   `json:"rating"`
	Type      string    `json:"type"` // movie / series
	WatchedAt time.Time `json:"watched_at"`
}

// UserViewingProfile 用户观影画像
type UserViewingProfile struct {
	UserID      string
	UserName    string
	Records     []ViewingRecord
	TopGenres   []ViewingGenreCount
	LastUpdated time.Time
}

type ViewingGenreCount struct {
	Genre string
	Count int
}

// ViewingHistoryService 观影历史服务
type ViewingHistoryService struct {
	embyURL    string
	embyAPIKey string
	httpClient *http.Client

	// 缓存：userID -> profile
	cache    map[string]*UserViewingProfile
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

// NewViewingHistoryService 创建观影历史服务
func NewViewingHistoryService(embyURL, embyAPIKey string) *ViewingHistoryService {
	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
	}
	return &ViewingHistoryService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		httpClient: &http.Client{Timeout: 20 * time.Second, Transport: transport},
		cache:      make(map[string]*UserViewingProfile),
		cacheTTL:   30 * time.Minute, // 30分钟刷新一次
	}
}

// FindEmbyUserByName 通过用户名查找 Emby 用户 ID
func (s *ViewingHistoryService) FindEmbyUserByName(name string) (string, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return "", fmt.Errorf("Emby 未配置")
	}

	resp, err := embydoGet(s.httpClient, s.embyURL, s.embyAPIKey, "/Users?IsDisabled=false")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var users []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", err
	}

	// 精确匹配
	for _, u := range users {
		if strings.EqualFold(u.Name, name) {
			return u.ID, nil
		}
	}
	// 模糊匹配
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), strings.ToLower(name)) ||
			strings.Contains(strings.ToLower(name), strings.ToLower(u.Name)) {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("未找到 Emby 用户: %s", name)
}

// GetProfile 获取用户观影画像（带缓存）
func (s *ViewingHistoryService) GetProfile(userID, userName string) (*UserViewingProfile, error) {
	// 缓存命中
	s.cacheMu.RLock()
	if profile, ok := s.cache[userID]; ok && time.Since(profile.LastUpdated) < s.cacheTTL {
		s.cacheMu.RUnlock()
		return profile, nil
	}
	s.cacheMu.RUnlock()

	// 从 Emby 拉取
	profile, err := s.fetchFromEmby(userID, userName)
	if err != nil {
		return nil, err
	}

	// 写入缓存
	s.cacheMu.Lock()
	s.cache[userID] = profile
	s.cacheMu.Unlock()

	return profile, nil
}

// fetchFromEmby 从 Emby API 拉取观影历史
func (s *ViewingHistoryService) fetchFromEmby(userID, userName string) (*UserViewingProfile, error) {
	profile := &UserViewingProfile{
		UserID:   userID,
		UserName: userName,
	}

	// 并发拉取电影和剧集
	types := []string{"Movie", "Series"}
	type result struct {
		items []ViewingRecord
		typ   string
		err   error
	}

	ch := make(chan result, len(types))
	for _, typ := range types {
		go func(t string) {
			items, err := s.fetchLatest(userID, t, 30)
			ch <- result{items: items, typ: t, err: err}
		}(typ)
	}

	genreMap := make(map[string]int)
	for range types {
		r := <-ch
		if r.err != nil {
			logger.Info("[ViewingHistory] Failed to fetch %s for %s: %v", r.typ, userName, r.err)
			continue
		}
		for _, item := range r.items {
			profile.Records = append(profile.Records, item)
			for _, g := range item.Genres {
				genreMap[g]++
			}
		}
	}

	// 统计类型偏好
	for genre, count := range genreMap {
		profile.TopGenres = append(profile.TopGenres, ViewingGenreCount{Genre: genre, Count: count})
	}
	sort.Slice(profile.TopGenres, func(i, j int) bool { return profile.TopGenres[i].Count > profile.TopGenres[j].Count })

	profile.LastUpdated = time.Now()
	return profile, nil
}

// fetchLatest 从 Emby API 获取最近观看
func (s *ViewingHistoryService) fetchLatest(userID, itemType string, limit int) ([]ViewingRecord, error) {
	path := fmt.Sprintf("/Users/%s/Items/Latest?IncludeItemTypes=%s&Limit=%d&Fields=Genres,CommunityRating,UserData",
		userID, itemType, limit)

	resp, err := embydoGet(s.httpClient, s.embyURL, s.embyAPIKey, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID              string   `json:"Id"`
		Name            string   `json:"Name"`
		ProductionYear  int      `json:"ProductionYear"`
		Genres          []string `json:"Genres"`
		CommunityRating float64  `json:"CommunityRating"`
		SeriesName      string   `json:"SeriesName"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	var items []ViewingRecord
	for _, r := range raw {
		name := r.Name
		if r.SeriesName != "" {
			name = r.SeriesName
		}
		items = append(items, ViewingRecord{
			ID:     r.ID,
			Title:  name,
			Year:   r.ProductionYear,
			Genres: r.Genres,
			Rating: r.CommunityRating,
			Type:   strings.ToLower(itemType),
		})
	}
	return items, nil
}

// GetTopGenre 获取用户最喜欢的类型
func (s *ViewingHistoryService) GetTopGenre(userID, userName string) string {
	profile, err := s.GetProfile(userID, userName)
	if err != nil || len(profile.TopGenres) == 0 {
		return ""
	}
	return profile.TopGenres[0].Genre
}

// GetRecentTitles 获取用户最近观看的电影名列表
func (s *ViewingHistoryService) GetRecentTitles(userID, userName string, limit int) []string {
	profile, err := s.GetProfile(userID, userName)
	if err != nil {
		return nil
	}
	var titles []string
	for i, r := range profile.Records {
		if i >= limit {
			break
		}
		titles = append(titles, r.Title)
	}
	return titles
}

// HasWatched 检查用户是否看过某部电影
func (s *ViewingHistoryService) HasWatched(userID, userName, movieName string) bool {
	profile, err := s.GetProfile(userID, userName)
	if err != nil {
		return false
	}
	lower := strings.ToLower(movieName)
	for _, r := range profile.Records {
		if strings.ToLower(r.Title) == lower {
			return true
		}
	}
	return false
}

// InvalidateCache 清除用户缓存
func (s *ViewingHistoryService) InvalidateCache(userID string) {
	s.cacheMu.Lock()
	delete(s.cache, userID)
	s.cacheMu.Unlock()
}
