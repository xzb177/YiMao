package services

import (
	"encoding/json"
	"fmt"
	"github.com/xzb177/yimao/internal/richmessage"
	"github.com/xzb177/yimao/pkg/logger"
	"os"
	"sync"
	"time"
)

// NotificationService handles user notifications for request status updates
type NotificationService struct {
	telegram       *TelegramClient
	moviepilot     *MoviePilotClient
	userMapping    UserMappingStore
	notifyFile     string
	knownUsers     map[int64]int64 // telegramID -> moviepilotID
	review         *ReviewService  // optional: resolve MoviePilot subscription/request ID back to Telegram user
	mu             sync.RWMutex
	processMu      sync.Mutex // serializes notification read-modify-write operations
	workerMu       sync.Mutex
	workerStop     chan struct{}
	workerDone     chan struct{}
	workerSignal   chan struct{}
	workerStopping bool
	sendStatus     func(int64, string) error
	resolveUser    func(int) int64
	// NotifyEnabled 检查用户是否开启了某类通知（由 main 注入）。
	// 参数：userID, notifyKey。返回 true 表示允许发送。
	NotifyEnabled func(userID int64, notifyKey string) bool
}

// NewNotificationService creates a new notification service
func NewNotificationService(telegram *TelegramClient, moviepilot *MoviePilotClient, userMapping UserMappingStore, review *ReviewService, dataDir string) *NotificationService {
	notifyFile := dataDir + "/notifications.json"

	return &NotificationService{
		telegram:    telegram,
		moviepilot:  moviepilot,
		userMapping: userMapping,
		review:      review,
		notifyFile:  notifyFile,
		knownUsers:  make(map[int64]int64),
	}
}

// StatusUpdate represents a status update to send to users
type StatusUpdate struct {
	RequestID    int       `json:"request_id"`
	MediaTitle   string    `json:"media_title"`
	MediaYear    string    `json:"media_year"`
	MediaType    string    `json:"media_type"` // movie or tv
	OldState     string    `json:"old_state"`
	NewState     string    `json:"new_state"`
	Username     string    `json:"username,omitempty"`
	SavePath     string    `json:"save_path,omitempty"`
	Percent      int       `json:"percent,omitempty"`
	CurrentEp    int       `json:"current_episode,omitempty"`
	TotalEp      int       `json:"total_episode,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	Notified     bool      `json:"notified"`
	Revision     uint64    `json:"revision"`
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

	return atomicWriteFile(s.notifyFile, data, 0600)
}

// NotifyStatusUpdate persists a pending update and wakes the single worker.
func (s *NotificationService) NotifyStatusUpdate(requestID int, status *SubscribeStatus) error {
	s.processMu.Lock()
	updates, err := s.LoadStatusUpdates()
	if err != nil {
		s.processMu.Unlock()
		return fmt.Errorf("load status updates: %w", err)
	}

	now := time.Now()
	update := StatusUpdate{
		RequestID:    requestID,
		MediaTitle:   status.Name,
		MediaYear:    status.Year,
		MediaType:    status.Type,
		NewState:     status.State,
		SavePath:     status.SavePath,
		Percent:      status.Percent,
		CurrentEp:    status.CurrentEpisode,
		TotalEp:      status.TotalEpisode,
		ErrorMessage: status.ErrorMessage,
		Timestamp:    now,
		Revision:     1,
	}
	found := false
	for i, current := range updates {
		if current.RequestID != requestID {
			continue
		}
		if sameStatusUpdate(current, update) {
			s.processMu.Unlock()
			s.signalWorker()
			return nil
		}
		update.OldState = current.NewState
		update.Revision = current.Revision + 1
		updates[i] = update
		found = true
		break
	}
	if !found {
		updates = append(updates, update)
	}
	if err := s.SaveStatusUpdates(updates); err != nil {
		s.processMu.Unlock()
		return fmt.Errorf("save status updates: %w", err)
	}
	s.processMu.Unlock()
	s.signalWorker()
	return nil
}

func sameStatusUpdate(current, next StatusUpdate) bool {
	return current.MediaTitle == next.MediaTitle &&
		current.MediaYear == next.MediaYear &&
		current.MediaType == next.MediaType &&
		current.NewState == next.NewState &&
		current.SavePath == next.SavePath &&
		current.Percent == next.Percent &&
		current.CurrentEp == next.CurrentEp &&
		current.TotalEp == next.TotalEp &&
		current.ErrorMessage == next.ErrorMessage
}

func (s *NotificationService) signalWorker() {
	s.workerMu.Lock()
	signal := s.workerSignal
	s.workerMu.Unlock()
	if signal != nil {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
}

// findUserForRequest finds the Telegram user ID for a request
func (s *NotificationService) findUserForRequest(requestID int) int64 {
	if requestID <= 0 {
		return 0
	}

	// Prefer ReviewService because approved reviews persist the MoviePilot subscription ID.
	if s.review != nil {
		s.review.mu.RLock()
		defer s.review.mu.RUnlock()
		for _, review := range s.review.reviews {
			if review == nil {
				continue
			}
			if review.SubscriptionID == requestID {
				return review.TelegramID
			}
		}
	}

	// Fallback: some MoviePilot status payloads include username. Resolve it via UserMapping if available.
	if s.moviepilot != nil && s.userMapping != nil {
		status, err := s.moviepilot.GetSubscriptionStatus(requestID)
		if err == nil && status != nil && status.Username != "" {
			if telegramID, ok := s.userMapping.GetTelegramIDByMoviePilotUsername(status.Username); ok {
				return telegramID
			}
		}
	}

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
	// 检查用户是否开启了下载通知
	if s.NotifyEnabled != nil && !s.NotifyEnabled(telegramID, NotifyDownload) {
		return nil
	}

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

	// Build Rich Message
	rmMovies := make([]richmessage.RecommendMovie, 0, count)
	for _, m := range movies[:count] {
		rmMovies = append(rmMovies, richmessage.RecommendMovie{Title: m.Title, Year: m.Year})
	}
	richMsg := richmessage.BuildDailyRecommendCard(rmMovies)

	// Build plain text fallback
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
	plainText := msg.Build()

	// Send to all users
	for _, telegramID := range telegramIDs {
		// 检查用户是否开启了每日推荐
		if s.NotifyEnabled != nil && !s.NotifyEnabled(telegramID, NotifyRecommend) {
			continue
		}
		// Try Rich Message, fall back to plain text
		if _, err := s.telegram.SendRichMessage(telegramID, richMsg.Markdown, nil); err != nil {
			logger.Info("[Notification] Rich Message failed for %d: %v, falling back to plain text", telegramID, err)
			if _, err2 := s.telegram.SendMessage(telegramID, plainText, "HTML", nil); err2 != nil {
				logger.Info("[Notification] Failed to send daily recommendation to %d: %v", telegramID, err2)
			}
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

// StartNotificationWorker starts the single background delivery worker.
func (s *NotificationService) StartNotificationWorker() {
	s.workerMu.Lock()
	if s.workerStop != nil {
		s.workerMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	signal := make(chan struct{}, 1)
	s.workerStop, s.workerDone, s.workerSignal = stop, done, signal
	s.workerMu.Unlock()

	go s.notificationWorker(stop, done, signal)
	s.signalWorker()
}

// StopNotificationWorker stops the worker and may be called repeatedly.
func (s *NotificationService) StopNotificationWorker() {
	s.workerMu.Lock()
	stop, done := s.workerStop, s.workerDone
	if stop == nil {
		s.workerMu.Unlock()
		return
	}
	if !s.workerStopping {
		close(stop)
		s.workerStopping = true
	}
	s.workerMu.Unlock()
	<-done
	s.workerMu.Lock()
	if s.workerDone == done {
		s.workerStop, s.workerDone, s.workerSignal = nil, nil, nil
		s.workerStopping = false
	}
	s.workerMu.Unlock()
}

func (s *NotificationService) notificationWorker(stop <-chan struct{}, done chan<- struct{}, signal <-chan struct{}) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			logger.Info("[NotificationService] Panic in notification worker: %v", r)
		}
	}()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-signal:
			s.processPendingNotifications()
		case <-ticker.C:
			s.checkStatusUpdates()
			s.processPendingNotifications()
		}
	}
}

func (s *NotificationService) processPendingNotifications() {
	for {
		s.processMu.Lock()
		updates, err := s.LoadStatusUpdates()
		if err != nil {
			s.processMu.Unlock()
			logger.Info("[Notification] Failed to load status updates: %v", err)
			return
		}
		var pending *StatusUpdate
		for i := range updates {
			if !updates[i].Notified {
				pendingUpdate := updates[i]
				pending = &pendingUpdate
				break
			}
		}
		s.processMu.Unlock()
		if pending == nil {
			return
		}

		var telegramID int64
		if s.resolveUser != nil {
			telegramID = s.resolveUser(pending.RequestID)
		} else {
			telegramID = s.findUserForRequest(pending.RequestID)
		}
		if telegramID == 0 {
			return
		}
		shouldMark := s.NotifyEnabled != nil && !s.NotifyEnabled(telegramID, NotifyDownload)
		if !shouldMark {
			message := s.formatStatusMessage(*pending)
			switch {
			case s.sendStatus != nil:
				err = s.sendStatus(telegramID, message)
			case s.telegram == nil:
				err = fmt.Errorf("telegram client is nil")
			default:
				_, err = s.telegram.SendMessage(telegramID, message, "", nil)
			}
			if err != nil {
				logger.Info("[Notification] Failed to send notification to user %d: %v", telegramID, err)
				return
			}
		}
		if err := s.markNotified(pending.RequestID, pending.Revision); err != nil {
			logger.Info("[Notification] Failed to mark notification: %v", err)
			return
		}
	}
}

func (s *NotificationService) markNotified(requestID int, revision uint64) error {
	s.processMu.Lock()
	defer s.processMu.Unlock()
	updates, err := s.LoadStatusUpdates()
	if err != nil {
		return err
	}
	for i := range updates {
		if updates[i].RequestID == requestID && updates[i].Revision == revision {
			updates[i].Notified = true
			return s.SaveStatusUpdates(updates)
		}
	}
	return nil
}

// checkStatusUpdates polls for status changes on tracked subscribed items.
func (s *NotificationService) checkStatusUpdates() {
	s.processMu.Lock()
	updates, err := s.LoadStatusUpdates()
	s.processMu.Unlock()
	if err != nil {
		logger.Info("[Notification] Failed to load status updates: %v", err)
		return
	}
	if len(updates) == 0 || s.moviepilot == nil {
		return
	}
	for i := range updates {
		requestID := updates[i].RequestID
		if requestID <= 0 {
			continue
		}
		status, err := s.moviepilot.GetSubscriptionStatus(requestID)
		if err != nil {
			logger.Info("[Notification] Failed to poll subscription %d: %v", requestID, err)
			continue
		}
		if status == nil || status.State == "" || status.State == updates[i].NewState {
			continue
		}
		if err := s.NotifyStatusUpdate(requestID, status); err != nil {
			logger.Info("[Notification] Failed to save polled status update %d: %v", requestID, err)
		}
	}
}
