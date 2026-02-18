package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

// EngagementSystem handles user engagement and retention features
type EngagementSystem struct {
	// User engagement data
	userData   map[int64]*UserEngagement
	dataMutex  sync.RWMutex

	// Daily challenges
	dailyChallenges  map[string]*DailyChallenge
	challengeMutex   sync.RWMutex

	// Streak system
	streaks    map[int64]*UserStreak
	streakMutex sync.RWMutex

	// Achievement system
	achievements map[int64][]string
	achieveMutex sync.RWMutex

	// Leaderboard cache
	leaderboard      []LeaderboardEntry
	leaderboardTime  time.Time
	leaderboardMutex sync.RWMutex

	storageFile   string
	jellyseerrURL string
	apiKey        string
	saveChan      chan struct{}
}

// UserEngagement tracks user engagement metrics
type UserEngagement struct {
	TelegramID      int64     `json:"telegramId"`
	JoinDate        time.Time `json:"joinDate"`
	LastActive      time.Time `json:"lastActive"`
	TotalRequests   int       `json:"totalRequests"`
	TotalSearches   int       `json:"totalSearches"`
	LoginStreak     int       `json:"loginStreak"`
	LongestStreak   int       `json:"longestStreak"`
	Level           int       `json:"level"`
	Experience      int       `json:"experience"`
	RequestsToday   int       `json:"requestsToday"`
	SearchesToday   int       `json:"searchesToday"`
	FavoriteGenres  []string  `json:"favoriteGenres"`
	Badges          []string  `json:"badges"`
	LastDailyBonus  time.Time `json:"lastDailyBonus"`
	NextBonusTime   time.Time `json:"nextBonusTime"`
	ConsecutiveDays int       `json:"consecutiveDays"`
}

// DailyChallenge represents a daily challenge
type DailyChallenge struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Reward      int       `json:"reward"`
	Progress    int       `json:"progress"`
	Target      int       `json:"target"`
	Completed   bool      `json:"completed"`
	Date        time.Time `json:"date"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// UserStreak tracks user activity streaks
type UserStreak struct {
	TelegramID      int64     `json:"telegramId"`
	CurrentStreak   int       `json:"currentStreak"`
	LongestStreak   int       `json:"longestStreak"`
	LastActiveDate  string    `json:"lastActiveDate"`
	StreakHistory   []string  `json:"streakHistory"`
	NextBonusStreak int       `json:"nextBonusStreak"`
}

// LeaderboardEntry represents a leaderboard entry
type LeaderboardEntry struct {
	Rank        int    `json:"rank"`
	TelegramID  int64  `json:"telegramId"`
	Username    string `json:"username"`
	Score       int    `json:"score"`
	Level       int    `json:"level"`
	DisplayText string `json:"displayText"`
}

// EngagementData stores all engagement data
type EngagementData struct {
	UserData        map[int64]*UserEngagement `json:"userData"`
	DailyChallenges map[string]*DailyChallenge `json:"dailyChallenges"`
	Streaks         map[int64]*UserStreak     `json:"streaks"`
	Achievements    map[int64][]string        `json:"achievements"`
	LastSync        string                    `json:"lastSync"`
}

var engagementSys *EngagementSystem

// InitEngagementSystem initializes the engagement system
func InitEngagementSystem() {
	engagementSys = &EngagementSystem{
		userData:        make(map[int64]*UserEngagement),
		dailyChallenges: make(map[string]*DailyChallenge),
		streaks:         make(map[int64]*UserStreak),
		achievements:    make(map[int64][]string),
		storageFile:     "engagement_data.json",
		jellyseerrURL:   jellyseerrURL,
		apiKey:          os.Getenv("JELLYSEERR_API_KEY"),
		saveChan:        make(chan struct{}, 1),
	}

	engagementSys.load()
	engagementSys.generateDailyChallenges()

	// Start background routines
	go engagementSys.updateStreaks()
	go engagementSys.refreshChallenges()
	go engagementSys.updateLeaderboard()
	go engagementSys.saveWorker()

	log.Println("EngagementSystem initialized")
}

// RecordActivity records user activity
func (e *EngagementSystem) RecordActivity(telegramID int64, activityType string, amount int) {
	e.dataMutex.Lock()
	defer e.dataMutex.Unlock()

	user, exists := e.userData[telegramID]
	if !exists {
		user = &UserEngagement{
			TelegramID:      telegramID,
			JoinDate:        time.Now(),
			LastActive:      time.Now(),
			Level:           1,
			Experience:      0,
			FavoriteGenres:  []string{},
			Badges:          []string{},
			ConsecutiveDays: 1,
		}
		e.userData[telegramID] = user
	}

	now := time.Now()
	user.LastActive = now

	switch activityType {
	case "search":
		user.TotalSearches++
		user.SearchesToday++
		user.Experience += amount
	case "request":
		user.TotalRequests++
		user.RequestsToday++
		user.Experience += amount
	case "login", "bonus":
		user.Experience += amount
	}

	// Level up check
	neededExp := e.expNeededForLevel(user.Level)
	for user.Experience >= neededExp {
		user.Experience -= neededExp
		user.Level++
		neededExp = e.expNeededForLevel(user.Level)
		// Send level up notification asynchronously
		go e.notifyLevelUp(telegramID, user.Level)
	}

	// Trigger save
	e.triggerSave()
}

// GetUserData gets user engagement data
func (e *EngagementSystem) GetUserData(telegramID int64) *UserEngagement {
	e.dataMutex.RLock()
	defer e.dataMutex.RUnlock()

	if user, exists := e.userData[telegramID]; exists {
		return user
	}

	// Return default data
	return &UserEngagement{
		TelegramID: telegramID,
		Level:      1,
		Experience: 0,
		Badges:     []string{},
	}
}

// FormatUserCard formats user profile card
func (e *EngagementSystem) FormatUserCard(telegramID int64, username string) string {
	user := e.GetUserData(telegramID)

	// Calculate progress to next level
	neededExp := e.expNeededForLevel(user.Level)
	progress := int((float64(user.Experience) * 100) / float64(neededExp))
	if progress > 100 {
		progress = 100
	}

	// Get title/rank
	title := e.getTitleForLevel(user.Level)

	msg := fmt.Sprintf("🎴 *%s*\n\n", title)
	msg += fmt.Sprintf("👤 %s\n\n", username)
	msg += fmt.Sprintf("⭐ 等级 %d\n", user.Level)
	msg += fmt.Sprintf("📊 经验: %d/%d (%d%%)\n", user.Experience, neededExp, progress)
	msg += fmt.Sprintf("🔥 连续签到: %d 天\n\n", user.ConsecutiveDays)

	// Stats
	msg += "📈 *统计*\n"
	msg += fmt.Sprintf("📋 总请求: %d\n", user.TotalRequests)
	msg += fmt.Sprintf("🔍 总搜索: %d\n", user.TotalSearches)
	msg += fmt.Sprintf("📅 今天请求: %d\n", user.RequestsToday)
	msg += fmt.Sprintf("🔍 今天搜索: %d\n\n", user.SearchesToday)

	// Badges
	if len(user.Badges) > 0 {
		msg += "🏆 *成就*\n"
		for _, badge := range user.Badges {
			msg += fmt.Sprintf("  %s\n", e.getBadgeEmoji(badge))
		}
	}

	return msg
}

// GetDailyChallenges gets available challenges
func (e *EngagementSystem) GetDailyChallenges(telegramID int64) []*DailyChallenge {
	e.challengeMutex.RLock()
	defer e.challengeMutex.RUnlock()

	challenges := make([]*DailyChallenge, 0, len(e.dailyChallenges)+1)
	for _, ch := range e.dailyChallenges {
		challenges = append(challenges, ch)
	}

	// Add user-specific level challenge
	user := e.GetUserData(telegramID)

	levelChallenge := &DailyChallenge{
		ID:          fmt.Sprintf("level_%d", telegramID),
		Title:       "等级挑战",
		Description: fmt.Sprintf("达到等级 %d", user.Level+1),
		Reward:      user.Level * 50,
		Progress:    user.Experience,
		Target:      e.expNeededForLevel(user.Level),
		Completed:   user.Experience >= e.expNeededForLevel(user.Level),
		Date:        time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	challenges = append(challenges, levelChallenge)

	return challenges
}

// FormatChallenges formats challenges for display
func (e *EngagementSystem) FormatChallenges(telegramID int64) string {
	challenges := e.GetDailyChallenges(telegramID)

	msg := "🎯 *每日挑战*\n\n完成挑战获得奖励！\n\n"

	for _, ch := range challenges {
		status := "⏳"
		if ch.Completed {
			status = "✅"
		}

		msg += fmt.Sprintf("%s *%s*\n", status, ch.Title)
		msg += fmt.Sprintf("   %s\n", ch.Description)

		if ch.Target > 0 {
			progress := (ch.Progress * 100) / ch.Target
			if progress > 100 {
				progress = 100
			}
			msg += fmt.Sprintf("   进度: %d/%d (%d%%)\n", ch.Progress, ch.Target, progress)
		}

		if !ch.Completed {
			msg += fmt.Sprintf("   🎁 奖励: +%d 经验\n", ch.Reward)
		} else {
			msg += fmt.Sprintf("   🎁 已领取\n")
		}
		msg += "\n"
	}

	return msg
}

// ClaimDailyBonus claims daily login bonus
func (e *EngagementSystem) ClaimDailyBonus(telegramID int64) (string, int, error) {
	e.dataMutex.Lock()
	defer e.dataMutex.Unlock()

	user, exists := e.userData[telegramID]
	if !exists {
		user = &UserEngagement{
			TelegramID:      telegramID,
			JoinDate:        time.Now(),
			LastActive:      time.Now(),
			Level:           1,
			Experience:      0,
			Badges:          []string{},
			ConsecutiveDays: 1,
			LastDailyBonus:  time.Now(),
			NextBonusTime:   time.Now().Add(24 * time.Hour),
		}
		e.userData[telegramID] = user
	}

	now := time.Now()

	// Check if bonus is available
	if !user.LastDailyBonus.IsZero() && now.Before(user.NextBonusTime) {
		remaining := time.Until(user.NextBonusTime)
		hours := int(remaining.Hours())
		mins := int(remaining.Minutes()) % 60
		return "", 0, fmt.Errorf("还需要等待 %d 小时 %d 分钟", hours, mins)
	}

	// Calculate bonus based on streak
	baseBonus := 50
	streakBonus := min(user.ConsecutiveDays*10, 150) // Cap streak bonus
	totalBonus := baseBonus + streakBonus

	// Cap the total bonus
	if totalBonus > 200 {
		totalBonus = 200
	}

	// Update user
	user.LastDailyBonus = now
	user.NextBonusTime = now.Add(24 * time.Hour)
	user.Experience += totalBonus

	// Check level up
	neededExp := e.expNeededForLevel(user.Level)
	for user.Experience >= neededExp {
		user.Experience -= neededExp
		user.Level++
		// Send notification asynchronously
		go e.notifyLevelUp(telegramID, user.Level)
		neededExp = e.expNeededForLevel(user.Level)
	}

	e.triggerSave()

	// Award badge for streak
	if user.ConsecutiveDays >= 7 {
		go e.awardBadge(telegramID, "weekly_streak")
	}

	return fmt.Sprintf("🎁 *每日签到奖励*\n\n基础奖励: %d 经验\n连续签到 +%d 经验\n\n共获得: %d 经验！", baseBonus, streakBonus, totalBonus), totalBonus, nil
}

// GetLeaderboard gets the leaderboard
func (e *EngagementSystem) GetLeaderboard(limit int) []LeaderboardEntry {
	e.leaderboardMutex.RLock()
	defer e.leaderboardMutex.RUnlock()

	if limit > 0 && limit < len(e.leaderboard) {
		return e.leaderboard[:limit]
	}
	return e.leaderboard
}

// FormatLeaderboard formats the leaderboard
func (e *EngagementSystem) FormatLeaderboard(limit int) string {
	leaderboard := e.GetLeaderboard(limit)

	if len(leaderboard) == 0 {
		return "🏆 *排行榜*\n\n暂无数据"
	}

	msg := "🏆 *排行榜*\n\n按经验值排名\n\n"

	for i, entry := range leaderboard {
		if i >= 10 {
			break
		}

		medal := ""
		switch i {
		case 0:
			medal = "🥇 "
		case 1:
			medal = "🥈 "
		case 2:
			medal = "🥉 "
		default:
			medal = fmt.Sprintf("%2d ", i+1)
		}

		msg += fmt.Sprintf("%s %s", medal, entry.DisplayText)
		if entry.Level > 1 {
			msg += fmt.Sprintf(" (Lv.%d)", entry.Level)
		}
		msg += "\n"
	}

	return msg
}

// Helper functions

func (e *EngagementSystem) expNeededForLevel(level int) int {
	// Exponential curve: 100 * 1.5^(level-1)
	return int(100 * math.Pow(1.5, float64(level-1)))
}

func (e *EngagementSystem) getTitleForLevel(level int) string {
	titles := []string{
		"入门影迷", "初级影迷", "影迷", "资深影迷", "电影爱好者",
		"剧集达人", "媒体专家", "影视狂人", "收藏家", "大师",
		"传奇", "殿堂级", "神话", "至尊", "不朽",
	}
	if level <= len(titles) {
		return titles[level-1]
	}
	return fmt.Sprintf("王者 Lv.%d", level)
}

func (e *EngagementSystem) getBadgeEmoji(badge string) string {
	badges := map[string]string{
		"first_request":  "🎬 初次请求",
		"first_search":   "🔍 搜索达人",
		"weekly_streak":  "🔥 周常用户",
		"monthly_streak": "💎 月度之星",
		"level_5":        "⭐ 五级达人",
		"level_10":       "🌟 十级专家",
		"level_20":       "✨ 二十级大师",
		"requests_100":   "📋 百次请求",
		"requests_500":   "📚 五百次请求",
		"explorer":       "🗺️ 探索者",
	}
	if emoji, ok := badges[badge]; ok {
		return emoji
	}
	return "🏅 " + badge
}

func (e *EngagementSystem) awardBadge(telegramID int64, badge string) {
	e.achieveMutex.Lock()
	defer e.achieveMutex.Unlock()

	if _, exists := e.achievements[telegramID]; !exists {
		e.achievements[telegramID] = []string{}
	}

	// Check if already has badge
	for _, b := range e.achievements[telegramID] {
		if b == badge {
			return
		}
	}

	e.achievements[telegramID] = append(e.achievements[telegramID], badge)

	// Update user data
	e.dataMutex.Lock()
	if user, exists := e.userData[telegramID]; exists {
		user.Badges = e.achievements[telegramID]
	}
	e.dataMutex.Unlock()

	e.triggerSave()

	// Notify user
	go e.notifyBadgeEarned(telegramID, badge)
}

func (e *EngagementSystem) notifyLevelUp(telegramID int64, level int) {
	title := e.getTitleForLevel(level)
	msg := fmt.Sprintf("🎉 *升级了！*\n\n恭喜达到 **等级 %d**\n新称号: %s\n\n继续加油！", level, title)
	sendPrivateMessage(telegramID, msg, nil)
}

func (e *EngagementSystem) notifyBadgeEarned(telegramID int64, badge string) {
	badgeName := e.getBadgeEmoji(badge)
	msg := fmt.Sprintf("🏆 *获得成就！*\n\n%s\n\n快去炫耀吧！", badgeName)
	sendPrivateMessage(telegramID, msg, nil)
}

// Background tasks

func (e *EngagementSystem) updateStreaks() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		e.checkAndUpdateStreaks()
	}
}

func (e *EngagementSystem) checkAndUpdateStreaks() {
	e.dataMutex.Lock()
	defer e.dataMutex.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.Add(-24 * time.Hour).Format("2006-01-02")

	for _, user := range e.userData {
		lastActive := user.LastActive.Format("2006-01-02")

		if lastActive != today {
			if lastActive == yesterday {
				// Continue streak - already counted
			} else {
				// Reset streak
				user.ConsecutiveDays = 1
			}
		}
	}

	e.triggerSave()
}

func (e *EngagementSystem) generateDailyChallenges() {
	e.challengeMutex.Lock()
	defer e.challengeMutex.Unlock()

	now := time.Now()
	expiresAt := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	e.dailyChallenges = map[string]*DailyChallenge{
		"daily_search_3": {
			ID:          "daily_search_3",
			Title:       "搜索达人",
			Description: "进行 3 次搜索",
			Reward:      30,
			Progress:    0,
			Target:      3,
			Completed:   false,
			Date:        now,
			ExpiresAt:   expiresAt,
		},
		"daily_request_1": {
			ID:          "daily_request_1",
			Title:       "求片新手",
			Description: "发起 1 次请求",
			Reward:      50,
			Progress:    0,
			Target:      1,
			Completed:   false,
			Date:        now,
			ExpiresAt:   expiresAt,
		},
		"daily_login": {
			ID:          "daily_login",
			Title:       "每日签到",
			Description: "每天登录机器人",
			Reward:      20,
			Progress:    0,
			Target:      1,
			Completed:   false,
			Date:        now,
			ExpiresAt:   expiresAt,
		},
	}

	log.Println("EngagementSystem: Generated new daily challenges")
}

func (e *EngagementSystem) refreshChallenges() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		e.generateDailyChallenges()

		// Reset daily counters
		e.dataMutex.Lock()
		for _, user := range e.userData {
			user.RequestsToday = 0
			user.SearchesToday = 0
		}
		e.dataMutex.Unlock()
		e.triggerSave()
	}
}

func (e *EngagementSystem) updateLeaderboard() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		e.rebuildLeaderboard()
	}
}

func (e *EngagementSystem) rebuildLeaderboard() {
	e.dataMutex.RLock()
	defer e.dataMutex.RUnlock()

	// Build leaderboard entries
	type entry struct {
		id    int64
		score int
		level int
		name  string
	}
	entries := []entry{}

	for id, user := range e.userData {
		username := fmt.Sprintf("用户%d", id)
		if userSyncMgr != nil {
			name := userSyncMgr.GetTelegramUsername(id)
			if name != "" {
				username = name
			}
		}

		totalScore := user.Experience + user.TotalRequests*10 + user.TotalSearches*2
		entries = append(entries, entry{
			id:    id,
			score: totalScore,
			level: user.Level,
			name:  username,
		})
	}

	// Sort by score descending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	// Build leaderboard
	leaderboard := make([]LeaderboardEntry, 0, len(entries))
	for i, e := range entries {
		emoji := ""
		switch i {
		case 0:
			emoji = "👑 "
		case 1:
			emoji = "🥈 "
		case 2:
			emoji = "🥉 "
		}

		leaderboard = append(leaderboard, LeaderboardEntry{
			Rank:        i + 1,
			TelegramID:  e.id,
			Username:    e.name,
			Score:       e.score,
			Level:       e.level,
			DisplayText: fmt.Sprintf("%s%s", emoji, e.name),
		})
	}

	e.leaderboardMutex.Lock()
	e.leaderboard = leaderboard
	e.leaderboardTime = time.Now()
	e.leaderboardMutex.Unlock()

	log.Println("EngagementSystem: Updated leaderboard")
}

// Save/Load with debouncing

func (e *EngagementSystem) triggerSave() {
	select {
	case e.saveChan <- struct{}{}:
	default:
		// Save already pending
	}
}

func (e *EngagementSystem) saveWorker() {
	const saveDelay = 5 * time.Second

	for range e.saveChan {
		time.Sleep(saveDelay)
		e.save()
	}
}

func (e *EngagementSystem) save() {
	e.dataMutex.RLock()
	e.challengeMutex.RLock()
	e.achieveMutex.RLock()

	data := EngagementData{
		UserData:        e.userData,
		DailyChallenges: e.dailyChallenges,
		Streaks:         e.streaks,
		Achievements:    e.achievements,
		LastSync:        time.Now().Format(time.RFC3339),
	}

	e.achieveMutex.RUnlock()
	e.challengeMutex.RUnlock()
	e.dataMutex.RUnlock()

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("EngagementSystem: Failed to marshal data: %v", err)
		return
	}

	if err := os.WriteFile(e.storageFile, jsonData, 0644); err != nil {
		log.Printf("EngagementSystem: Failed to save data: %v", err)
	}
}

func (e *EngagementSystem) load() {
	data, err := os.ReadFile(e.storageFile)
	if err != nil {
		log.Printf("EngagementSystem: Data file not found, starting fresh: %v", err)
		return
	}

	var loaded EngagementData
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("EngagementSystem: Failed to load data: %v", err)
		return
	}

	e.userData = loaded.UserData
	e.dailyChallenges = loaded.DailyChallenges
	e.streaks = loaded.Streaks
	e.achievements = loaded.Achievements

	if e.userData == nil {
		e.userData = make(map[int64]*UserEngagement)
	}
	if e.dailyChallenges == nil {
		e.dailyChallenges = make(map[string]*DailyChallenge)
	}
	if e.streaks == nil {
		e.streaks = make(map[int64]*UserStreak)
	}
	if e.achievements == nil {
		e.achievements = make(map[int64][]string)
	}

	log.Printf("EngagementSystem: Loaded %d users, %d challenges", len(e.userData), len(e.dailyChallenges))
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
