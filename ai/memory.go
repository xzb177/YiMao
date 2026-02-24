// Package ai provides user memory and personality system
package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PersonalityMemory stores information about a user's personality
type PersonalityMemory struct {
	UserID      int64    `json:"user_id"`
	Nickname    string   `json:"nickname,omitempty"`    // 用户自定义昵称
	RealName    string   `json:"real_name,omitempty"`   // 真实姓名
	Username    string   `json:"username,omitempty"`    // Telegram用户名
	FirstSeen   int64    `json:"first_seen"`            // 首次互动时间
	LastSeen    int64    `json:"last_seen"`             // 最后互动时间
	MessageCount int     `json:"message_count"`         // 消息数量

	// 偏好
	Preferences UserPreferences `json:"preferences"`

	// 关系
	RelationLevel string `json:"relation_level"` // stranger, acquaintance, friend, close

	// 标签 (由小凛添加)
	Tags []string `json:"tags,omitempty"`

	// 重要事件
	Memories []string `json:"memories,omitempty"` // 小凛记住的重要时刻

	// 情绪记录
	MoodHistory []MoodEntry `json:"mood_history,omitempty"`

	// 最后情绪状态 (用于情感支持)
	LastMood       string `json:"last_mood,omitempty"`        // 最后一次检测到的情绪
	LastMoodTime   int64  `json:"last_mood_time,omitempty"`   // 最后一次情绪检测时间

	// 对话上下文 (用于多轮对话)
	LastTopics     []string `json:"last_topics,omitempty"`    // 最近讨论的话题
	LastQuestions  []string `json:"last_questions,omitempty"` // 最近提出的问题

	// 请求历史 (用于延续对话)
	RequestHistory []RequestRecord `json:"request_history,omitempty"` // 求片历史

	// 情感状态记录 (用于情感支持)
	EmotionalState string `json:"emotional_state,omitempty"` // 当前情感状态: happy, sad, stressed, neutral
}

// RequestRecord 求片记录
type RequestRecord struct {
	MediaTitle string `json:"media_title"`  // 媒体标题
	RequestTime int64 `json:"request_time"` // 请求时间
	Status     string `json:"status"`       // 状态: pending, available, declined
}

// UserPreferences 用户偏好
type UserPreferences struct {
	FavoriteGenres    []string `json:"favorite_genres,omitempty"`     // 喜欢的类型
	DislikedGenres    []string `json:"disliked_genres,omitempty"`     // 讨厌的类型
	FavoriteMedia     []string `json:"favorite_media,omitempty"`      // 喜欢的作品
	WatchingList      []string `json:"watching_list,omitempty"`       // 正在看
	CommunicationStyle string `json:"communication_style,omitempty"` // 交流风格偏好
}

// MoodEntry 情绪记录
type MoodEntry struct {
	Timestamp int64  `json:"timestamp"`
	Mood      string `json:"mood"`      // happy, sad, excited, bored等
	Context   string `json:"context"`   // 触发原因
}

// MemorySystem 管理用户记忆
type MemorySystem struct {
	memories map[int64]*PersonalityMemory
	mu       sync.RWMutex
	filePath string
}

var (
	globalMemory *MemorySystem
	memoryOnce    sync.Once
)

// GetMemorySystem 获取全局记忆系统
func GetMemorySystem() *MemorySystem {
	memoryOnce.Do(func() {
		globalMemory = &MemorySystem{
			memories: make(map[int64]*PersonalityMemory),
			filePath: filepath.Join(os.Getenv("HOME"), "emby-telegram-bot", "user_memories.json"),
		}
		globalMemory.Load()
	})
	return globalMemory
}

// Load 从文件加载记忆
func (m *MemorySystem) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常的
		}
		return err
	}

	var stored struct {
		Memories map[int64]*PersonalityMemory `json:"memories"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	m.memories = stored.Memories
	return nil
}

// Save 保存记忆到文件
func (m *MemorySystem) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.MarshalIndent(struct {
		Memories map[int64]*PersonalityMemory `json:"memories"`
		UpdatedAt int64                      `json:"updated_at"`
	}{
		Memories:  m.memories,
		UpdatedAt: time.Now().Unix(),
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.filePath, data, 0644)
}

// GetOrCreateUser 获取或创建用户记忆
func (m *MemorySystem) GetOrCreateUser(userID int64, username string) *PersonalityMemory {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.memories[userID]
	if !exists {
		now := time.Now().Unix()
		user = &PersonalityMemory{
			UserID:        userID,
			Username:      username,
			FirstSeen:     now,
			LastSeen:      now,
			MessageCount:  0,
			RelationLevel: "stranger",
			Tags:          []string{},
			Memories:      []string{},
			MoodHistory:   []MoodEntry{},
		}
		m.memories[userID] = user
	}
	return user
}

// UpdateInteraction 更新互动记录
func (m *MemorySystem) UpdateInteraction(userID int64, username string, mood string) {
	user := m.GetOrCreateUser(userID, username)

	user.LastSeen = time.Now().Unix()
	user.MessageCount++

	// 更新关系等级
	if user.MessageCount >= 100 {
		user.RelationLevel = "close"
	} else if user.MessageCount >= 30 {
		user.RelationLevel = "friend"
	} else if user.MessageCount >= 10 {
		user.RelationLevel = "acquaintance"
	}

	// 记录情绪
	if mood != "" {
		user.MoodHistory = append(user.MoodHistory, MoodEntry{
			Timestamp: time.Now().Unix(),
			Mood:      mood,
		})
		// 只保留最近50条
		if len(user.MoodHistory) > 50 {
			user.MoodHistory = user.MoodHistory[len(user.MoodHistory)-50:]
		}
	}
}

// AddMemory 添加重要记忆
func (m *MemorySystem) AddMemory(userID int64, memory string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user == nil {
		return
	}

	user.Memories = append(user.Memories, memory)
	// 最多保留20条重要记忆
	if len(user.Memories) > 20 {
		user.Memories = user.Memories[len(user.Memories)-20:]
	}
}

// SetNickname 设置用户昵称
func (m *MemorySystem) SetNickname(userID int64, nickname string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user != nil {
		user.Nickname = nickname
	}
}

// GetNickname 获取用户昵称
func (m *MemorySystem) GetNickname(userID int64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user := m.memories[userID]
	if user != nil && user.Nickname != "" {
		return user.Nickname
	}
	return ""
}

// AddTag 添加用户标签
func (m *MemorySystem) AddTag(userID int64, tag string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user != nil {
		for _, t := range user.Tags {
			if t == tag {
				return
			}
		}
		user.Tags = append(user.Tags, tag)
	}
}

// GetUserContext 获取用户上下文信息（用于AI提示）
func (m *MemorySystem) GetUserContext(userID int64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user := m.memories[userID]
	if user == nil {
		return ""
	}

	var context string

	// 关系和称呼
	switch user.RelationLevel {
	case "close":
		context += fmt.Sprintf("这位是熟人了，叫ta昵称\"%s\"或者\"两脚兽\"", user.getDisplayName())
	case "friend":
		context += "这是朋友，可以稍微随意一点"
	case "acquaintance":
		context += "这是认识的人，保持基本礼貌"
	default:
		context += "这是陌生人，保持高冷"
	}

	// 偏好
	if len(user.Preferences.FavoriteGenres) > 0 {
		context += fmt.Sprintf("\n喜欢类型: %v", user.Preferences.FavoriteGenres)
	}

	// 重要记忆
	if len(user.Memories) > 0 {
		context += fmt.Sprintf("\n记住的事: %s", user.Memories[len(user.Memories)-1])
	}

	// 标签
	if len(user.Tags) > 0 {
		context += fmt.Sprintf("\n标签: %v", user.Tags)
	}

	return context
}

// getDisplayName 获取显示名称
func (u *PersonalityMemory) getDisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.RealName != "" {
		return u.RealName
	}
	return u.Username
}

// FormatMemoryForAI 将用户记忆格式化为AI提示
func (m *MemorySystem) FormatMemoryForAI(userID int64) string {
	user := m.GetOrCreateUser(userID, "")

	if user.MessageCount < 5 {
		return "" // 新用户，不需要太多上下文
	}

	var info strings.Builder

	// 基本信息
	info.WriteString("【用户信息】")
	info.WriteString(fmt.Sprintf("昵称: %s", user.getDisplayName()))
	info.WriteString(fmt.Sprintf(" | 互动次数: %d", user.MessageCount))
	info.WriteString(fmt.Sprintf(" | 关系: %s", user.getRelationText()))

	// 偏好
	if len(user.Preferences.FavoriteGenres) > 0 {
		info.WriteString(fmt.Sprintf("\n【偏好】喜欢: %v", user.Preferences.FavoriteGenres))
	}
	if len(user.Preferences.DislikedGenres) > 0 {
		info.WriteString(fmt.Sprintf(" | 不喜欢: %v", user.Preferences.DislikedGenres))
	}
	if len(user.Preferences.FavoriteMedia) > 0 {
		info.WriteString(fmt.Sprintf("\n【喜欢的作品】%v", user.Preferences.FavoriteMedia))
	}

	// 最近话题 (用于多轮对话)
	if len(user.LastTopics) > 0 {
		info.WriteString(fmt.Sprintf("\n【最近讨论的话题】"))
		for i, topic := range user.LastTopics {
			if i >= 3 {
				break // 只显示最近3个
			}
			info.WriteString(fmt.Sprintf("• %s", topic))
		}
	}

	// 最近问题 (用于延续对话)
	if len(user.LastQuestions) > 0 {
		info.WriteString(fmt.Sprintf("\n【最近问过的问题】"))
		for i, q := range user.LastQuestions {
			if i >= 3 {
				break
			}
			info.WriteString(fmt.Sprintf("• %s", q))
		}
	}

	// 求片历史 (用于延续对话)
	if len(user.RequestHistory) > 0 {
		info.WriteString(fmt.Sprintf("\n【求片历史】"))
		for i, req := range user.RequestHistory {
			if i >= 5 {
				break
			}
			info.WriteString(fmt.Sprintf("• %s (%s)", req.MediaTitle, req.Status))
		}
	}

	// 重要记忆
	if len(user.Memories) > 0 {
		info.WriteString(fmt.Sprintf("\n【重要记忆】"))
		for _, mem := range user.Memories {
			info.WriteString(fmt.Sprintf("• %s", mem))
		}
	}

	// 情感状态 (用于情感支持)
	if user.EmotionalState != "" && user.EmotionalState != "neutral" {
		info.WriteString(fmt.Sprintf("\n【当前情感状态】%s", user.getEmotionalStateText()))
	}

	// 最后的情绪 (如果最近有负面情绪，提醒关注)
	if user.LastMood != "" {
		timeSinceMood := time.Now().Unix() - user.LastMoodTime
		if timeSinceMood < 86400 { // 24小时内有情绪记录
			if containsNegativeMood(user.LastMood) {
				info.WriteString(fmt.Sprintf("\n【情感关怀】用户最近心情不好(%s)，需要关心和安慰", user.LastMood))
			}
		}
	}

	// 标签
	if len(user.Tags) > 0 {
		info.WriteString(fmt.Sprintf("\n【标签】%v", user.Tags))
	}

	return info.String()
}

// getEmotionalStateText 获取情感状态文本
func (u *PersonalityMemory) getEmotionalStateText() string {
	switch u.EmotionalState {
	case "happy":
		return "开心"
	case "sad":
		return "难过"
	case "stressed":
		return "压力大"
	case "excited":
		return "兴奋"
	case "bored":
		return "无聊"
	default:
		return "平静"
	}
}

// containsNegativeMood 检查是否包含负面情绪
func containsNegativeMood(mood string) bool {
	negativeMoods := []string{"难过", "生气", "烦躁", "沮丧", "失望", "伤心", "压力", "疲惫"}
	for _, nm := range negativeMoods {
		if strings.Contains(mood, nm) {
			return true
		}
	}
	return false
}

// UpdateMood 更新用户情绪状态
func (m *MemorySystem) UpdateMood(userID int64, mood string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user == nil {
		return
	}

	user.LastMood = mood
	user.LastMoodTime = time.Now().Unix()

	// 更新情感状态
	if containsNegativeMood(mood) {
		if strings.Contains(mood, "压力") {
			user.EmotionalState = "stressed"
		} else {
			user.EmotionalState = "sad"
		}
	} else if strings.Contains(mood, "开心") || strings.Contains(mood, "高兴") {
		user.EmotionalState = "happy"
	} else if strings.Contains(mood, "无聊") {
		user.EmotionalState = "bored"
	}
}

// AddTopic 添加讨论话题
func (m *MemorySystem) AddTopic(userID int64, topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user == nil {
		return
	}

	// 检查是否已存在
	for _, t := range user.LastTopics {
		if t == topic {
			return
		}
	}

	user.LastTopics = append(user.LastTopics, topic)
	// 只保留最近10个话题
	if len(user.LastTopics) > 10 {
		user.LastTopics = user.LastTopics[len(user.LastTopics)-10:]
	}
}

// AddQuestion 添加用户问题
func (m *MemorySystem) AddQuestion(userID int64, question string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user == nil {
		return
	}

	user.LastQuestions = append(user.LastQuestions, question)
	// 只保留最近10个问题
	if len(user.LastQuestions) > 10 {
		user.LastQuestions = user.LastQuestions[len(user.LastQuestions)-10:]
	}
}

// AddRequestRecord 添加求片记录
func (m *MemorySystem) AddRequestRecord(userID int64, mediaTitle string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user == nil {
		return
	}

	record := RequestRecord{
		MediaTitle: mediaTitle,
		RequestTime: time.Now().Unix(),
		Status:     status,
	}

	user.RequestHistory = append(user.RequestHistory, record)
	// 只保留最近20条记录
	if len(user.RequestHistory) > 20 {
		user.RequestHistory = user.RequestHistory[len(user.RequestHistory)-20:]
	}
}

// GetRecentRequests 获取最近的求片记录
func (m *MemorySystem) GetRecentRequests(userID int64) []RequestRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user := m.memories[userID]
	if user == nil {
		return nil
	}

	// 返回最近3条
	if len(user.RequestHistory) <= 3 {
		return user.RequestHistory
	}
	return user.RequestHistory[len(user.RequestHistory)-3:]
}

// GetUserEmotionalContext 获取用户情感上下文（用于情感支持）
func (m *MemorySystem) GetUserEmotionalContext(userID int64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user := m.memories[userID]
	if user == nil {
		return ""
	}

	var ctx strings.Builder

	// 当前情感状态
	if user.EmotionalState != "" && user.EmotionalState != "neutral" {
		ctx.WriteString(fmt.Sprintf("用户当前心情: %s", user.getEmotionalStateText()))
	}

	// 最近的情绪记录
	if len(user.MoodHistory) > 0 {
		recentMoods := user.MoodHistory
		if len(recentMoods) > 5 {
			recentMoods = recentMoods[len(recentMoods)-5:]
		}

		negativeCount := 0
		for _, mood := range recentMoods {
			if containsNegativeMood(mood.Mood) {
				negativeCount++
			}
		}

		if negativeCount >= 3 {
			ctx.WriteString(" | 用户最近情绪低落，需要更多关心和鼓励")
		}
	}

	return ctx.String()
}

// ClearEmotionalState 清除情感状态（用户情绪好转时调用）
func (m *MemorySystem) ClearEmotionalState(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := m.memories[userID]
	if user != nil {
		user.EmotionalState = "neutral"
	}
}

func (u *PersonalityMemory) getRelationText() string {
	switch u.RelationLevel {
	case "close":
		return "熟人"
	case "friend":
		return "朋友"
	case "acquaintance":
		return "认识的人"
	default:
		return "陌生人"
	}
}
