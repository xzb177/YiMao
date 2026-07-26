package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// SeasonRadarItem 描述用户追踪的剧集续季状态。
type SeasonRadarItem struct {
	UserID       int64     `json:"user_id"`
	TmdbID       int       `json:"tmdb_id"`
	Title        string    `json:"title"`
	KnownSeasons int       `json:"known_seasons"`
	LastChecked  time.Time `json:"last_checked"`
}

// SeasonRadarService 维护用户求过的剧集，并周期性侦测 TMDB 新季。
type SeasonRadarService struct {
	mu      sync.Mutex
	file    string
	items   map[string]SeasonRadarItem
	tmdb    *TMDBClient
	notify  func(userID int64, tmdbID int, title string, season TVSeason) bool
	enabled func(userID int64) bool
}

func NewSeasonRadarService(dataDir string, tmdb *TMDBClient) *SeasonRadarService {
	s := &SeasonRadarService{
		file:  filepath.Join(dataDir, "season_radar.json"),
		items: make(map[string]SeasonRadarItem),
		tmdb:  tmdb,
	}
	s.load()
	return s
}

// SetTMDB 注入 TMDB 客户端（主程序里雷达先于 TMDB 客户端创建）。
func (s *SeasonRadarService) SetTMDB(tmdb *TMDBClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tmdb = tmdb
}

func (s *SeasonRadarService) SetNotifier(fn func(userID int64, tmdbID int, title string, season TVSeason) bool) {
	s.notify = fn
}

func (s *SeasonRadarService) SetEnabled(fn func(userID int64) bool) {
	s.enabled = fn
}

func radarKey(userID int64, tmdbID int) string { return fmt.Sprintf("%d:%d", userID, tmdbID) }

func (s *SeasonRadarService) tmdbClient() *TMDBClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tmdb
}

// TrackTV 查询并记录当前已播季数，首次建立基线，不会把旧季误报为新季。
func (s *SeasonRadarService) TrackTV(userID int64, tmdbID int, title string) {
	tmdb := s.tmdbClient()
	if tmdb == nil {
		return
	}
	details, err := tmdb.GetTVDetailsWithSeasons(tmdbID)
	if err != nil || details == nil {
		return
	}
	latest := 0
	now := time.Now()
	for _, season := range details.Seasons {
		if season.SeasonNumber <= latest || season.AirDate == "" {
			continue
		}
		if air, err := time.Parse("2006-01-02", season.AirDate); err == nil && !air.After(now) {
			latest = season.SeasonNumber
		}
	}
	s.Track(userID, tmdbID, title, latest)
}

// Track 记录用户求过的剧集。仅保留季数最多的历史观察值。
func (s *SeasonRadarService) Track(userID int64, tmdbID int, title string, knownSeasons int) {
	if userID == 0 || tmdbID <= 0 || title == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := radarKey(userID, tmdbID)
	old, exists := s.items[key]
	if exists && knownSeasons < old.KnownSeasons {
		knownSeasons = old.KnownSeasons
	}
	s.items[key] = SeasonRadarItem{
		UserID: userID, TmdbID: tmdbID, Title: title,
		KnownSeasons: knownSeasons, LastChecked: old.LastChecked,
	}
	s.saveLocked()
}

// Scan 查询所有追踪剧集。首次扫描只建立基线，不给用户补发旧季通知。
// 每次最多处理 100 条，避免 TMDB 故障时占满调度协程。
func (s *SeasonRadarService) Scan() {
	tmdb := s.tmdbClient()
	if tmdb == nil || s.notify == nil {
		return
	}
	s.mu.Lock()
	items := make([]SeasonRadarItem, 0, len(s.items))
	for _, item := range s.items {
		if time.Since(item.LastChecked) < 12*time.Hour {
			continue
		}
		items = append(items, item)
		if len(items) >= 100 {
			break
		}
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].LastChecked.Before(items[j].LastChecked) })

	for _, item := range items {
		details, err := tmdb.GetTVDetailsWithSeasons(item.TmdbID)
		if err != nil || details == nil {
			logger.Info("[SeasonRadar] 查询失败: tmdb=%d err=%v", item.TmdbID, err)
			continue
		}
		latest := item.KnownSeasons
		for _, season := range details.Seasons {
			if season.SeasonNumber > latest {
				latest = season.SeasonNumber
			}
		}
		newSeasons := make([]TVSeason, 0)
		for _, season := range details.Seasons {
			if season.SeasonNumber > item.KnownSeasons && season.SeasonNumber > 0 && season.AirDate != "" {
				if air, err := time.Parse("2006-01-02", season.AirDate); err == nil && !air.After(time.Now()) {
					newSeasons = append(newSeasons, season)
				}
			}
		}
		if s.enabled == nil || s.enabled(item.UserID) {
			// 按季顺序通知；中间某一条发送失败就停住，下一轮从失败季重试，
			// 不跳过用户没收到的季数。
			for _, season := range newSeasons {
				if !s.notify(item.UserID, item.TmdbID, item.Title, season) {
					break
				}
				item.KnownSeasons = season.SeasonNumber
			}
		} else {
			// 用户关闭时仍推进基线，重新打开后只接收未来新季，不补发历史通知。
			item.KnownSeasons = latest
		}
		item.LastChecked = time.Now()
		s.mu.Lock()
		s.items[radarKey(item.UserID, item.TmdbID)] = item
		s.saveLocked()
		s.mu.Unlock()
	}
}

func (s *SeasonRadarService) load() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	var items map[string]SeasonRadarItem
	if err := json.Unmarshal(data, &items); err == nil && items != nil {
		s.items = items
	}
}

func (s *SeasonRadarService) saveLocked() {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return
	}
	if err := atomicWriteFile(s.file, data, 0o644); err != nil {
		logger.Info("[SeasonRadar] 保存失败: %v", err)
	}
}
