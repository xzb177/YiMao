package services

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// Scheduler handles periodic tasks like daily recommendations
type Scheduler struct {
	notification *NotificationService
	moviepilot   *MoviePilotClient
	tmdb         *TMDBClient // 每日推荐数据源：改用 TMDB 热门接口
	adminService *AdminService
	userMapping  UserMappingStore
	telegram     *TelegramClient // 群组推送用
	chatID       int64           // 群组 chat ID（>0 时推送到群）
	stopCh       chan struct{}
	stopOnce     sync.Once

	// mu guards the mutable config fields below (accessed from both the
	// run() goroutine and external setters).
	mu sync.RWMutex
	// Daily recommendation time (hour, minute)
	dailyHour   int
	dailyMinute int
	enabled     bool
}

// NewScheduler creates a new scheduler
func NewScheduler(
	notification *NotificationService,
	moviepilot *MoviePilotClient,
	tmdb *TMDBClient,
	adminService *AdminService,
	userMapping UserMappingStore,
	telegram *TelegramClient,
	chatID int64,
) *Scheduler {
	return &Scheduler{
		notification: notification,
		moviepilot:   moviepilot,
		tmdb:         tmdb,
		adminService: adminService,
		userMapping:  userMapping,
		telegram:     telegram,
		chatID:       chatID,
		stopCh:       make(chan struct{}),
		dailyHour:    9, // Default 9 AM
		dailyMinute:  0, // Default 0 minutes
		enabled:      true,
	}
}

// SetDailyTime sets the time for daily recommendations
func (s *Scheduler) SetDailyTime(hour, minute int) {
	s.mu.Lock()
	s.dailyHour = hour
	s.dailyMinute = minute
	s.mu.Unlock()
	logger.Info("[Scheduler] Daily recommendation time set to %02d:%02d", hour, minute)
}

// SetEnabled enables or disables the scheduler
func (s *Scheduler) SetEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
	if enabled {
		logger.Info("[Scheduler] Scheduler enabled")
	} else {
		logger.Info("[Scheduler] Scheduler disabled")
	}
}

// isEnabled returns whether the scheduler is currently enabled.
func (s *Scheduler) isEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// dailyTime returns the configured daily run hour and minute.
func (s *Scheduler) dailyTime() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dailyHour, s.dailyMinute
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[Scheduler] run panic: %v", r)
			}
		}()
		s.run()
	}()
	h, m := s.dailyTime()
	logger.Info("[Scheduler] Started (daily recommendation at %02d:%02d)", h, m)
}

// Stop stops the scheduler. Safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		logger.Info("[Scheduler] Stopped")
	})
}

// run is the main scheduler loop
func (s *Scheduler) run() {
	// Calculate first run time
	nextRun := s.nextDailyRun()

	for {
		select {
		case <-s.stopCh:
			return
		case <-time.After(time.Until(nextRun)):
			if s.isEnabled() {
				s.sendDailyRecommendations()
			}
			nextRun = s.nextDailyRun()
		}
	}
}

// nextDailyRun calculates the next daily run time
func (s *Scheduler) nextDailyRun() time.Time {
	hour, minute := s.dailyTime()
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	// If time has passed today, schedule for tomorrow
	if next.Before(now) || next.Equal(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}

// fetchTrendingMovies 从 TMDB 获取本周热门电影并转换为 []SearchResult，
// 以复用现有的 shuffleAndPick / TrendingResultItem 转换逻辑。
// 当未配置 TMDB client 或请求失败时，返回 error 由调用方优雅降级（跳过本次推荐）。
func (s *Scheduler) fetchTrendingMovies() ([]SearchResult, error) {
	if s.tmdb == nil {
		return nil, fmt.Errorf("TMDB client 未初始化，跳过本次推荐")
	}

	// 使用 TMDB 真实端点 GET /3/trending/movie/week（带 api_key + language=zh-CN）
	trending, err := s.tmdb.GetTrendingMovies("week")
	if err != nil {
		return nil, fmt.Errorf("获取 TMDB 热门电影失败: %w", err)
	}

	// 将 TMDB 热门结果转换为内部统一的 SearchResult 结构
	results := make([]SearchResult, 0, len(trending.Results))
	for _, m := range trending.Results {
		results = append(results, SearchResult{
			ID:       m.ID,
			Title:    m.GetTitle(),
			Year:     FlexibleYear(m.GetYear()),
			Type:     string(MediaTypeMovie),
			Poster:   s.tmdb.GetPosterURL(m.PosterPath), // 拼接完整 TMDB 海报 URL
			Rating:   m.VoteAverage,
			Overview: m.Overview,
		})
	}
	return results, nil
}

// sendDailyRecommendations sends daily recommendations to all users
func (s *Scheduler) sendDailyRecommendations() {
	logger.Info("[Scheduler] Sending daily recommendations...")

	// 改用 TMDB 获取热门电影（MoviePilot 没有 trending 端点）
	results, err := s.fetchTrendingMovies()
	if err != nil {
		// 优雅降级：记日志跳过本次推荐，不 panic、不发空消息
		logger.Info("[Scheduler] Failed to get trending movies: %v", err)
		return
	}

	if len(results) == 0 {
		logger.Info("[Scheduler] No trending movies found")
		return
	}

	// Shuffle and pick top recommendations
	movies := s.shuffleAndPick(results, 5)

	// Get all users to notify
	userIDs := s.getActiveUsers()
	if len(userIDs) == 0 {
		logger.Info("[Scheduler] No active users to notify")
		return
	}

	// Convert to TrendingResultItem
	items := make([]TrendingResultItem, len(movies))
	for i, m := range movies {
		items[i] = TrendingResultItem{
			ID:     m.ID,
			Title:  m.Title,
			Year:   m.Year.Int(),
			Type:   m.Type,
			Poster: m.Poster,
		}
	}

	// Send notifications to individual users
	if err := s.notification.SendDailyRecommendation(userIDs, items); err != nil {
		logger.Info("[Scheduler] Failed to send daily recommendations: %v", err)
	} else {
		logger.Info("[Scheduler] Sent daily recommendations to %d users", len(userIDs))
	}

	// 推送到群组
	if s.chatID != 0 && s.telegram != nil {
		if err := s.sendGroupRecommendation(items); err != nil {
			logger.Info("[Scheduler] Failed to send group recommendation: %v", err)
		} else {
			logger.Info("[Scheduler] Sent daily recommendation to group %d", s.chatID)
		}
	}
}

// getActiveUsers returns a list of all user IDs who should receive daily recommendations.
// Includes all mapped Telegram users (admins + regular users).
func (s *Scheduler) getActiveUsers() []int64 {
	users := s.userMapping.GetAllTelegramUsers()
	if len(users) == 0 {
		// Fallback to admins if no users are mapped yet
		return s.adminService.GetAdminIDs()
	}
	return users
}

// shuffleAndPick shuffles the results and picks n items
func (s *Scheduler) shuffleAndPick(results []SearchResult, n int) []SearchResult {
	if len(results) <= n {
		return results
	}

	// Create a copy and shuffle
	shuffled := make([]SearchResult, len(results))
	copy(shuffled, results)

	// Note: Go 1.20+ automatically seeds the global random generator
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:n]
}

// SendTestRecommendation sends a test recommendation immediately
func (s *Scheduler) SendTestRecommendation(userID int64) error {
	// 改用 TMDB 获取热门电影
	movies, err := s.fetchTrendingMovies()
	if err != nil {
		return err
	}

	if len(movies) == 0 {
		return nil
	}

	// Pick top 3
	if len(movies) > 3 {
		movies = movies[:3]
	}

	// Convert to TrendingResultItem
	items := make([]TrendingResultItem, len(movies))
	for i, m := range movies {
		items[i] = TrendingResultItem{
			ID:     m.ID,
			Title:  m.Title,
			Year:   m.Year.Int(),
			Type:   m.Type,
			Poster: m.Poster,
		}
	}

	return s.notification.SendDailyRecommendation([]int64{userID}, items)
}

// sendGroupRecommendation sends daily recommendation to the group chat
func (s *Scheduler) sendGroupRecommendation(movies []TrendingResultItem) error {
	msg := NewMessageBuilder()
	msg.Bold("🎬 每日推荐").Newline()
	msg.Newline()

	for i, movie := range movies {
		year := ""
		if movie.Year > 0 {
			year = fmt.Sprintf(" (%d)", movie.Year)
		}
		msg.Textf("%d. %s%s", i+1, movie.Title, year).Newline()
	}

	msg.Newline()
	msg.Italic("💬 私聊机器人可搜索和求片")

	_, err := s.telegram.SendMessage(s.chatID, msg.Build(), "HTML", nil)
	return err
}
