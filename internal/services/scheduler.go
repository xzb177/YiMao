package services

import (
	"github.com/xzb177/yimao/pkg/logger"
	"math/rand"
	"time"
)

// Scheduler handles periodic tasks like daily recommendations
type Scheduler struct {
	notification *NotificationService
	moviepilot   *MoviePilotClient
	adminService *AdminService
	userMapping  *UserMappingService
	stopCh       chan struct{}

	// Daily recommendation time (hour, minute)
	dailyHour   int
	dailyMinute int
	enabled     bool
}

// NewScheduler creates a new scheduler
func NewScheduler(
	notification *NotificationService,
	moviepilot *MoviePilotClient,
	adminService *AdminService,
	userMapping *UserMappingService,
) *Scheduler {
	return &Scheduler{
		notification: notification,
		moviepilot:   moviepilot,
		adminService: adminService,
		userMapping:  userMapping,
		stopCh:       make(chan struct{}),
		dailyHour:    9,   // Default 9 AM
		dailyMinute:  0,   // Default 0 minutes
		enabled:      true,
	}
}

// SetDailyTime sets the time for daily recommendations
func (s *Scheduler) SetDailyTime(hour, minute int) {
	s.dailyHour = hour
	s.dailyMinute = minute
	logger.Info("[Scheduler] Daily recommendation time set to %02d:%02d", hour, minute)
}

// SetEnabled enables or disables the scheduler
func (s *Scheduler) SetEnabled(enabled bool) {
	s.enabled = enabled
	if enabled {
		logger.Info("[Scheduler] Scheduler enabled")
	} else {
		logger.Info("[Scheduler] Scheduler disabled")
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	go s.run()
	logger.Info("[Scheduler] Started (daily recommendation at %02d:%02d)", s.dailyHour, s.dailyMinute)
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	close(s.stopCh)
	logger.Info("[Scheduler] Stopped")
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
			if s.enabled {
				s.sendDailyRecommendations()
			}
			nextRun = s.nextDailyRun()
		}
	}
}

// nextDailyRun calculates the next daily run time
func (s *Scheduler) nextDailyRun() time.Time {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), s.dailyHour, s.dailyMinute, 0, 0, now.Location())

	// If time has passed today, schedule for tomorrow
	if next.Before(now) || next.Equal(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}

// sendDailyRecommendations sends daily recommendations to all users
func (s *Scheduler) sendDailyRecommendations() {
	logger.Info("[Scheduler] Sending daily recommendations...")

	// Get trending movies
	trending, err := s.moviepilot.GetTrending(MediaTypeMovie, 1)
	if err != nil {
		logger.Info("[Scheduler] Failed to get trending movies: %v", err)
		return
	}

	if len(trending.Results) == 0 {
		logger.Info("[Scheduler] No trending movies found")
		return
	}

	// Shuffle and pick top recommendations
	movies := s.shuffleAndPick(trending.Results, 5)

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

	// Send notifications
	if err := s.notification.SendDailyRecommendation(userIDs, items); err != nil {
		logger.Info("[Scheduler] Failed to send daily recommendations: %v", err)
	} else {
		logger.Info("[Scheduler] Sent daily recommendations to %d users", len(userIDs))
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
	// Get trending movies
	trending, err := s.moviepilot.GetTrending(MediaTypeMovie, 1)
	if err != nil {
		return err
	}

	if len(trending.Results) == 0 {
		return nil
	}

	// Pick top 3
	movies := trending.Results
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
