package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// NotificationService handles user notifications for request status updates
type NotificationService struct {
	telegram       *TelegramClient
	moviepilot     *MoviePilotClient
	userMapping    *UserMappingService
	notifyFile     string
	knownUsers     map[int64]int64 // telegramID -> moviepilotID
	mu             sync.RWMutex
}

// NewNotificationService creates a new notification service
func NewNotificationService(telegram *TelegramClient, moviepilot *MoviePilotClient, userMapping *UserMappingService, dataDir string) *NotificationService {
	notifyFile := dataDir + "/notifications.json"

	return &NotificationService{
		telegram:    telegram,
		moviepilot:  moviepilot,
		userMapping: userMapping,
		notifyFile:  notifyFile,
		knownUsers:   make(map[int64]int64),
	}
}

// StatusUpdate represents a status update to send to users
type StatusUpdate struct {
	RequestID    int    `json:"request_id"`
	MediaTitle  string `json:"media_title"`
	MediaYear   string `json:"media_year"`
	MediaType   string `json:"media_type"` // movie or tv
	OldState     string `json:"old_state"`
	NewState     string `json:"new_state"`
	Username    string `json:"username,omitempty"`
	SavePath     string `json:"save_path,omitempty"`
	Percent      int    `json:"percent,omitempty"`
	CurrentEp    int    `json:"current_episode,omitempty"`
	TotalEp      int    `json:"total_episode,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	Notified     bool   `json:"notified"`
}

// LoadStatusUpdates loads pending notifications from file
func (s *NotificationService) LoadStatusUpdates() ([]StatusUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.notifyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []StatusUpdate{}, nil
		}
		return nil, err
	}

	var updates []StatusUpdate
	if err := json.Unmarshal(data, &updates); err != nil {
		return nil, err
	}

	return updates, nil
}

// SaveStatusUpdates saves notifications to file
func (s *NotificationService) SaveStatusUpdates(updates []StatusUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(updates, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.notifyFile, data, 0600)
}

// NotifyStatusUpdate notifies a user about request status change
func (s *NotificationService) NotifyStatusUpdate(requestID int, status *SubscribeStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find all users who should be notified (admin who submitted or the request owner)
	// For now, we'll store notifications and process them periodically

	update := &StatusUpdate{
		RequestID:    status.ID,
		MediaTitle:  status.Name,
		MediaYear:   status.Year,
		OldState:     "",
		NewState:     status.State,
		SavePath:     status.SavePath,
		Percent:      status.Percent,
		CurrentEp:    status.CurrentEpisode,
		TotalEp:      status.TotalEpisode,
		Timestamp:    time.Now(),
		Notified:     false,
	}

	// Load existing updates
	updates, err := s.LoadStatusUpdates()
	if err != nil {
		updates = []StatusUpdate{}
	}

	// Check if this is a new state change
	for i, u := range updates {
		if u.RequestID == requestID {
			if u.NewState != status.State {
				u.OldState = u.NewState
				u.NewState = status.State
				u.Timestamp = time.Now()
				u.Notified = false
			}
			updates[i] = u
			return s.saveAndNotify(updates)
		}
	}

	// New request
	updates = append(updates, *update)
	return s.saveAndNotify(updates)
}

// saveAndNotify saves and sends notifications
func (s *NotificationService) saveAndNotify(updates []StatusUpdate) error {
	// Save to file
	if err := s.SaveStatusUpdates(updates); err != nil {
		log.Printf("[Notification] Failed to save notifications: %v", err)
		return err
	}

	// Send pending notifications
	go s.processNotifications(updates)

	return nil
}

// processNotifications sends pending notifications to users
func (s *NotificationService) processNotifications(updates []StatusUpdate) {
	for i, update := range updates {
		if update.Notified {
			continue
		}

		// Find the user to notify
		telegramID := s.findUserForRequest(update.RequestID)
		if telegramID == 0 {
			continue
		}

		// Send notification
		message := s.formatStatusMessage(update)
		_, err := s.telegram.SendMessage(telegramID, message, "", nil)
		if err != nil {
			log.Printf("[Notification] Failed to send notification to user %d: %v", telegramID, err)
			continue
		}

		// Mark as notified
		updates[i].Notified = true
	}

	s.SaveStatusUpdates(updates)
}

// findUserForRequest finds the Telegram user ID for a request
func (s *NotificationService) findUserForRequest(requestID int) int64 {
	// For now, we'll return a placeholder
	// In production, you would map requests to users
	// This could be enhanced to track who made each request
	return 0
}

// formatStatusMessage formats a status update message
func (s *NotificationService) formatStatusMessage(update StatusUpdate) string {
	msg := NewMessageBuilder()
	msg.Bold("📋 求片状态更新").Newline()
	msg.Newline()

	// Media info
	mediaType := "🎬 电影"
	if update.MediaType == "电视剧" || update.MediaType == "tv" {
		mediaType = "📺 电视剧"
	}

	msg.Textf("%s %s (%s)", mediaType, update.MediaTitle, update.MediaYear).Newline()
	msg.Newline()

	// State change
	if update.OldState != "" {
		msg.Italic(fmt.Sprintf("状态: %s → %s", GetStateText(update.OldState), GetStateText(update.NewState))).Newline()
	} else {
		msg.Italic(fmt.Sprintf("状态: %s", GetStateText(update.NewState))).Newline()
	}

	// Additional info based on state
	switch update.NewState {
	case StateDownloading:
		if update.Percent > 0 {
			msg.Textf("📊 进度: %d%%", update.Percent).Newline()
		}
	case StateCompleted:
		if update.SavePath != "" {
			msg.Italic("✅ 已下载，可以观看了！").Newline()
		}
	case StateFailed:
		if update.ErrorMessage != "" {
			msg.Textf("❌ 失败原因: %s", update.ErrorMessage).Newline()
		}
	}

	return msg.Build()
}

// SendDownloadCompleted sends a notification when media is downloaded
func (s *NotificationService) SendDownloadCompleted(telegramID int64, mediaTitle, mediaYear, mediaType, savePath string) error {
	msg := NewMessageBuilder()
	msg.Bold("🎉 下载完成！").Newline()
	msg.Newline()

	mediaEmoji := "🎬"
	if mediaType == "tv" || mediaType == "电视剧" {
		mediaEmoji = "📺"
	}

	msg.Textf("%s %s (%s)", mediaEmoji, mediaTitle, mediaYear).Newline()
	msg.Italic("🎬 已下载完成，可以观看了！").Newline()

	_, err := s.telegram.SendMessage(telegramID, msg.Build(), "HTML", nil)
	return err
}

// SendDailyRecommendation sends daily recommendation push
func (s *NotificationService) SendDailyRecommendation(telegramIDs []int64, movies []TrendingResultItem) error {
	if len(movies) == 0 {
		return fmt.Errorf("no movies to recommend")
	}

	// Limit to top 5
	count := len(movies)
	if count > 5 {
		count = 5
	}

	msg := NewMessageBuilder()
	msg.Bold("🎬 每日推荐").Newline()
	msg.Italic("💡 今日精选推荐").Newline()
	msg.Newline()

	for i, movie := range movies[:count] {
		year := ""
		if movie.Year > 0 {
			year = fmt.Sprintf(" (%d)", movie.Year)
		}
		msg.Textf("%d. %s%s", i+1, movie.Title, year).Newline()
	}

	msg.Newline()
	msg.Italic("💬 输入 /start 查看更多功能")

	// Send to all users
	for _, telegramID := range telegramIDs {
		if _, err := s.telegram.SendMessage(telegramID, msg.Build(), "HTML", nil); err != nil {
			log.Printf("[Notification] Failed to send daily recommendation to %d: %v", telegramID, err)
		}
	}

	return nil
}

// TrendingResultItem represents a single trending/search result for notifications
type TrendingResultItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
	Type   string `json:"type"`
	Poster string `json:"poster_path"`
}

// StartNotificationWorker starts the background worker to check for status updates
func (s *NotificationService) StartNotificationWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			s.checkStatusUpdates()
		}
	}()
}

// checkStatusUpdates polls for status changes on subscribed items
func (s *NotificationService) checkStatusUpdates() {
	// Get all subscribe requests and check their status
	// This would require maintaining a list of tracked requests
	// For now, this is a placeholder for the implementation
}