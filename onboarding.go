package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// OnboardingManager handles new user onboarding
type OnboardingManager struct {
	completedUsers map[int64]bool
	completedMutex sync.RWMutex

	progress     map[int64]*OnboardingProgress
	progressMutex sync.RWMutex

	storageFile string
}

// OnboardingProgress tracks a user's onboarding progress
type OnboardingProgress struct {
	UserID     int64
	Username   string
	StartedAt  time.Time
	Step       int
	Completed  bool
	LastAction string
	Actions    []string
}

// OnboardingStep represents a step in the onboarding flow
type OnboardingStep struct {
	ID          int
	Title       string
	Message     string
	Buttons     []OnboardingButton
	SkipAllowed bool
}

// OnboardingButton represents a button in onboarding
type OnboardingButton struct {
	Text    string
	Action  string
	Primary bool
}

var onboardingMgr *OnboardingManager

// InitOnboarding initializes the onboarding manager
func InitOnboarding() {
	storageFile := "/root/emby-telegram-bot/onboarding_data.json"

	onboardingMgr = &OnboardingManager{
		completedUsers: make(map[int64]bool),
		progress:       make(map[int64]*OnboardingProgress),
		storageFile:    storageFile,
	}

	// Load existing data
	onboardingMgr.load()

	log.Println("Onboarding manager initialized")
}

// IsOnboardingComplete checks if user has completed onboarding
func (m *OnboardingManager) IsOnboardingComplete(userID int64) bool {
	m.completedMutex.RLock()
	defer m.completedMutex.RUnlock()
	return m.completedUsers[userID]
}

// GetProgress returns user's onboarding progress
func (m *OnboardingManager) GetProgress(userID int64) *OnboardingProgress {
	m.progressMutex.RLock()
	defer m.progressMutex.RUnlock()
	return m.progress[userID]
}

// StartOnboarding starts the onboarding flow for a user
func (m *OnboardingManager) StartOnboarding(userID int64, username string) *OnboardingStep {
	m.progressMutex.Lock()
	defer m.progressMutex.Unlock()

	// Check if already completed
	if m.completedUsers[userID] {
		return nil
	}

	// Create or update progress
	progress := &OnboardingProgress{
		UserID:     userID,
		Username:   username,
		StartedAt:  time.Now(),
		Step:       1,
		Completed:  false,
		LastAction: "started",
		Actions:    []string{"started_onboarding"},
	}

	m.progress[userID] = progress
	m.save()

	return m.getStep(1)
}

// AdvanceProgress advances user to next onboarding step
func (m *OnboardingManager) AdvanceProgress(userID int64, action string) *OnboardingStep {
	m.progressMutex.Lock()
	defer m.progressMutex.Unlock()

	progress, exists := m.progress[userID]
	if !exists {
		return nil
	}

	progress.Actions = append(progress.Actions, action)
	progress.LastAction = action

	// Advance to next step
	nextStep := progress.Step + 1

	// Check if onboarding is complete
	if nextStep > len(m.getAllSteps()) {
		progress.Completed = true
		progress.Step = nextStep

		// Mark as completed
		m.completedMutex.Lock()
		m.completedUsers[userID] = true
		m.completedMutex.Unlock()

		m.save()
		return nil // Onboarding complete
	}

	progress.Step = nextStep
	m.save()

	return m.getStep(nextStep)
}

// CompleteOnboarding marks onboarding as complete
func (m *OnboardingManager) CompleteOnboarding(userID int64) {
	m.progressMutex.Lock()
	defer m.progressMutex.Unlock()

	if progress, exists := m.progress[userID]; exists {
		progress.Completed = true
		progress.Actions = append(progress.Actions, "completed")
	}

	m.completedMutex.Lock()
	m.completedUsers[userID] = true
	m.completedMutex.Unlock()

	m.save()
}

// SkipOnboarding allows user to skip onboarding
func (m *OnboardingManager) SkipOnboarding(userID int64) {
	m.CompleteOnboarding(userID)
}

// ResetOnboarding resets onboarding for a user (for testing)
func (m *OnboardingManager) ResetOnboarding(userID int64) {
	m.progressMutex.Lock()
	defer m.progressMutex.Unlock()

	delete(m.progress, userID)

	m.completedMutex.Lock()
	delete(m.completedUsers, userID)
	m.completedMutex.Unlock()

	m.save()
}

// getStep returns the onboarding step by ID
func (m *OnboardingManager) getStep(stepID int) *OnboardingStep {
	steps := m.getAllSteps()
	if stepID < 1 || stepID > len(steps) {
		return nil
	}
	return &steps[stepID-1]
}

// getAllSteps returns all onboarding steps
func (m *OnboardingManager) getAllSteps() []OnboardingStep {
	return []OnboardingStep{
		{
			ID:    1,
			Title: "欢迎 👋",
			Message: "欢迎使用云海看板娘！\n\n" +
				"我是你的智能媒体助手\n\n" +
				"• 🎬 搜索电影和剧集\n" +
				"• 📋 一键请求资源\n" +
				"• 🔔 自动提醒可用\n\n" +
				"点击左下角菜单查看所有功能",
			Buttons: []OnboardingButton{
				{Text: "🚀 开始探索", Action: "next", Primary: true},
				{Text: "⏭️ 跳过", Action: "skip", Primary: false},
			},
			SkipAllowed: true,
		},
		{
			ID:    2,
			Title: "快速搜索 🔍",
			Message: "超简单的搜索方式\n\n" +
				"直接输入内容名就行！\n\n" +
				"试试：\n" +
				"• 复仇者联盟\n" +
				"• 权力的游戏\n" +
				"• 2024年的电影\n\n" +
				"系统会自动识别~",
			Buttons: []OnboardingButton{
				{Text: "明白了", Action: "next", Primary: true},
				{Text: "🔙 返回", Action: "prev", Primary: false},
			},
			SkipAllowed: true,
		},
		{
			ID:    3,
			Title: "一键请求 📋",
			Message: "搜索到想看的内容？\n\n" +
				"直接点击【请求】按钮\n" +
				"就这么简单！\n\n" +
				"完成后会自动通知你~",
			Buttons: []OnboardingButton{
				{Text: "明白了", Action: "next", Primary: true},
				{Text: "🔙 返回", Action: "prev", Primary: false},
			},
			SkipAllowed: true,
		},
	}
}

// save saves onboarding data to file
func (m *OnboardingManager) save() {
	data := struct {
		CompletedUsers map[int64]bool
		Progress       map[int64]*OnboardingProgress
	}{
		CompletedUsers: m.completedUsers,
		Progress:       m.progress,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("Error marshaling onboarding data: %v", err)
		return
	}

	err = os.WriteFile(m.storageFile, jsonData, 0644)
	if err != nil {
		log.Printf("Error saving onboarding data: %v", err)
	}
}

// load loads onboarding data from file
func (m *OnboardingManager) load() {
	data, err := os.ReadFile(m.storageFile)
	if err != nil {
		log.Println("No existing onboarding data found")
		return
	}

	var loaded struct {
		CompletedUsers map[int64]bool
		Progress       map[int64]*OnboardingProgress
	}

	err = json.Unmarshal(data, &loaded)
	if err != nil {
		log.Printf("Error loading onboarding data: %v", err)
		return
	}

	m.completedUsers = loaded.CompletedUsers
	m.progress = loaded.Progress

	if m.completedUsers == nil {
		m.completedUsers = make(map[int64]bool)
	}
	if m.progress == nil {
		m.progress = make(map[int64]*OnboardingProgress)
	}

	log.Printf("Loaded onboarding data: %d completed users", len(m.completedUsers))
}

// FormatOnboardingStep formats an onboarding step for display
func FormatOnboardingStep(step *OnboardingStep, progress *OnboardingProgress) (string, *TelegramInlineKeyboard) {
	if step == nil {
		return "", nil
	}

	stepNum := 1
	if progress != nil {
		stepNum = progress.Step
	}
	if stepNum > 3 {
		stepNum = 3
	}

	msg := fmt.Sprintf("✨ *%s* (%d/3)\n\n", step.Title, stepNum)
	msg += step.Message

	// Create keyboard
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{},
	}

	for _, btn := range step.Buttons {
		row := []map[string]string{
			{"text": btn.Text, "callback_data": fmt.Sprintf("onboard_%s", btn.Action)},
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
	}

	return msg, keyboard
}

// GetWelcomeKeyboard returns the welcome keyboard for new users
func GetWelcomeKeyboard(userID int64) *TelegramInlineKeyboard {
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "📖 快速入门", "callback_data": "onboard_start"},
			},
			{
				{"text": "🔍 搜索内容", "callback_data": "action_search"},
				{"text": "📋 我的请求", "callback_data": "action_myrequests"},
			},
		},
	}

	return keyboard
}

// GetQuickStartKeyboard returns a quick start keyboard
func GetQuickStartKeyboard() *TelegramInlineKeyboard {
	keyboard := &TelegramInlineKeyboard{
		InlineKeyboard: [][]map[string]string{
			{
				{"text": "🔍 搜索内容", "callback_data": "action_search"},
			},
			{
				{"text": "🔥 热门推荐", "callback_data": "search_trending"},
				{"text": "📺 热播剧集", "callback_data": "search_tv_hot"},
			},
			{
				{"text": "🎬 最新电影", "callback_data": "search_movie_new"},
				{"text": "🎲 随机推荐", "callback_data": "action_random"},
			},
			{
				{"text": "📋 我的请求", "callback_data": "action_myrequests"},
				{"text": "⚙️ 设置", "callback_data": "action_settings"},
			},
			{
				{"text": "❓ 帮助", "callback_data": "action_help"},
			},
		},
	}

	return keyboard
}

// HandleOnboardingCallback handles onboarding button callbacks
func HandleOnboardingCallback(userID int64, username string, action string) (string, *TelegramInlineKeyboard, bool) {
	if onboardingMgr == nil {
		return "❌ 教程系统未初始化", nil, false
	}

	switch action {
	case "start":
		// Start onboarding
		step := onboardingMgr.StartOnboarding(userID, username)
		if step == nil {
			// Already completed
			msg := "🎉 *你已经完成过教程了！*\n\n"
			msg += "现在可以直接使用机器人\n\n"
			msg += "💡 输入内容名开始搜索"
			return msg, GetQuickStartKeyboard(), false
		}

		progress := onboardingMgr.GetProgress(userID)
		msg, keyboard := FormatOnboardingStep(step, progress)
		return msg, keyboard, false

	case "next":
		// Advance to next step
		nextStep := onboardingMgr.AdvanceProgress(userID, "clicked_next")
		if nextStep == nil {
			// Onboarding complete
			msg := "🎉 *教程完成！*\n\n"
			msg += "你已经掌握了基本操作\n\n"
			msg += "💡 现在可以开始使用了"
			return msg, GetQuickStartKeyboard(), true
		}

		progress := onboardingMgr.GetProgress(userID)
		msg, keyboard := FormatOnboardingStep(nextStep, progress)
		return msg, keyboard, false

	case "prev":
		// Go back
		progress := onboardingMgr.GetProgress(userID)
		if progress != nil && progress.Step > 1 {
			step := onboardingMgr.getStep(progress.Step - 1)
			msg, keyboard := FormatOnboardingStep(step, progress)
			return msg, keyboard, false
		}

	case "skip":
		onboardingMgr.SkipOnboarding(userID)
		msg := "✅ *教程已跳过*\n\n"
		msg += "你可以随时发送 /help 查看帮助"
		return msg, GetQuickStartKeyboard(), true

	case "search":
		onboardingMgr.CompleteOnboarding(userID)
		msg := "🔍 *开始搜索*\n\n"
		msg += "输入你想找的内容名称\n\n"
		msg += "例如：\n"
		msg += "• 复仇者联盟\n"
		msg += "• 权力的游戏\n"
		msg += "• 2024年的电影"
		return msg, nil, true

	case "help":
		onboardingMgr.CompleteOnboarding(userID)
		msg := GetHelpMessage(LevelNormal)
		return msg, nil, true
	}

	return "", nil, false
}

// ShouldShowOnboarding checks if onboarding should be shown to user
func ShouldShowOnboarding(userID int64) bool {
	if onboardingMgr == nil {
		return false
	}
	return !onboardingMgr.IsOnboardingComplete(userID)
}

// GetWelcomeForNewUser returns welcome message for new users
func GetWelcomeForNewUser(userID int64, username string) (string, *TelegramInlineKeyboard) {
	msg := "👋 *欢迎使用云海看板娘！*\n\n"
	msg += fmt.Sprintf("你好，%s！\n\n", username)
	msg += "我是你的智能媒体助手\n\n"
	msg += "🎬 搜索电影和剧集\n"
	msg += "📋 一键请求资源\n"
	msg += "🔔 自动提醒可用\n\n"
	msg += "💡 点击左下角菜单查看所有功能"

	keyboard := GetWelcomeKeyboard(userID)
	return msg, keyboard
}
