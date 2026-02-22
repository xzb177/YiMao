// Package ai provides intelligent learning and feedback system
package ai

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ============================================
// 智能学习系统 - 从反馈中持续优化
// ============================================

// LearningSystem 学习系统
type LearningSystem struct {
	engine          *RecommendationEngine
	feedbackBuffer  map[int64][]*Feedback       // 用户反馈缓存
	bufferMutex     sync.RWMutex
	modelVersion    int                        // 当前模型版本
	lastUpdate      time.Time
	enabled         bool
}

// Feedback 用户反馈
type Feedback struct {
	UserID        int64     `json:"user_id"`
	ItemID        string    `json:"item_id"`
	ItemTitle     string    `json:"item_title"`
	Reaction      string    `json:"reaction"`     // like/dislike/skip/view
	Rating        float64   `json:"rating"`       // 1-5 if provided
	Timestamp     time.Time `json:"timestamp"`
	Context       string    `json:"context"`      // mood/situation
	RecommendID   string    `json:"recommend_id"` // 哪次推荐的
	StrategyUsed  string    `json:"strategy_used"` // 使用的推荐策略
}

// LearningMetrics 学习指标
type LearningMetrics struct {
	TotalFeedback      int                    `json:"total_feedback"`
	PositiveRate       float64                `json:"positive_rate"`
	StrategyPerformance map[string]float64    `json:"strategy_performance"` // 各策略表现
	UserSatisfaction   map[int64]float64      `json:"user_satisfaction"`    // 用户满意度
	TrendingItems      map[string]int         `json:"trending_items"`       // 热门项目
	LastUpdate         time.Time              `json:"last_update"`
}

// Insight 洞察（AI 生成的发现）
type Insight struct {
	Type        string    `json:"type"`        // trend/pattern/anomaly
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence"`
	Timestamp   time.Time `json:"timestamp"`
	Actionable  bool      `json:"actionable"`
}

// NewLearningSystem 创建学习系统
func NewLearningSystem(engine *RecommendationEngine) *LearningSystem {
	ls := &LearningSystem{
		engine:         engine,
		feedbackBuffer: make(map[int64][]*Feedback),
		modelVersion:   1,
		lastUpdate:     time.Now(),
		enabled:        engine != nil,
	}

	// 加载历史数据
	ls.loadFromDisk()

	// 定期保存和学习
	go ls.periodicLearning()

	return ls
}

// RecordFeedback 记录反馈
func (ls *LearningSystem) RecordFeedback(feedback *Feedback) {
	if !ls.enabled {
		return
	}

	feedback.Timestamp = time.Now()

	ls.bufferMutex.Lock()
	ls.feedbackBuffer[feedback.UserID] = append(ls.feedbackBuffer[feedback.UserID], feedback)

	// 达到阈值时触发学习
	if len(ls.feedbackBuffer[feedback.UserID]) >= 5 {
		go ls.learnFromFeedback(feedback.UserID)
	}
	ls.bufferMutex.Unlock()

	log.Printf("[Learning] Recorded feedback: user=%d, item=%s, reaction=%s",
		feedback.UserID, feedback.ItemTitle, feedback.Reaction)
}

// learnFromFeedback 从反馈中学习
func (ls *LearningSystem) learnFromFeedback(userID int64) {
	ls.bufferMutex.Lock()
	feedbacks := ls.feedbackBuffer[userID]
	delete(ls.feedbackBuffer, userID)
	ls.bufferMutex.Unlock()

	if len(feedbacks) == 0 {
		return
	}

	log.Printf("[Learning] Processing %d feedbacks for user %d", len(feedbacks), userID)

	// 更新推荐引擎中的用户画像
	for _, fb := range feedbacks {
		switch fb.Reaction {
		case "like", "love":
			ls.engine.RecordInteraction(userID, "feedback", map[string]interface{}{
				"sentiment": "positive",
				"item_id":   fb.ItemID,
			})
		case "dislike", "hate":
			ls.engine.RecordInteraction(userID, "feedback", map[string]interface{}{
				"sentiment": "negative",
				"item_id":   fb.ItemID,
			})
		}
	}

	// 定期生成洞察并保存
	ls.generateInsights()
	ls.saveToDisk()
}

// generateInsights AI 生成洞察
func (ls *LearningSystem) generateInsights() {
	if ls.engine == nil || ls.engine.zhipu == nil || !ls.engine.zhipu.IsEnabled() {
		return
	}

	// 收集全局反馈数据
	ls.bufferMutex.RLock()
	allFeedbacks := []*Feedback{}
	for _, feedbacks := range ls.feedbackBuffer {
		allFeedbacks = append(allFeedbacks, feedbacks...)
	}
	ls.bufferMutex.RUnlock()

	if len(allFeedbacks) < 10 {
		return // 数据不足
	}

	// 统计
	positiveCount := 0
	strategyStats := make(map[string]int)
	trendingItems := make(map[string]int)

	for _, fb := range allFeedbacks {
		if fb.Reaction == "like" || fb.Reaction == "love" {
			positiveCount++
		}
		if fb.StrategyUsed != "" {
			strategyStats[fb.StrategyUsed]++
		}
		if fb.Reaction == "like" {
			trendingItems[fb.ItemTitle]++
		}
	}

	// 生成 AI 洞察
	insightQuery := fmt.Sprintf(`分析推荐系统反馈数据：

【反馈统计】
- 总反馈数: %d
- 正面反馈率: %.1f%%
- 策略表现: %v
- 热门项目: %v

【任务】
1. 分析哪个策略表现最好
2. 发现用户行为模式
3. 提出优化建议

【返回JSON】
{
  "best_strategy": "表现最好的策略",
  "insights": ["洞察1", "洞察2", "洞察3"],
  "suggestions": ["建议1", "建议2"]
}`,
		len(allFeedbacks),
		float64(positiveCount)/float64(len(allFeedbacks))*100,
		strategyStats,
		trendingItems)

	response, err := ls.engine.zhipu.Send(insightQuery, "你是推荐系统分析专家。")
	if err == nil {
		log.Printf("[Learning] AI Insights: %s", response)
	}
}

// GetPersonalizedReason 获取个性化推荐理由（AI 生成）
func (ls *LearningSystem) GetPersonalizedReason(userID int64, itemTitle string, itemType string) string {
	if ls.engine == nil || ls.engine.zhipu == nil || !ls.engine.zhipu.IsEnabled() {
		return "根据你的观看偏好推荐"
	}

	// 获取用户画像
	ls.engine.profileMutex.RLock()
	profile, exists := ls.engine.userProfiles[userID]
	ls.engine.profileMutex.RUnlock()

	var userContext string
	if exists {
		userContext = fmt.Sprintf(`
- 观影人格: %s
- 喜欢类型: %s
- 最近请求: %s
`,
			profile.AIPersona,
			func() string {
				if len(profile.Preferences.FavoriteGenres) > 0 {
					return strings.Join(profile.Preferences.FavoriteGenres, ", ")
				}
				return "不限"
			}(),
			func() string {
				if len(profile.Behavior.SearchQueries) > 0 {
					return strings.Join(profile.Behavior.SearchQueries[len(profile.Behavior.SearchQueries)-3:], ", ")
				}
				return "无"
			}())
	}

	query := fmt.Sprintf(`为用户生成个性化推荐理由：

【推荐项目】
- 标题: %s
- 类型: %s

【用户画像】%s
【要求】
- 用一句话（15字内）说明为什么推荐
- 个性化、具体、有说服力
- 可以带点傲娇风格（如果用户喜欢的话）`,
		itemTitle, itemType, userContext)

	response, err := ls.engine.zhipu.Send(query, "你是推荐理由生成专家。")
	if err != nil {
		return "根据你的喜好精心挑选"
	}

	// 清理响应
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// 限制长度
	if len(response) > 30 {
		response = response[:27] + "..."
	}

	return response
}

// ExplainRecommendation 解释推荐（详细版）
func (ls *LearningSystem) ExplainRecommendation(userID int64, result *RecommendationResultV2) ([]string, error) {
	if ls.engine == nil || ls.engine.zhipu == nil || !ls.engine.zhipu.IsEnabled() {
		return []string{
			"评分: " + fmt.Sprintf("%.1f", result.Rating),
			"类型: " + strings.Join(result.Genre, ", "),
			"年份: " + fmt.Sprintf("%d", result.Year),
		}, nil
	}

	// 获取用户画像用于上下文
	ls.engine.profileMutex.RLock()
	profile, _ := ls.engine.userProfiles[userID]
	ls.engine.profileMutex.RUnlock()

	var contextInfo string
	if profile != nil {
		contextInfo = fmt.Sprintf(`
用户偏好:
- 喜欢类型: %s
- 观影人格: %s
- 最近活跃: %s`,
			func() string {
				if len(profile.Preferences.FavoriteGenres) > 0 {
					return strings.Join(profile.Preferences.FavoriteGenres, ", ")
				}
				return "不限"
			}(),
			profile.AIPersona,
			func() string {
				if profile.Behavior.RequestCount > 10 {
					return "非常活跃"
				}
				return "一般"
			}())
	}

	query := fmt.Sprintf(`解释为什么推荐这个给用户：

【推荐项目】
- 标题: %s (%d)
- 类型: %s
- 评分: %.1f

%s

【任务】
生成 3 个推荐理由，每条不超过 20 字

【返回格式】
["理由1", "理由2", "理由3"]`,
		result.Title, result.Year, strings.Join(result.Genre, ", "), result.Rating, contextInfo)

	response, err := ls.engine.zhipu.Send(query, "你是推荐解释专家。")
	if err != nil {
		return result.Why, nil
	}

	// 解析
	response = cleanAIResponse(response)
	var reasons []string
	if err := json.Unmarshal([]byte(response), &reasons); err != nil {
		// 尝试直接分行解析
		reasons = strings.Split(response, "\n")
	}

	return reasons, nil
}

// GetMetrics 获取学习指标
func (ls *LearningSystem) GetMetrics() *LearningMetrics {
	metrics := &LearningMetrics{
		StrategyPerformance: make(map[string]float64),
		UserSatisfaction:   make(map[int64]float64),
		TrendingItems:      make(map[string]int),
		LastUpdate:         time.Now(),
	}

	ls.bufferMutex.RLock()
	defer ls.bufferMutex.RUnlock()

	totalFeedback := 0
	positiveCount := 0
	strategyStats := make(map[string]int)
	userStats := make(map[int64][]int) // userID -> [positive, total]

	for _, feedbacks := range ls.feedbackBuffer {
		for _, fb := range feedbacks {
			totalFeedback++
			if fb.Reaction == "like" || fb.Reaction == "love" {
				positiveCount++
				userStats[fb.UserID] = append(userStats[fb.UserID], 1)
			} else {
				userStats[fb.UserID] = append(userStats[fb.UserID], 0)
			}

			if fb.StrategyUsed != "" {
				strategyStats[fb.StrategyUsed]++
			}

			if fb.Reaction == "like" {
				metrics.TrendingItems[fb.ItemTitle]++
			}
		}
	}

	metrics.TotalFeedback = totalFeedback
	if totalFeedback > 0 {
		metrics.PositiveRate = float64(positiveCount) / float64(totalFeedback)
	}

	// 计算各策略表现
	for strategy, count := range strategyStats {
		metrics.StrategyPerformance[strategy] = float64(count) / float64(totalFeedback)
	}

	// 计算用户满意度
	for userID, ratings := range userStats {
		if len(ratings) > 0 {
			sum := 0
			for _, r := range ratings {
				sum += r
			}
			metrics.UserSatisfaction[userID] = float64(sum) / float64(len(ratings))
		}
	}

	return metrics
}

// periodicLearning 定期学习
func (ls *LearningSystem) periodicLearning() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		// 处理所有待学习的反馈
		ls.bufferMutex.Lock()
		userIDs := make([]int64, 0, len(ls.feedbackBuffer))
		for userID := range ls.feedbackBuffer {
			userIDs = append(userIDs, userID)
		}
		ls.bufferMutex.Unlock()

		for _, userID := range userIDs {
			ls.learnFromFeedback(userID)
		}

		// 生成洞察
		ls.generateInsights()

		// 保存到磁盘
		ls.saveToDisk()

		log.Printf("[Learning] Periodic learning completed")
	}
}

// saveToDisk 保存到磁盘
func (ls *LearningSystem) saveToDisk() error {
	ls.bufferMutex.Lock()
	defer ls.bufferMutex.Unlock()

	data := map[string]interface{}{
		"model_version":    ls.modelVersion,
		"last_update":      ls.lastUpdate.Format(time.RFC3339),
		"feedback_buffer":  ls.feedbackBuffer,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile("ai_learning_data.json", jsonData, 0644)
}

// loadFromDisk 从磁盘加载
func (ls *LearningSystem) loadFromDisk() error {
	data, err := os.ReadFile("ai_learning_data.json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var savedData struct {
		ModelVersion   int                        `json:"model_version"`
		LastUpdate     string                     `json:"last_update"`
		FeedbackBuffer map[int64][]*Feedback      `json:"feedback_buffer"`
	}

	if err := json.Unmarshal(data, &savedData); err != nil {
		return err
	}

	ls.modelVersion = savedData.ModelVersion
	if savedData.LastUpdate != "" {
		if t, err := time.Parse(time.RFC3339, savedData.LastUpdate); err == nil {
			ls.lastUpdate = t
		}
	}

	// 只加载最近的反馈
	for userID, feedbacks := range savedData.FeedbackBuffer {
		// 只保留最近 7 天的反馈
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		valid := []*Feedback{}
		for _, fb := range feedbacks {
			if !fb.Timestamp.Before(cutoff) {
				valid = append(valid, fb)
			}
		}
		if len(valid) > 0 {
			ls.feedbackBuffer[userID] = valid
		}
	}

	log.Printf("[Learning] Loaded data from disk: version=%d, users=%d", ls.modelVersion, len(ls.feedbackBuffer))
	return nil
}

// GetRecommendationExplanation 为推荐结果生成解释
func (ls *LearningSystem) GetRecommendationExplanation(userID int64, results []*RecommendationResultV2) string {
	if len(results) == 0 {
		return "暂时没有推荐内容"
	}

	// 获取学习指标
	metrics := ls.GetMetrics()

	var sb strings.Builder
	sb.WriteString("🎯 *为你推荐*\n\n")

	// 个性化说明
	if satisfaction, ok := metrics.UserSatisfaction[userID]; ok && satisfaction > 0.7 {
		sb.WriteString("✨ 根据你的观看偏好精心挑选\n\n")
	}

	sb.WriteString(fmt.Sprintf("📊 推荐理由：\n\n"))

	for i, result := range results {
		if i >= 3 {
			break
		}

		sb.WriteString(fmt.Sprintf("%d. **%s** (%d)\n", i+1, result.Title, result.Year))

		// 使用 AI 生成的推荐理由
		if result.Reason != "" {
			sb.WriteString(fmt.Sprintf("   💡 %s\n", result.Reason))
		} else {
			reason := ls.GetPersonalizedReason(userID, result.Title, result.MediaType)
			sb.WriteString(fmt.Sprintf("   💡 %s\n", reason))
		}

		// 显示匹配度
		if len(result.MatchReasons) > 0 {
			matches := make([]string, 0, len(result.MatchReasons))
			for reason := range result.MatchReasons {
				matches = append(matches, reason)
			}
			if len(matches) > 0 {
				sb.WriteString(fmt.Sprintf("   🎯 匹配: %s\n", strings.Join(matches, ", ")))
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\n💡 基于你的观影人格：**%s**", ls.getUserPersona(userID)))

	return sb.String()
}

// getUserPersona 获取用户观影人格
func (ls *LearningSystem) getUserPersona(userID int64) string {
	if ls.engine == nil {
		return "探索者"
	}

	ls.engine.profileMutex.RLock()
	defer ls.engine.profileMutex.RUnlock()

	if profile, exists := ls.engine.userProfiles[userID]; exists {
		return profile.AIPersona
	}

	return "探索者"
}
