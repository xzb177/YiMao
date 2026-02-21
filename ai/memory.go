// Package ai provides user memory and personality system
package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UserMemory stores information about a user
type UserMemory struct {
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

	// 标签 (由凛冬添加)
	Tags []string `json:"tags,omitempty"`

	// 重要事件
	Memories []string `json:"memories,omitempty"` // 凛冬记住的重要时刻

	// 情绪记录
	MoodHistory []MoodEntry `json:"mood_history,omitempty"`
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
	memories map[int64]*UserMemory
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
			memories: make(map[int64]*UserMemory),
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
		Memories map[int64]*UserMemory `json:"memories"`
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
		Memories map[int64]*UserMemory `json:"memories"`
		UpdatedAt int64                 `json:"updated_at"`
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
func (m *MemorySystem) GetOrCreateUser(userID int64, username string) *UserMemory {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.memories[userID]
	if !exists {
		now := time.Now().Unix()
		user = &UserMemory{
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
func (u *UserMemory) getDisplayName() string {
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

	var info string
	info += fmt.Sprintf("【用户信息】")
	info += fmt.Sprintf("昵称: %s", user.getDisplayName())
	info += fmt.Sprintf(" | 互动次数: %d", user.MessageCount)
	info += fmt.Sprintf(" | 关系: %s", user.getRelationText())

	if len(user.Preferences.FavoriteGenres) > 0 {
		info += fmt.Sprintf("\n【偏好】喜欢: %v", user.Preferences.FavoriteGenres)
	}

	if len(user.Memories) > 0 {
		info += fmt.Sprintf("\n【重要记忆】")
		for _, mem := range user.Memories {
			info += fmt.Sprintf("• %s", mem)
		}
	}

	return info
}

func (u *UserMemory) getRelationText() string {
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
