package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
)

// PushNotificationSystem handles engaging push notifications
type PushNotificationSystem struct {
	enabled            bool
	lastNotification   map[int64]time.Time
	notificationMutex  sync.RWMutex
	cooldown           time.Duration
	maxDailyPerUser    int
	dailyCount         map[int64]int
	dailyCountMutex    sync.RWMutex
	lastResetDate      string
}

var pushNotifySys *PushNotificationSystem

// InitPushNotificationSystem initializes the push notification system
func InitPushNotificationSystem() {
	pushNotifySys = &PushNotificationSystem{
		enabled:          os.Getenv("PUSH_NOTIFICATIONS") != "false",
		lastNotification: make(map[int64]time.Time),
		cooldown:         4 * time.Hour,
		maxDailyPerUser:  3,
		dailyCount:       make(map[int64]int),
		lastResetDate:    time.Now().Format("2006-01-02"),
	}

	// Start random notification worker
	if pushNotifySys.enabled {
		go pushNotifySys.notificationWorker()
		go pushNotifySys.dailyCountReset()
	}

	log.Println("PushNotificationSystem initialized")
}

// NotificationType represents types of engaging notifications
type NotificationType string

const (
	NotifyNewContent    NotificationType = "new_content"
	NotifyTrending      NotificationType = "trending"
	NotifyReminder      NotificationType = "reminder"
	NotifyAchievement   NotificationType = "achievement"
	NotifyRecommend     NotificationType = "recommend"
	NotifyRandom        NotificationType = "random"
)

// EngagingNotification represents a notification designed to bring users back
type EngagingNotification struct {
	Type        NotificationType
	Title       string
	Message     string
	ActionText  string
	ActionData  string
	Priority    int // 1-10
	CooldownMin int // Minimum minutes between same type
}

// NotificationTemplate represents a reusable notification template
type NotificationTemplate struct {
	Type        NotificationType
	Title       string
	Message     string
	ActionText  string
	ActionData  string
	Priority    int
	MinInactive float64 // Minimum inactive hours
	MaxInactive float64 // Maximum inactive hours (0 = no max)
}

var notificationTemplates = []NotificationTemplate{
	// 1-2 days inactive
	{
		Type:        NotifyReminder,
		Title:       "想看新内容了吗？",
		Message:     "🔥 最近有新的热门内容入库了，来看看吧！",
		ActionText:  "🔍 搜索内容",
		ActionData:  "action_search",
		Priority:    5,
		MinInactive: 24,
		MaxInactive: 48,
	},
	// 2-3 days inactive
	{
		Type:        NotifyReminder,
		Title:       "好久不见",
		Message:     "📺 好久不见了！新剧集等你来追~",
		ActionText:  "🚀 开始探索",
		ActionData:  "action_search",
		Priority:    7,
		MinInactive: 48,
		MaxInactive: 72,
	},
	// 3+ days inactive
	{
		Type:        NotifyReminder,
		Title:       "回来吧！",
		Message:     "🎁 签到奖励在等你哦！",
		ActionText:  "💪 回到战斗",
		ActionData:  "engage_daily",
		Priority:    9,
		MinInactive: 72,
		MaxInactive: 0,
	},
	// Random fun notifications
	{
		Type:        NotifyRandom,
		Title:       "你知道吗？",
		Message:     "今天是个看剧的好日子~",
		ActionText:  "🎬 看看推荐",
		ActionData:  "engage_recommend",
		Priority:    3,
		MinInactive: 6,
		MaxInactive: 0,
	},
	{
		Type:        NotifyRandom,
		Title:       "放松一下",
		Message:     "工作累了？来看部剧休息一下吧~",
		ActionText:  "☕ 来看看",
		ActionData:  "action_search",
		Priority:    3,
		MinInactive: 6,
		MaxInactive: 0,
	},
}

// GetEngagingNotifications returns notifications designed to re-engage users
func GetEngagingNotifications(telegramID int64, lastActive time.Time) []EngagingNotification {
	inactiveHours := time.Since(lastActive).Hours()

	notifications := []EngagingNotification{}

	// Find matching templates
	for _, template := range notificationTemplates {
		if inactiveHours >= template.MinInactive &&
			(template.MaxInactive == 0 || inactiveHours < template.MaxInactive) {

			// Add some variety
			if template.Type == NotifyRandom && rand.Float64() > 0.3 {
				continue // Only include 30% of random notifications
			}

			notifications = append(notifications, EngagingNotification{
				Type:       template.Type,
				Title:      template.Title,
				Message:    template.Message,
				ActionText: template.ActionText,
				ActionData: template.ActionData,
				Priority:   template.Priority,
			})
		}
	}

	// Always add recommendation for users inactive > 6 hours
	if inactiveHours > 6 {
		recommendations := []string{
			"为你推荐了一部新内容！",
			"发现了你可能喜欢的电影！",
			"有人刚刚请求了超好看的内容！",
		}
		notifications = append(notifications, EngagingNotification{
			Type:       NotifyRecommend,
			Title:      "个性化推荐",
			Message:    recommendations[rand.Intn(len(recommendations))],
			ActionText: "🎯 查看推荐",
			ActionData: "engage_recommend",
			Priority:   6,
		})
	}

	return notifications
}

// CanSendNotification checks if notification can be sent
func (p *PushNotificationSystem) CanSendNotification(telegramID int64) bool {
	p.notificationMutex.RLock()
	last, exists := p.lastNotification[telegramID]
	p.notificationMutex.RUnlock()

	// Check cooldown
	if exists && time.Since(last) < p.cooldown {
		return false
	}

	// Check daily limit
	p.dailyCountMutex.RLock()
	count := p.dailyCount[telegramID]
	p.dailyCountMutex.RUnlock()

	if count >= p.maxDailyPerUser {
		return false
	}

	return true
}

// MarkNotificationSent marks that a notification was sent
func (p *PushNotificationSystem) MarkNotificationSent(telegramID int64) {
	p.notificationMutex.Lock()
	p.lastNotification[telegramID] = time.Now()
	p.notificationMutex.Unlock()

	p.dailyCountMutex.Lock()
	p.dailyCount[telegramID]++
	p.dailyCountMutex.Unlock()
}

// notificationWorker sends periodic notifications to inactive users
func (p *PushNotificationSystem) notificationWorker() {
	ticker := time.NewTicker(2 * time.Hour)
	defer ticker.Stop()

	// Wait a bit before starting
	time.Sleep(30 * time.Minute)

	for range ticker.C {
		p.checkInactiveUsers()
	}
}

// dailyCountReset resets daily counts at midnight
func (p *PushNotificationSystem) dailyCountReset() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		today := time.Now().Format("2006-01-02")
		if today != p.lastResetDate {
			p.dailyCountMutex.Lock()
			p.dailyCount = make(map[int64]int)
			p.lastResetDate = today
			p.dailyCountMutex.Unlock()
			log.Println("PushNotify: Reset daily notification counts")
		}
	}
}

// checkInactiveUsers checks for inactive users and sends notifications
func (p *PushNotificationSystem) checkInactiveUsers() {
	if engagementSys == nil || !p.enabled {
		return
	}

	// Get users who haven't been active in a while
	engagementSys.dataMutex.RLock()
	var inactiveUsers []int64
	cutoff := time.Now().Add(-24 * time.Hour)

	for id, user := range engagementSys.userData {
		if user.LastActive.Before(cutoff) {
			inactiveUsers = append(inactiveUsers, id)
		}
	}
	engagementSys.dataMutex.RUnlock()

	// Sample a few users (don't spam everyone)
	const maxSamples = 5
	if len(inactiveUsers) > maxSamples {
		// Shuffle and pick
		rand.Shuffle(len(inactiveUsers), func(i, j int) {
			inactiveUsers[i], inactiveUsers[j] = inactiveUsers[j], inactiveUsers[i]
		})
		inactiveUsers = inactiveUsers[:maxSamples]
	}

	for _, userID := range inactiveUsers {
		if !p.CanSendNotification(userID) {
			continue
		}

		notifications := GetEngagingNotifications(userID, engagementSys.GetUserData(userID).LastActive)

		// Send highest priority notification
		if len(notifications) > 0 {
			highestPriority := notifications[0]
			for _, n := range notifications {
				if n.Priority > highestPriority.Priority {
					highestPriority = n
				}
			}

			msg := fmt.Sprintf("%s\n\n%s", highestPriority.Title, highestPriority.Message)

			// Add action button
			keyboard := &TelegramInlineKeyboard{
				InlineKeyboard: [][]map[string]string{
					{
						{"text": highestPriority.ActionText, "callback_data": highestPriority.ActionData},
					},
				},
			}

			sendPrivateMessage(userID, msg, keyboard)
			p.MarkNotificationSent(userID)

			log.Printf("PushNotify: Sent re-engagement notification to user %d", userID)
		}
	}
}

// GetRecoveryFlow returns recovery flow for inactive users
func GetRecoveryFlow(inactiveHours float64) (string, *TelegramInlineKeyboard) {
	var msg string
	var keyboard *TelegramInlineKeyboard

	if inactiveHours < 24 {
		msg = "👋 *欢迎回来！*\n\n今天想看点什么？"
		keyboard = &TelegramInlineKeyboard{
			InlineKeyboard: [][]map[string]string{
				{
					{"text": "🔍 搜索", "callback_data": "action_search"},
					{"text": "🎯 推荐", "callback_data": "engage_recommend"},
				},
				{
					{"text": "🎁 每日签到", "callback_data": "engage_daily"},
				},
			},
		}
	} else if inactiveHours < 72 {
		msg = "🎉 *好久不见了！*\n\n"
		msg += "我们想你了！\n\n"
		msg += "在你离开的这段时间：\n"
		msg += "• 📺 新内容持续更新\n"
		msg += "• 🔥 热门内容等你发现\n"
		msg += "• 🎁 签到奖励待领取\n\n"
		msg += "要看看有什么新东西吗？"
		keyboard = &TelegramInlineKeyboard{
			InlineKeyboard: [][]map[string]string{
				{
					{"text": "🚀 看看新内容", "callback_data": "action_search"},
				},
				{
					{"text": "🏆 查看排行", "callback_data": "engage_leaderboard"},
					{"text": "🎯 我的进度", "callback_data": "engage_profile"},
				},
			},
		}
	} else {
		msg = "🌟 *欢迎回家！*\n\n"
		msg += "真的很想你！\n\n"
		msg += "给你准备了一份回归礼包：\n"
		msg += "• 🎁 2倍签到奖励\n"
		msg += "• ⭐ 额外经验加成\n"
		msg += "• 🎯 专属推荐\n\n"
		msg += "要开始了吗？"
		keyboard = &TelegramInlineKeyboard{
			InlineKeyboard: [][]map[string]string{
				{
					{"text": "🎁 领取礼包", "callback_data": "engage_daily"},
				},
				{
					{"text": "🔥 开始探索", "callback_data": "action_search"},
				},
				{
					{"text": "📊 我的成就", "callback_data": "engage_badges"},
				},
			},
		}
	}

	return msg, keyboard
}

// GetWelcomeBackMessage returns a personalized welcome back message
func GetWelcomeBackMessage(telegramID int64, username string) (string, *TelegramInlineKeyboard) {
	if engagementSys == nil {
		msg := fmt.Sprintf("👋 欢迎回来，%s！", username)
		return msg, nil
	}

	user := engagementSys.GetUserData(telegramID)
	inactiveHours := time.Since(user.LastActive).Hours()

	// Award returning user bonus
	if inactiveHours > 24 {
		engagementSys.RecordActivity(telegramID, "login", 10)
	}

	msg, keyboard := GetRecoveryFlow(inactiveHours)

	// Add greeting at top
	greeting := fmt.Sprintf("👋 *欢迎回来，%s！*\n\n", username)
	msg = greeting + msg

	return msg, keyboard
}
