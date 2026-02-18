package main

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// NotificationRewards handles rewards for notifications and interactions
type NotificationRewards struct {
	lootTable map[string]*LootDrop
	mu        sync.RWMutex
	enabled   bool
}

// LootDrop represents a potential reward drop
type LootDrop struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Probability float64 `json:"probability"` // 0-1
	MinReward   int     `json:"minReward"`
	MaxReward   int     `json:"maxReward"`
	Emoji       string  `json:"emoji"`
}

// RewardResult represents a reward that was given
type RewardResult struct {
	Name        string
	Description string
	Amount      int
	Emoji       string
}

var notificationRewards *NotificationRewards

// InitNotificationRewards initializes the reward system
func InitNotificationRewards() {
	notificationRewards = &NotificationRewards{
		lootTable: map[string]*LootDrop{
			"exp_small": {
				ID:          "exp_small",
				Name:        "经验值",
				Description: "获得少量经验",
				Probability: 0.40,
				MinReward:   5,
				MaxReward:   15,
				Emoji:       "✨",
			},
			"exp_medium": {
				ID:          "exp_medium",
				Name:        "经验包",
				Description: "获得中等经验",
				Probability: 0.25,
				MinReward:   20,
				MaxReward:   50,
				Emoji:       "⭐",
			},
			"exp_large": {
				ID:          "exp_large",
				Name:        "经验宝箱",
				Description: "获得大量经验！",
				Probability: 0.08,
				MinReward:   75,
				MaxReward:   150,
				Emoji:       "💎",
			},
			"streak_bonus": {
				ID:          "streak_bonus",
				Name:        "连续签到奖励",
				Description: "连续签到加成",
				Probability: 0.12,
				MinReward:   10,
				MaxReward:   30,
				Emoji:       "🔥",
			},
			"rare_find": {
				ID:          "rare_find",
				Name:        "稀有发现",
				Description: "系统特别奖励",
				Probability: 0.03,
				MinReward:   100,
				MaxReward:   300,
				Emoji:       "🌟",
			},
		},
		enabled: true,
	}

	// Initialize RNG with proper seed
	rand.Seed(time.Now().UnixNano())

	log.Println("NotificationRewards initialized")
}

// TryDropReward attempts to drop a reward
// Returns (reward, dropped)
func TryDropReward(telegramID int64, reason string) (*RewardResult, bool) {
	if notificationRewards == nil || !notificationRewards.enabled || engagementSys == nil {
		return nil, false
	}

	// 15% base chance for any reward (increased from 10%)
	if rand.Float64() > 0.15 {
		return nil, false
	}

	notificationRewards.mu.RLock()
	defer notificationRewards.mu.RUnlock()

	// Roll for specific reward based on probability weights
	roll := rand.Float64()
	cumulative := 0.0
	totalProb := 0.0

	// Calculate total probability for normalization
	for _, drop := range notificationRewards.lootTable {
		totalProb += drop.Probability
	}

	// Normalize roll to total probability range
	normalizedRoll := roll * totalProb

	for _, drop := range notificationRewards.lootTable {
		cumulative += drop.Probability
		if normalizedRoll <= cumulative {
			// Award this reward
			amount := drop.MinReward + rand.Intn(drop.MaxReward-drop.MinReward+1)

			reward := &RewardResult{
				Name:        drop.Name,
				Description: drop.Description,
				Amount:      amount,
				Emoji:       drop.Emoji,
			}

			// Apply the reward
			switch drop.ID {
			case "exp_small", "exp_medium", "exp_large", "rare_find":
				engagementSys.RecordActivity(telegramID, "bonus", amount)
			case "streak_bonus":
				engagementSys.RecordActivity(telegramID, "bonus", amount)
			}

			// Notify user asynchronously
			go sendRewardNotification(telegramID, reward, reason)

			log.Printf("RewardSystem: %s dropped for user %d (reason: %s, amount: %d)",
				drop.ID, telegramID, reason, amount)
			return reward, true
		}
	}

	return nil, false
}

// sendRewardNotification sends a reward notification to user
func sendRewardNotification(telegramID int64, reward *RewardResult, reason string) {
	msg := fmt.Sprintf("🎁 *意外收获！*\n\n")
	msg += fmt.Sprintf("%s *%s*\n", reward.Emoji, reward.Name)
	msg += fmt.Sprintf("%s\n", reward.Description)
	msg += fmt.Sprintf("数量: +%d\n\n", reward.Amount)
	msg += fmt.Sprintf("💡 因为%s触发", reason)

	sendPrivateMessage(telegramID, msg, nil)
}

// FormatRewardMessage formats a reward message
func FormatRewardMessage(reward *RewardResult) string {
	msg := fmt.Sprintf("%s *%s*\n", reward.Emoji, reward.Name)
	msg += fmt.Sprintf("%s (+%d)", reward.Description, reward.Amount)
	return msg
}

// GetLootDropStatus gets info about current loot drops (for admins)
func GetLootDropStatus() string {
	if notificationRewards == nil {
		return "奖励系统未初始化"
	}

	notificationRewards.mu.RLock()
	defer notificationRewards.mu.RUnlock()

	msg := "🎁 *当前掉落表*\n\n"
	msg += "每次互动有 15%% 概率触发奖励\n\n"

	for _, drop := range notificationRewards.lootTable {
		probability := int(drop.Probability * 100)
		msg += fmt.Sprintf("%s %s - %d%%\n", drop.Emoji, drop.Name, probability)
		msg += fmt.Sprintf("   奖励: %d-%d 经验\n\n", drop.MinReward, drop.MaxReward)
	}

	return msg
}

// SetEnabled enables or disables the reward system
func SetRewardsEnabled(enabled bool) {
	if notificationRewards != nil {
		notificationRewards.mu.Lock()
		notificationRewards.enabled = enabled
		notificationRewards.mu.Unlock()
		log.Printf("RewardSystem: Enabled = %v", enabled)
	}
}

// IsRewardsEnabled checks if rewards are enabled
func IsRewardsEnabled() bool {
	if notificationRewards == nil {
		return false
	}
	notificationRewards.mu.RLock()
	defer notificationRewards.mu.RUnlock()
	return notificationRewards.enabled
}
