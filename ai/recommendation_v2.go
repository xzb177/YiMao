// Package ai provides next-generation AI recommendation system
package ai

import (
	"github.com/xzb177/yimao/pkg/logger"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============================================
// AI 推荐系统 2.0 - 核心架构
// ============================================
// 设计理念：
// 1. 多维度用户画像 - 基于行为分析构建精准用户模型
// 2. 混合推荐策略 - 内容过滤 + 协同过滤 + AI 推理
// 3. 实时学习 - 从用户反馈中持续优化
// 4. 上下文感知 - 考虑时间、情绪、社交场景
// 5. 可解释性 - AI 能解释为什么推荐这个
// ============================================

// RecommendationEngine 推荐引擎核心
type RecommendationEngine struct {
	zhipu        *ZhipuClient
	userProfiles  map[int64]*UserProfileV2
	profileMutex  sync.RWMutex
	globalStats   *GlobalMediaStats
	trendingMgr   *TrendingAIManager
	enabled       bool
}

// UserProfileV2 用户画像（多维度）
type UserProfileV2 struct {
	UserID          int64                    `json:"user_id"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`

	// 行为维度
	Behavior        *UserBehavior            `json:"behavior"`
	Preferences     *UserPreferencesV2        `json:"preferences"`
	Context         *UserContext             `json:"context"`
	Interaction     *InteractionHistory      `json:"interaction"`

	// AI 推理结果（标签）
	AITags          []string                 `json:"ai_tags"`
	AIPersona       string                   `json:"ai_persona"` // AI 给用户定义的"观影人格"
	LastAIAnalysis  time.Time                `json:"last_ai_analysis"`
}

// UserBehavior 用户行为数据
type UserBehavior struct {
	// 搜索行为
	SearchQueries   []string                 `json:"search_queries"`   // 最近搜索
	SearchFreq      map[string]int           `json:"search_freq"`       // 搜索频率

	// 请求行为
	RequestCount    int                      `json:"request_count"`
	RequestTypes    map[string]int           `json:"request_types"`     // movie/tv 比例
	RequestGenres   map[string]int           `json:"request_genres"`    // 类型偏好
	RequestYears    map[int]int              `json:"request_years"`     // 年份偏好

	// 观看行为（如果有数据）
	WatchFrequency  string                   `json:"watch_frequency"`  // high/medium/low
	PeWatchingTime  string                   `json:"pe_watching_time"`  // 偏好时段
	CompletionRate  float64                  `json:"completion_rate"`   // 完片率（如果有）

	// 社交行为
	GroupActivity    int                     `json:"group_activity"`    // 群组活跃度
	HelpRequests     int                     `json:"help_requests"`     // 求助次数
	FeedbackGiven    int                     `json:"feedback_given"`    // 反馈次数
}

// UserPreferencesV2 用户偏好（显式+隐式）
type UserPreferencesV2 struct {
	// 显式偏好
	FavoriteGenres   []string                 `json:"favorite_genres"`
	DislikedGenres   []string                 `json:"disliked_genres"`
	FavoriteActors   []string                 `json:"favorite_actors"`
	FavoriteDirectors []string                `json:"favorite_directors"`
	Language         string                   `json:"language"`          // zh/en/both

	// 隐式偏好（AI 推断）
	InferredMoods    []string                 `json:"inferred_moods"`
	InferredThemes   []string                 `json:"inferred_themes"`
	Openness         float64                  `json:"openness"`         // 接受新内容程度 0-1
	NostalgiaTendency float64                 `json:"nostalgia_tendency"` // 怀旧倾向 0-1

	// 特殊偏好
	MinRating        float64                  `json:"min_rating"`        // 最低接受评分
	ExcludeWatched   bool                     `json:"exclude_watched"`   // 排除已看过
}

// UserContext 用户上下文
type UserContext struct {
	CurrentMood      string                   `json:"current_mood"`
	LastMoodUpdate   time.Time                `json:"last_mood_update"`
	TimeOfDayPattern  map[string]string       `json:"time_of_day_pattern"` // 早晚偏好
	DayOfWeekPattern  map[string]string       `json:"day_of_week_pattern"` // 工作日/周末偏好
	SeasonalPattern  map[string]string       `json:"seasonal_pattern"`   // 季节偏好
}

// InteractionHistory 交互历史（用于协同过滤）
type InteractionHistory struct {
	PositiveItems    []string                 `json:"positive_items"`    // 喜欢的项目
	NegativeItems    []string                 `json:"negative_items"`    // 不喜欢
	SkippedItems     []string                 `json:"skipped_items"`      // 跳过
	LastInteraction  time.Time                `json:"last_interaction"`
}

// GlobalMediaStats 全局媒体统计（用于热门推荐）
type GlobalMediaStats struct {
	PopularMovies   []string                 `json:"popular_movies"`
	PopularTVShows   []string                 `json:"popular_tv_shows"`
	TrendingGenres   []string                 `json:"trending_genres"`
	NewReleases      []string                 `json:"new_releases"`
	LastUpdate       time.Time                `json:"last_update"`
}

// RecommendationRequest 推荐请求
type RecommendationRequestV2 struct {
	UserID           int64                    `json:"user_id"`
	Count            int                      `json:"count"`
	Strategy         RecommendStrategy        `json:"strategy"`
	Context          string                   `json:"context"`           // mood/situation
	MediaType        string                   `json:"media_type"`        // movie/tv/both
	ExcludeSeen      bool                     `json:"exclude_seen"`
	MinRating        float64                  `json:"min_rating"`
}

// RecommendStrategy 推荐策略
type RecommendStrategy string

const (
	StrategyPersonalized  RecommendStrategy = "personalized"   // 个性化推荐
	StrategyTrending      RecommendStrategy = "trending"       // 热门推荐
	StrategySimilar       RecommendStrategy = "similar"        // 相似推荐
	StrategyDiscovery     RecommendStrategy = "discovery"      // 发现推荐（探索新内容）
	StrategyMood          RecommendStrategy = "mood"           // 心情推荐
	StrategySocial        RecommendStrategy = "social"         // 社交推荐（群组热门）
	StrategyHybrid        RecommendStrategy = "hybrid"         // 混合策略
)

// RecommendationResult 推荐结果（增强版）
type RecommendationResultV2 struct {
	// 基本信息
	Title            string                   `json:"title"`
	OriginalTitle    string                   `json:"original_title"`
	Year             int                      `json:"year"`
	MediaType        string                   `json:"media_type"`
	Genre            []string                 `json:"genre"`
	Rating           float64                  `json:"rating"`
	TmdbID           int                      `json:"tmdb_id"`
	PosterPath       string                   `json:"poster_path"`
	Overview         string                   `json:"overview"`

	// 推荐理由（核心）
	Reason           string                   `json:"reason"`           // 用户友好的推荐理由
	Why              []string                 `json:"why"`              // 详细解释（AI 生成）
	Confidence       float64                  `json:"confidence"`       // 推荐置信度 0-1

	// 标签
	Tags             []string                 `json:"tags"`             // 如：["和你看过的X相似", "符合你现在的心情"]
	MatchReasons     map[string]float64       `json:"match_reasons"`    // 各维度的匹配度

	// 交互
	QuickActions     []string                 `json:"quick_actions"`    // 快捷操作
}

// NewRecommendationEngine 创建新的推荐引擎
func NewRecommendationEngine(zhipu *ZhipuClient, trendingMgr *TrendingAIManager) *RecommendationEngine {
	return &RecommendationEngine{
		zhipu:       zhipu,
		userProfiles: make(map[int64]*UserProfileV2),
		globalStats: &GlobalMediaStats{
			PopularMovies:  []string{},
			PopularTVShows:  []string{},
			TrendingGenres:  []string{},
			NewReleases:    []string{},
			LastUpdate:     time.Time{},
		},
		trendingMgr: trendingMgr,
		enabled:     zhipu != nil && zhipu.IsEnabled(),
	}
}

// ============================================
// 核心推荐方法
// ============================================

// Recommend 主推荐入口（智能路由）
func (e *RecommendationEngine) Recommend(req *RecommendationRequestV2) ([]*RecommendationResultV2, error) {
	if !e.enabled {
		return nil, fmt.Errorf("recommendation engine not enabled")
	}

	// 确保用户画像存在
	e.ensureUserProfile(req.UserID)

	// 如果没有指定策略，自动选择
	if req.Strategy == "" {
		req.Strategy = e.selectStrategy(req.UserID)
	}

	logger.Info("[RecEngine] User %d: strategy=%s, context=%s", req.UserID, req.Strategy, req.Context)

	switch req.Strategy {
	case StrategyPersonalized:
		return e.personalizedRecommend(req)
	case StrategyTrending:
		return e.trendingRecommend(req)
	case StrategyMood:
		return e.moodBasedRecommend(req)
	case StrategyDiscovery:
		return e.discoveryRecommend(req)
	case StrategyHybrid:
		return e.hybridRecommend(req)
	default:
		return e.personalizedRecommend(req)
	}
}

// selectStrategy 根据用户状态自动选择推荐策略
func (e *RecommendationEngine) selectStrategy(userID int64) RecommendStrategy {
	e.profileMutex.RLock()
	profile, exists := e.userProfiles[userID]
	e.profileMutex.RUnlock()

	if !exists {
		return StrategyTrending // 新用户先用热门
	}

	// 检查用户是否有足够数据
	if len(profile.Behavior.SearchQueries) < 3 && profile.Behavior.RequestCount < 3 {
		return StrategyTrending // 数据不足用热门
	}

	// 检查当前是否有明确的心情
	if profile.Context.CurrentMood != "" && time.Since(profile.Context.LastMoodUpdate) < 2*time.Hour {
		return StrategyMood // 有明确心情用心情推荐
	}

	// 检查是否是发现新内容的好时机
	lastDiscovery := profile.Interaction.LastInteraction
	if time.Since(lastDiscovery) > 7*24*time.Hour {
		return StrategyDiscovery // 一周没交互了，尝试发现
	}

	return StrategyPersonalized // 默认个性化
}

// personalizedRecommend 个性化推荐（核心算法）
func (e *RecommendationEngine) personalizedRecommend(req *RecommendationRequestV2) ([]*RecommendationResultV2, error) {
	e.profileMutex.RLock()
	profile := e.userProfiles[req.UserID]
	e.profileMutex.RUnlock()

	// 构建推荐查询
	query := e.buildPersonalizationQuery(profile, req)

	// 调用 AI 生成推荐
	results, err := e.aiGenerateRecommendations(query)
	if err != nil {
		return nil, err
	}

	// 为每个结果添加匹配度分析
	for _, result := range results {
		result.MatchReasons = e.calculateMatchReasons(profile, result)
		result.Tags = e.generateTags(profile, result)
	}

	return results, nil
}

// buildPersonalizationQuery 构建个性化查询（发送给 AI）
func (e *RecommendationEngine) buildPersonalizationQuery(profile *UserProfileV2, req *RecommendationRequestV2) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("【推荐数量】%d部", req.Count))

	// 用户画像摘要
	parts = append(parts, "\n【用户画像】")
	parts = append(parts, fmt.Sprintf("- 观影人格: %s", profile.AIPersona))
	if profile.AITags != nil && len(profile.AITags) > 0 {
		parts = append(parts, fmt.Sprintf("- 用户标签: %s", strings.Join(profile.AITags, ", ")))
	}

	// 偏好
	if len(profile.Preferences.FavoriteGenres) > 0 {
		parts = append(parts, fmt.Sprintf("- 喜欢类型: %s", strings.Join(profile.Preferences.FavoriteGenres, ", ")))
	}
	if len(profile.Preferences.DislikedGenres) > 0 {
		parts = append(parts, fmt.Sprintf("- 不喜欢类型: %s", strings.Join(profile.Preferences.DislikedGenres, ", ")))
	}

	// 行为
	if profile.Behavior.RequestCount > 0 {
		parts = append(parts, fmt.Sprintf("- 请求次数: %d", profile.Behavior.RequestCount))
	}

	// 偏好年份
	if profile.Behavior.RequestYears != nil && len(profile.Behavior.RequestYears) > 0 {
		topYear := e.getTopYear(profile.Behavior.RequestYears)
		if topYear > 0 {
			parts = append(parts, fmt.Sprintf("- 偏好年份: %d年代", topYear/10*10))
		}
	}

	// 偏好类型
	if profile.Behavior.RequestGenres != nil && len(profile.Behavior.RequestGenres) > 0 {
		topGenre := e.getTopGenre(profile.Behavior.RequestGenres)
		if topGenre != "" {
			parts = append(parts, fmt.Sprintf("- 最爱类型: %s", topGenre))
		}
	}

	// 上下文
	if req.Context != "" {
		parts = append(parts, fmt.Sprintf("- 当前场景: %s", req.Context))
	} else if profile.Context.CurrentMood != "" {
		parts = append(parts, fmt.Sprintf("- 当前心情: %s", profile.Context.CurrentMood))
	}

	// 媒体类型限制
	if req.MediaType != "" && req.MediaType != "both" {
		typeName := "电影"
		if req.MediaType == "tv" {
			typeName = "剧集"
		}
		parts = append(parts, fmt.Sprintf("- 只要%s", typeName))
	}

	// 评分要求
	if req.MinRating > 0 {
		parts = append(parts, fmt.Sprintf("- 最低评分: %.1f", req.MinRating))
	} else if profile.Preferences.MinRating > 0 {
		parts = append(parts, fmt.Sprintf("- 最低评分: %.1f", profile.Preferences.MinRating))
	}

	return strings.Join(parts, "\n")
}

// aiGenerateRecommendations 调用 AI 生成推荐
func (e *RecommendationEngine) aiGenerateRecommendations(query string) ([]*RecommendationResultV2, error) {
	if e.zhipu == nil || !e.zhipu.IsEnabled() {
		return nil, fmt.Errorf("AI client not available")
	}

	currentTime := time.Now().Format("2006-01-02")

	systemPrompt := fmt.Sprintf(`你是小凛的推荐引擎核心，负责生成个性化影视推荐。

【任务】根据用户画像生成精准推荐。

【返回格式】纯JSON数组：
[
  {
    "title": "片名",
    "original_title": "原始片名",
    "year": 2024,
    "media_type": "movie/tv",
    "genre": ["类型1", "类型2"],
    "rating": 8.5,
    "tmdb_id": 123,
    "reason": "推荐理由（20字内，结合用户画像）",
    "why": ["理由1", "理由2", "理由3"],
    "confidence": 0.9
  }
]

【推荐原则】
1. 优先考虑用户历史偏好
2. 平衡熟悉和新鲜内容
3. 推荐理由要具体、个性化
4. confidence 表示推荐置信度(0-1)
5. genre 必须是数组
6. tmdb_id 尽量准确（用于后续查询）

当前时间: %s`, currentTime)

	response, err := e.zhipu.Send(query, systemPrompt)
	if err != nil {
		return nil, err
	}

	return e.parseRecommendationResults(response)
}

// trendingRecommend 热门推荐
func (e *RecommendationEngine) trendingRecommend(req *RecommendationRequestV2) ([]*RecommendationResultV2, error) {
	if e.trendingMgr == nil || !e.trendingMgr.IsEnabled() {
		return nil, fmt.Errorf("trending manager not available")
	}

	var results []*TrendingResult
	var err error

	// 根据媒体类型获取热门
	if req.MediaType == "tv" {
		results, err = e.trendingMgr.GetHotTVShows(req.Count)
	} else {
		results, err = e.trendingMgr.GetTrendingMovies(req.Count)
	}

	if err != nil {
		return nil, err
	}

	// 转换为推荐结果格式
	recResults := make([]*RecommendationResultV2, 0, len(results))
	for _, item := range results {
		recResults = append(recResults, &RecommendationResultV2{
			Title:         item.Title,
			Year:          item.Year,
			MediaType:     item.MediaType,
			Genre:         []string{item.Genre},
			Rating:        item.Rating,
			TmdbID:        item.TmdbID,
			Reason:        item.Reason,
			Why:           []string{"当前热门", "评分不错", "值得一看"},
			Confidence:    0.8,
			Tags:          []string{"热门", "新片"},
			QuickActions:  []string{"立即请求", "查看详情"},
		})
	}

	return recResults, nil
}

// moodBasedRecommend 心情推荐（增强版）
func (e *RecommendationEngine) moodBasedRecommend(req *RecommendationRequestV2) ([]*RecommendationResultV2, error) {
	e.profileMutex.RLock()
	profile := e.userProfiles[req.UserID]
	e.profileMutex.RUnlock()

	// 解析心情（支持复杂的心情描述）
	moodAnalysis := e.analyzeMood(req.Context, profile)

	// 构建增强的查询
	query := e.buildMoodQuery(moodAnalysis, profile, req)

	results, err := e.aiGenerateRecommendations(query)
	if err != nil {
		return nil, err
	}

	// 为每个结果添加心情相关标签
	for _, result := range results {
		result.Tags = append(result.Tags, moodAnalysis.MoodCategory, "心情推荐")
		if moodAnalysis.ShouldIncludeNew {
			result.Tags = append(result.Tags, "新鲜感")
		}
	}

	return results, nil
}

// MoodAnalysis 心情分析结果
type MoodAnalysis struct {
	// 基础心情分类
	MoodCategory    string   `json:"mood_category"`    // 主要分类
	MoodSubCategory  string   `json:"mood_sub_category"` // 子分类

	// 推荐特征
	PreferredGenres  []string `json:"preferred_genres"`  // 推荐类型
	PreferredTones   []string `json:"preferred_tones"`   // 推荐基调
	MinRating        float64  `json:"min_rating"`        // 最低评分要求
	Pace             string   `json:"pace"`              // 节奏：快/中/慢
	IncludeNew       bool     `json:"include_new"`       // 是否包含新片
	ShouldIncludeNew bool     `json:"should_include_new"`// 建议包含新片
	YearPreference   string   `json:"year_preference"`   // 年份偏好
	DurationHint     string   `json:"duration_hint"`     // 时长建议

	// 上下文因素
	TimeOfDay        string   `json:"time_of_day"`       // 时间段
	IsLateNight      bool     `json:"is_late_night"`     // 是否深夜
	Weekend          bool     `json:"weekend"`           // 是否周末
}

// analyzeMood 分析用户心情（多维度解析）
func (e *RecommendationEngine) analyzeMood(input string, profile *UserProfileV2) *MoodAnalysis {
	analysis := &MoodAnalysis{
		TimeOfDay:   e.getTimeOfDay(),
		IsLateNight: e.isLateNight(),
		Weekend:     e.isWeekend(),
		MinRating:   6.5,
	}

	// 如果没有输入，使用用户当前心情或默认心情
	moodInput := input
	if moodInput == "" {
		moodInput = profile.Context.CurrentMood
	}
	if moodInput == "" {
		moodInput = "放松"
	}

	// 解析心情关键词
	moodLower := strings.ToLower(moodInput)

	// 心情分类映射（增强版）
	moodMappings := map[string]*MoodAnalysis{
		// === 开心类 ===
		"开心": {
			MoodCategory:   "开心",
			PreferredGenres: []string{"喜剧", "动画", "音乐", "家庭"},
			PreferredTones:  []string{"轻松", "愉快", "正能量", "温馨"},
			Pace:            "中",
			IncludeNew:      true,
			YearPreference:  "近三年",
			DurationHint:    "90-120分钟",
		},
		"快乐": {
			MoodCategory:   "开心",
			PreferredGenres: []string{"喜剧", "动画", "冒险", "音乐"},
			PreferredTones:  []string{"轻松", "愉快", "正能量"},
			Pace:            "快",
			IncludeNew:      true,
		},
		"愉快": {
			MoodCategory:   "开心",
			PreferredGenres: []string{"喜剧", "爱情", "家庭"},
			PreferredTones:  []string{"温馨", "轻松"},
			Pace:            "慢",
		},

		// === 难过/治愈类 ===
		"难过": {
			MoodCategory:   "难过",
			PreferredGenres: []string{"喜剧", "温情", "治愈", "动画"},
			PreferredTones:  []string{"温暖", "治愈", "励志", "正能量"},
			Pace:            "慢",
			MinRating:       7.0,
			IncludeNew:      false,
			YearPreference:  "经典",
		},
		"沮丧": {
			MoodCategory:   "难过",
			PreferredGenres: []string{"励志", "传记", "剧情"},
			PreferredTones:  []string{"励志", "温暖", "治愈"},
			Pace:            "中",
			MinRating:       7.5,
		},
		"郁闷": {
			MoodCategory:   "难过",
			PreferredGenres: []string{"喜剧", "治愈", "动画"},
			PreferredTones:  []string{"轻松", "温暖", "治愈"},
		},
		"治愈": {
			MoodCategory:   "治愈",
			PreferredGenres: []string{"温情", "动画", "剧情", "家庭"},
			PreferredTones:  []string{"温暖", "治愈", "宁静"},
			Pace:            "慢",
		},

		// === 紧张/刺激类 ===
		"紧张": {
			MoodCategory:   "紧张",
			PreferredGenres: []string{"悬疑", "惊悚", "动作", "犯罪"},
			PreferredTones:  []string{"紧张", "刺激", "烧脑"},
			Pace:            "快",
			MinRating:       6.0,
			IncludeNew:      true,
		},
		"焦虑": {
			MoodCategory:   "紧张",
			PreferredGenres: []string{"悬疑", "惊悚", "犯罪"},
			PreferredTones:  []string{"烧脑", "紧张"},
			Pace:            "快",
		},
		"刺激": {
			MoodCategory:   "刺激",
			PreferredGenres: []string{"恐怖", "惊悚", "动作", "冒险"},
			PreferredTones:  []string{"刺激", "紧张", "惊悚"},
			Pace:            "快",
		},
		"恐怖": {
			MoodCategory:   "刺激",
			PreferredGenres: []string{"恐怖", "惊悚", "悬疑"},
			PreferredTones:  []string{"恐怖", "惊悚"},
			Pace:            "快",
		},

		// === 无聊类 ===
		"无聊": {
			MoodCategory:   "无聊",
			PreferredGenres: []string{"惊悚", "科幻", "冒险", "悬疑", "动作"},
			PreferredTones:  []string{"刺激", "烧脑", "反转"},
			Pace:            "快",
			MinRating:       7.0,
			IncludeNew:      true,
		},
		"没劲": {
			MoodCategory:   "无聊",
			PreferredGenres: []string{"喜剧", "科幻", "冒险"},
			PreferredTones:  []string{"有趣", "新奇"},
		},

		// === 放松类 ===
		"放松": {
			MoodCategory:   "放松",
			PreferredGenres: []string{"喜剧", "爱情", "动画", "纪录片"},
			PreferredTones:  []string{"轻松", "温馨", "治愈"},
			Pace:            "慢",
			MinRating:       6.5,
		},
		"休闲": {
			MoodCategory:   "放松",
			PreferredGenres: []string{"喜剧", "爱情", "家庭"},
			PreferredTones:  []string{"轻松", "温馨"},
			Pace:            "慢",
		},
		"舒适": {
			MoodCategory:   "放松",
			PreferredGenres: []string{"动画", "纪录片", "剧情"},
			PreferredTones:  []string{"宁静", "治愈"},
			Pace:            "慢",
		},

		// === 兴奋类 ===
		"兴奋": {
			MoodCategory:   "兴奋",
			PreferredGenres: []string{"动作", "科幻", "冒险", "超级英雄"},
			PreferredTones:  []string{"热血", "刺激", "震撼"},
			Pace:            "快",
			IncludeNew:      true,
			YearPreference:  "近期大片",
		},
		"热血": {
			MoodCategory:   "兴奋",
			PreferredGenres: []string{"动作", "冒险", "超级英雄"},
			PreferredTones:  []string{"热血", "励志"},
			Pace:            "快",
		},

		// === 思考类 ===
		"思考": {
			MoodCategory:   "思考",
			PreferredGenres: []string{"科幻", "悬疑", "剧情", "传记"},
			PreferredTones:  []string{"烧脑", "深度", "哲学"},
			Pace:            "中",
			MinRating:       7.5,
		},
		"烧脑": {
			MoodCategory:   "思考",
			PreferredGenres: []string{"悬疑", "科幻", "惊悚"},
			PreferredTones:  []string{"烧脑", "反转", "复杂"},
			Pace:            "中",
			MinRating:       7.0,
		},
		"学习": {
			MoodCategory:   "思考",
			PreferredGenres: []string{"纪录片", "传记", "历史", "科普"},
			PreferredTones:  []string{"知识", "深度", "教育"},
			Pace:            "中",
		},

		// === 浪漫类 ===
		"浪漫": {
			MoodCategory:   "浪漫",
			PreferredGenres: []string{"爱情", "剧情", "浪漫"},
			PreferredTones:  []string{"浪漫", "温馨", "甜蜜"},
			Pace:            "慢",
		},
		"甜蜜": {
			MoodCategory:   "浪漫",
			PreferredGenres: []string{"爱情", "喜剧", "动画"},
			PreferredTones:  []string{"甜蜜", "温馨"},
			Pace:            "慢",
		},
		"失恋": {
			MoodCategory:   "难过",
			PreferredGenres: []string{"励志", "治愈", "喜剧"},
			PreferredTones:  []string{"治愈", "正能量", "温暖"},
			Pace:            "慢",
			MinRating:       7.0,
		},

		// === 怀旧类 ===
		"怀旧": {
			MoodCategory:   "怀旧",
			PreferredGenres: []string{"经典", "剧情", "家庭"},
			PreferredTones:  []string{"怀旧", "经典", "回忆"},
			Pace:            "慢",
			IncludeNew:      false,
			YearPreference:  "90年代-2000年代",
		},
		"回忆": {
			MoodCategory:   "怀旧",
			PreferredGenres: []string{"经典", "剧情"},
			PreferredTones:  []string{"怀旧", "温暖"},
			YearPreference:  "经典老片",
		},

		// === 愤怒/发泄类 ===
		"生气": {
			MoodCategory:   "愤怒",
			PreferredGenres: []string{"动作", "犯罪", "惊悚"},
			PreferredTones:  []string{"爽片", "解压", "动作"},
			Pace:            "快",
		},
		"愤怒": {
			MoodCategory:   "愤怒",
			PreferredGenres: []string{"动作", "犯罪", "复仇"},
			PreferredTones:  []string{"爽片", "解压"},
			Pace:            "快",
		},
		"解压": {
			MoodCategory:   "放松",
			PreferredGenres: []string{"动作", "喜剧", "爽片"},
			PreferredTones:  []string{"解压", "爽快"},
			Pace:            "快",
		},

		// === 孤独类 ===
		"孤独": {
			MoodCategory:   "孤独",
			PreferredGenres: []string{"剧情", "温情", "治愈", "动画"},
			PreferredTones:  []string{"温暖", "陪伴感", "治愈"},
			Pace:            "慢",
		},
		"寂寞": {
			MoodCategory:   "孤独",
			PreferredGenres: []string{"爱情", "剧情", "温情"},
			PreferredTones:  []string{"温暖", "治愈"},
		},

		// === 困倦类 ===
		"困": {
			MoodCategory:   "困倦",
			PreferredGenres: []string{"喜剧", "动画", "轻松剧情"},
			PreferredTones:  []string{"轻松", "不需要太烧脑"},
			Pace:            "慢",
			MinRating:       6.0,
			DurationHint:    "90分钟以内",
		},
		"累了": {
			MoodCategory:   "困倦",
			PreferredGenres: []string{"喜剧", "治愈", "动画"},
			PreferredTones:  []string{"轻松", "治愈"},
			Pace:            "慢",
		},
		"疲劳": {
			MoodCategory:   "困倦",
			PreferredGenres: []string{"喜剧", "纪录片", "动画"},
			PreferredTones:  []string{"轻松", "治愈"},
		},

		// === 探索类 ===
		"探索": {
			MoodCategory:   "探索",
			PreferredGenres: []string{"科幻", "纪录片", "冒险"},
			PreferredTones:  []string{"新奇", "知识", "探索"},
			Pace:            "中",
			IncludeNew:      true,
		},
		"好奇": {
			MoodCategory:   "探索",
			PreferredGenres: []string{"科幻", "悬疑", "纪录片"},
			PreferredTones:  []string{"新奇", "烧脑"},
		},
	}

	// 精确匹配
	if mapping, ok := moodMappings[moodInput]; ok {
		analysis.mergeFrom(mapping)
	} else {
		// 模糊匹配
		found := false
		for key, mapping := range moodMappings {
			if strings.Contains(moodLower, key) || strings.Contains(key, moodInput) {
				analysis.mergeFrom(mapping)
				found = true
				break
			}
		}
		if !found {
			// 默认映射
			analysis.mergeFrom(moodMappings["放松"])
		}
	}

	// 根据时间段调整
	e.adjustByTimeOfDay(analysis)

	// 根据用户历史偏好调整
	e.adjustByUserPreference(analysis, profile)

	return analysis
}

// mergeFrom 从另一个 MoodAnalysis 合并数据
func (a *MoodAnalysis) mergeFrom(other *MoodAnalysis) {
	if other.MoodCategory != "" && a.MoodCategory == "" {
		a.MoodCategory = other.MoodCategory
	}
	if other.MoodSubCategory != "" {
		a.MoodSubCategory = other.MoodSubCategory
	}
	if len(other.PreferredGenres) > 0 {
		a.PreferredGenres = other.PreferredGenres
	}
	if len(other.PreferredTones) > 0 {
		a.PreferredTones = other.PreferredTones
	}
	if other.MinRating > 0 && a.MinRating == 0 {
		a.MinRating = other.MinRating
	}
	if other.Pace != "" && a.Pace == "" {
		a.Pace = other.Pace
	}
	if other.YearPreference != "" {
		a.YearPreference = other.YearPreference
	}
	if other.DurationHint != "" {
		a.DurationHint = other.DurationHint
	}
	a.ShouldIncludeNew = other.IncludeNew
}

// adjustByTimeOfDay 根据时间段调整推荐
func (e *RecommendationEngine) adjustByTimeOfDay(analysis *MoodAnalysis) {
	if analysis.IsLateNight {
		// 深夜：避免恐怖、过于刺激的内容
		if analysis.MoodCategory == "刺激" || analysis.MoodCategory == "紧张" {
			// 转向悬疑/犯罪类
			newGenres := []string{}
			for _, g := range analysis.PreferredGenres {
				if g != "恐怖" && g != "惊悚" {
					newGenres = append(newGenres, g)
				}
			}
			if len(newGenres) > 0 {
				analysis.PreferredGenres = newGenres
			}
		}
		// 深夜推荐节奏较慢的内容
		if analysis.Pace == "快" {
			analysis.Pace = "中"
		}
	}

	switch analysis.TimeOfDay {
	case "早上":
		// 早上适合励志、积极的内容
		if analysis.MoodCategory == "放松" {
			analysis.PreferredTones = append(analysis.PreferredTones, "励志", "积极")
		}
	case "晚上":
		// 晚上可以看更长的内容
		if analysis.DurationHint == "" {
			analysis.DurationHint = "不限"
		}
	}
}

// adjustByUserPreference 根据用户历史偏好调整
func (e *RecommendationEngine) adjustByUserPreference(analysis *MoodAnalysis, profile *UserProfileV2) {
	if profile == nil {
		return
	}

	// 如果用户有明确的类型偏好，尝试融合
	if len(profile.Preferences.FavoriteGenres) > 0 {
		// 检查是否有重叠
		hasOverlap := false
		for _, userGenre := range profile.Preferences.FavoriteGenres {
			for _, moodGenre := range analysis.PreferredGenres {
				if strings.Contains(moodGenre, userGenre) || strings.Contains(userGenre, moodGenre) {
					hasOverlap = true
					break
				}
			}
		}

		// 如果有重叠，考虑添加用户喜欢的其他类型
		if hasOverlap && len(analysis.PreferredGenres) < 5 {
			for _, userGenre := range profile.Preferences.FavoriteGenres {
				add := true
				for _, existing := range analysis.PreferredGenres {
					if strings.Contains(existing, userGenre) || strings.Contains(userGenre, existing) {
						add = false
						break
					}
				}
				if add {
					analysis.PreferredGenres = append(analysis.PreferredGenres, userGenre)
					if len(analysis.PreferredGenres) >= 5 {
						break
					}
				}
			}
		}
	}

	// 调整最低评分
	if profile.Preferences.MinRating > analysis.MinRating {
		analysis.MinRating = profile.Preferences.MinRating
	}

	// 根据用户怀旧倾向调整
	if profile.Preferences.NostalgiaTendency > 0.6 && analysis.YearPreference == "" {
		analysis.YearPreference = "包含经典作品"
	}
	if profile.Preferences.NostalgiaTendency < 0.3 && analysis.YearPreference == "" {
		analysis.IncludeNew = true
	}
}

// buildMoodQuery 构建心情推荐查询
func (e *RecommendationEngine) buildMoodQuery(analysis *MoodAnalysis, profile *UserProfileV2, req *RecommendationRequestV2) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("【心情推荐】"))
	parts = append(parts, fmt.Sprintf("- 心情分类: %s", analysis.MoodCategory))
	if analysis.MoodSubCategory != "" {
		parts = append(parts, fmt.Sprintf("- 细分心情: %s", analysis.MoodSubCategory))
	}
	parts = append(parts, fmt.Sprintf("- 推荐类型: %s", strings.Join(analysis.PreferredGenres, "、")))
	if len(analysis.PreferredTones) > 0 {
		parts = append(parts, fmt.Sprintf("- 内容基调: %s", strings.Join(analysis.PreferredTones, "、")))
	}
	parts = append(parts, fmt.Sprintf("- 推荐节奏: %s", analysis.Pace))
	parts = append(parts, fmt.Sprintf("- 最低评分: %.1f", analysis.MinRating))

	// 时间上下文
	if analysis.IsLateNight {
		parts = append(parts, "- 时间: 深夜，推荐适合深夜观看的内容")
	}
	if analysis.Weekend {
		parts = append(parts, "- 时间: 周末，可以推荐较长作品")
	}

	// 年份偏好
	if analysis.YearPreference != "" {
		parts = append(parts, fmt.Sprintf("- 年份偏好: %s", analysis.YearPreference))
	} else if analysis.IncludeNew {
		parts = append(parts, "- 包含新片: 是")
	}

	// 时长建议
	if analysis.DurationHint != "" {
		parts = append(parts, fmt.Sprintf("- 建议时长: %s", analysis.DurationHint))
	}

	// 媒体类型限制
	if req.MediaType != "" && req.MediaType != "both" {
		typeName := "电影"
		if req.MediaType == "tv" {
			typeName = "剧集"
		}
		parts = append(parts, fmt.Sprintf("- 只要%s", typeName))
	}

	// 用户画像补充（如果有的话）
	if profile != nil && len(profile.Behavior.RequestGenres) > 0 {
		topGenre := e.getTopGenre(profile.Behavior.RequestGenres)
		if topGenre != "" {
			parts = append(parts, fmt.Sprintf("- 用户常看: %s", topGenre))
		}
	}

	parts = append(parts, fmt.Sprintf("- 推荐数量: %d", req.Count))

	return strings.Join(parts, "\n")
}

// getTimeOfDay 获取当前时间段
func (e *RecommendationEngine) getTimeOfDay() string {
	hour := time.Now().Hour()
	switch {
	case hour >= 6 && hour < 9:
		return "早上"
	case hour >= 9 && hour < 12:
		return "上午"
	case hour >= 12 && hour < 14:
		return "中午"
	case hour >= 14 && hour < 18:
		return "下午"
	case hour >= 18 && hour < 22:
		return "晚上"
	case hour >= 22 || hour < 2:
		return "深夜"
	default:
		return "凌晨"
	}
}

// isLateNight 判断是否是深夜
func (e *RecommendationEngine) isLateNight() bool {
	hour := time.Now().Hour()
	return hour >= 23 || hour < 6
}

// isWeekend 判断是否是周末
func (e *RecommendationEngine) isWeekend() bool {
	weekday := time.Now().Weekday()
	return weekday == time.Saturday || weekday == time.Sunday
}

// discoveryRecommend 发现推荐（探索新内容）
func (e *RecommendationEngine) discoveryRecommend(req *RecommendationRequestV2) ([]*RecommendationResultV2, error) {
	e.profileMutex.RLock()
	profile := e.userProfiles[req.UserID]
	e.profileMutex.RUnlock()

	// 分析用户探索倾向
	openness := 0.5
	if profile.Preferences.Openness > 0 {
		openness = profile.Preferences.Openness
	}

	// 构建发现查询（推荐用户没接触过的内容）
	query := fmt.Sprintf(`【探索发现推荐】
- 用户探索意愿: %.0f%%
- 偏好类型: %s
- 推荐数量: %d
- 目标：推荐用户可能喜欢但未接触过的内容
- 包含：不同国家、不同时代、不同类型的高质量作品
`, openness*100,
		func() string {
			if len(profile.Preferences.FavoriteGenres) > 0 {
				return strings.Join(profile.Preferences.FavoriteGenres, ", ")
			}
			return "不限"
		}(),
		req.Count)

	results, err := e.aiGenerateRecommendations(query)
	if err != nil {
		return nil, err
	}

	// 添加发现标签
	for _, r := range results {
		r.Tags = append(r.Tags, "发现", "新体验")
	}

	return results, nil
}

// hybridRecommend 混合推荐（综合多种策略）
func (e *RecommendationEngine) hybridRecommend(req *RecommendationRequestV2) ([]*RecommendationResultV2, error) {
	// 40% 个性化 + 30% 热门 + 20% 心情 + 10% 发现
	allResults := make([]*RecommendationResultV2, 0)

	// 个性化
	personalCount := req.Count * 2 / 5
	if personalCount > 0 {
		personalReq := *req
		personalReq.Count = personalCount
		personalReq.Strategy = StrategyPersonalized
		if results, err := e.personalizedRecommend(&personalReq); err == nil {
			allResults = append(allResults, results...)
		}
	}

	// 热门
	trendingCount := req.Count * 3 / 10
	if trendingCount > 0 {
		trendingReq := *req
		trendingReq.Count = trendingCount
		trendingReq.Strategy = StrategyTrending
		if results, err := e.trendingRecommend(&trendingReq); err == nil {
			allResults = append(allResults, results...)
		}
	}

	// 心情
	moodCount := req.Count * 2 / 10
	if moodCount > 0 {
		moodReq := *req
		moodReq.Count = moodCount
		moodReq.Strategy = StrategyMood
		if results, err := e.moodBasedRecommend(&moodReq); err == nil {
			allResults = append(allResults, results...)
		}
	}

	// 发现
	discoveryCount := req.Count / 10
	if discoveryCount > 0 {
		discoveryReq := *req
		discoveryReq.Count = discoveryCount
		discoveryReq.Strategy = StrategyDiscovery
		if results, err := e.discoveryRecommend(&discoveryReq); err == nil {
			allResults = append(allResults, results...)
		}
	}

	// 去重并排序
	return e.deduplicateAndRank(allResults, req.Count)
}

// ============================================
// 用户画像管理
// ============================================

// ensureUserProfile 确保用户画像存在
func (e *RecommendationEngine) ensureUserProfile(userID int64) {
	e.profileMutex.Lock()
	defer e.profileMutex.Unlock()

	if _, exists := e.userProfiles[userID]; !exists {
		e.userProfiles[userID] = &UserProfileV2{
			UserID:          userID,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
			Behavior:        &UserBehavior{
				SearchQueries:   []string{},
				SearchFreq:      make(map[string]int),
				RequestTypes:    make(map[string]int),
				RequestGenres:   make(map[string]int),
				RequestYears:    make(map[int]int),
			},
			Preferences:     &UserPreferencesV2{
				FavoriteGenres:    []string{},
				DislikedGenres:    []string{},
				FavoriteActors:    []string{},
				FavoriteDirectors: []string{},
				Language:          "both",
				InferredMoods:     []string{},
				InferredThemes:    []string{},
				Openness:          0.5,
				NostalgiaTendency: 0.3,
				MinRating:         6.0,
			},
			Context:         &UserContext{
				CurrentMood:       "",
				TimeOfDayPattern:  make(map[string]string),
				DayOfWeekPattern:  make(map[string]string),
				SeasonalPattern:   make(map[string]string),
			},
			Interaction:     &InteractionHistory{
				PositiveItems:    []string{},
				NegativeItems:    []string{},
				SkippedItems:     []string{},
				LastInteraction:  time.Now(),
			},
			AITags:          []string{},
			AIPersona:       "探索者",
			LastAIAnalysis:  time.Time{},
		}
		logger.Info("[RecEngine] Created profile for user %d", userID)
	}
}

// RecordInteraction 记录用户交互（核心学习功能）
func (e *RecommendationEngine) RecordInteraction(userID int64, interactionType string, data map[string]interface{}) {
	e.ensureUserProfile(userID)

	e.profileMutex.Lock()
	defer e.profileMutex.Unlock()

	profile := e.userProfiles[userID]
	profile.UpdatedAt = time.Now()

	switch interactionType {
	case "search":
		query, _ := data["query"].(string)
		if query != "" {
			// 更新搜索历史
			profile.Behavior.SearchQueries = appendToSlice(profile.Behavior.SearchQueries, query, 20)
			profile.Behavior.SearchFreq[query]++
		}

	case "request":
		mediaType, _ := data["media_type"].(string)
		genre, _ := data["genre"].(string)
		year, _ := data["year"].(int)

		profile.Behavior.RequestCount++
		if mediaType != "" {
			profile.Behavior.RequestTypes[mediaType]++
		}
		if genre != "" {
			profile.Behavior.RequestGenres[genre]++
		}
		if year > 0 {
			decade := year / 10 * 10
			profile.Behavior.RequestYears[decade]++
		}

	case "feedback":
		sentiment, _ := data["sentiment"].(string) // positive/negative
		itemID, _ := data["item_id"].(string)

		if sentiment == "positive" {
			profile.Interaction.PositiveItems = appendToSlice(profile.Interaction.PositiveItems, itemID, 50)
		} else if sentiment == "negative" {
			profile.Interaction.NegativeItems = appendToSlice(profile.Interaction.NegativeItems, itemID, 50)
		}
		profile.Interaction.LastInteraction = time.Now()

	case "mood":
		mood, _ := data["mood"].(string)
		if mood != "" {
			profile.Context.CurrentMood = mood
			profile.Context.LastMoodUpdate = time.Now()

			// 记录时间-心情模式
			hour := time.Now().Hour()
			var timeSlot string
			switch {
			case hour >= 6 && hour < 12:
				timeSlot = "上午"
			case hour >= 12 && hour < 18:
				timeSlot = "下午"
			case hour >= 18 && hour < 24:
				timeSlot = "晚上"
			default:
				timeSlot = "深夜"
			}
			profile.Context.TimeOfDayPattern[timeSlot] = mood
		}
	}

	// 定期触发 AI 分析（每 10 次交互）
	if profile.Behavior.RequestCount%10 == 0 {
		go e.analyzeUserProfile(userID)
	}
}

// analyzeUserProfile AI 分析用户画像
func (e *RecommendationEngine) analyzeUserProfile(userID int64) {
	e.profileMutex.RLock()
	profile := e.userProfiles[userID]
	e.profileMutex.RUnlock()

	if e.zhipu == nil || !e.zhipu.IsEnabled() {
		return
	}

	// 构建分析查询
	query := fmt.Sprintf(`分析用户观影画像：

【行为统计】
- 搜索次数: %d
- 请求次数: %d
- 偏好类型: %v
- 偏好年份: %v
- 正面反馈: %d
- 负面反馈: %d

【任务】
1. 生成用户观影人格（如：悬疑迷、浪漫主义者、动作片狂热者）
2. 提炼 3-5 个用户标签
3. 推断用户的探索倾向（0-1）和怀旧倾向（0-1）

【返回JSON】
{
  "persona": "观影人格描述",
  "tags": ["标签1", "标签2", "标签3"],
  "openness": 0.7,
  "nostalgia": 0.3,
  "inferred_moods": ["心情1", "心情2"],
  "inferred_themes": ["主题1", "主题2"]
}`,
		profile.Behavior.RequestCount,
		len(profile.Behavior.SearchQueries),
		profile.Behavior.RequestGenres,
		profile.Behavior.RequestYears,
		len(profile.Interaction.PositiveItems),
		len(profile.Interaction.NegativeItems))

	response, err := e.zhipu.Send(query, "你是用户行为分析专家，负责分析观影偏好。")
	if err != nil {
		logger.Info("[RecEngine] AI analysis failed for user %d: %v", userID, err)
		return
	}

	// 解析并更新画像
	var analysis struct {
		Persona         string   `json:"persona"`
		Tags            []string `json:"tags"`
		Openness        float64  `json:"openness"`
		Nostalgia       float64  `json:"nostalgia"`
		InferredMoods   []string `json:"inferred_moods"`
		InferredThemes  []string `json:"inferred_themes"`
	}

	if err := json.Unmarshal([]byte(cleanAIResponse(response)), &analysis); err == nil {
		e.profileMutex.Lock()
		profile.AIPersona = analysis.Persona
		profile.AITags = analysis.Tags
		profile.Preferences.Openness = analysis.Openness
		profile.Preferences.NostalgiaTendency = analysis.Nostalgia
		if len(analysis.InferredMoods) > 0 {
			profile.Preferences.InferredMoods = analysis.InferredMoods
		}
		if len(analysis.InferredThemes) > 0 {
			profile.Preferences.InferredThemes = analysis.InferredThemes
		}
		profile.LastAIAnalysis = time.Now()
		e.profileMutex.Unlock()

		logger.Info("[RecEngine] Updated profile for user %d: persona=%s, tags=%v", userID, analysis.Persona, analysis.Tags)
	}
}

// ============================================
// 辅助方法
// ============================================

// parseRecommendationResults 解析推荐结果
func (e *RecommendationEngine) parseRecommendationResults(response string) ([]*RecommendationResultV2, error) {
	response = cleanAIResponse(response)

	var results []*RecommendationResultV2
	if err := json.Unmarshal([]byte(response), &results); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	return results, nil
}

// calculateMatchReasons 计算匹配原因
func (e *RecommendationEngine) calculateMatchReasons(profile *UserProfileV2, result *RecommendationResultV2) map[string]float64 {
	reasons := make(map[string]float64)

	// 类型匹配
	for _, genre := range result.Genre {
		for _, fav := range profile.Preferences.FavoriteGenres {
			if strings.Contains(genre, fav) || strings.Contains(fav, genre) {
				reasons["类型匹配"] = 0.9
				break
			}
		}
	}

	// 年份匹配
	if result.Year > 0 {
		for year := range profile.Behavior.RequestYears {
			if abs(result.Year-year) < 10 {
				reasons["年代偏好"] = 0.8
				break
			}
		}
	}

	// 评分匹配
	if result.Rating >= profile.Preferences.MinRating {
		reasons["评分符合"] = 0.7
	}

	// 心情匹配
	if profile.Context.CurrentMood != "" {
		reasons["符合当前心情"] = 0.8
	}

	return reasons
}

// generateTags 生成推荐标签
func (e *RecommendationEngine) generateTags(profile *UserProfileV2, result *RecommendationResultV2) []string {
	tags := []string{}

	// 添加基础标签
	if result.Rating >= 8.0 {
		tags = append(tags, "高评分")
	}
	if result.Year >= time.Now().Year()-1 {
		tags = append(tags, "新片")
	}

	// 根据用户画像添加标签
	for _, reason := range result.Why {
		if strings.Contains(reason, "相似") {
			tags = append(tags, "和你喜欢的相似")
		}
		if strings.Contains(reason, "导演") || strings.Contains(reason, "演员") {
			tags = append(tags, "班底偏好")
		}
	}

	return tags
}

// deduplicateAndRank 去重并排序
func (e *RecommendationEngine) deduplicateAndRank(results []*RecommendationResultV2, maxCount int) ([]*RecommendationResultV2, error) {
	// 去重
	seen := make(map[int]bool)
	unique := []*RecommendationResultV2{}
	for _, r := range results {
		if r.TmdbID > 0 && !seen[r.TmdbID] {
			seen[r.TmdbID] = true
			unique = append(unique, r)
		} else if r.TmdbID == 0 {
			unique = append(unique, r)
		}
	}

	// 按置信度排序
	for i := 0; i < len(unique); i++ {
		for j := i + 1; j < len(unique); j++ {
			if unique[j].Confidence > unique[i].Confidence {
				unique[i], unique[j] = unique[j], unique[i]
			}
		}
	}

	// 限制数量
	if len(unique) > maxCount {
		unique = unique[:maxCount]
	}

	return unique, nil
}

// getTopYear 获取偏好年份
func (e *RecommendationEngine) getTopYear(years map[int]int) int {
	maxCount := 0
	topYear := 0
	for year, count := range years {
		if count > maxCount {
			maxCount = count
			topYear = year
		}
	}
	return topYear
}

// getTopGenre 获取偏好类型
func (e *RecommendationEngine) getTopGenre(genres map[string]int) string {
	maxCount := 0
	topGenre := ""
	for genre, count := range genres {
		if count > maxCount {
			maxCount = count
			topGenre = genre
		}
	}
	return topGenre
}

// appendToSlice 添加到切片并限制长度
func appendToSlice(slice []string, item string, maxLen int) []string {
	slice = append(slice, item)
	if len(slice) > maxLen {
		slice = slice[len(slice)-maxLen:]
	}
	return slice
}

// abs 绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// cleanAIResponse 清理 AI 响应
func cleanAIResponse(response string) string {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)
	return response
}

// ============================================
// API 服务器端点（集成）
// ============================================

// StartRecommendationAPI 启动推荐 API 服务器
func (e *RecommendationEngine) StartRecommendationAPI(port string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/recommend", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 解析请求
		userID := r.URL.Query().Get("user_id")
		count := r.URL.Query().Get("count")
		strategy := r.URL.Query().Get("strategy")
		context := r.URL.Query().Get("context")

		if userID == "" {
			http.Error(w, "missing user_id", http.StatusBadRequest)
			return
		}

		uid := 0
		fmt.Sscanf(userID, "%d", &uid)

		reqCount := 5
		if count != "" {
			fmt.Sscanf(count, "%d", &reqCount)
		}

		req := &RecommendationRequestV2{
			UserID:    int64(uid),
			Count:     reqCount,
			Strategy:  RecommendStrategy(strategy),
			Context:   context,
			MediaType: "both",
		}

		results, err := e.Recommend(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(results)
	})

	mux.HandleFunc("/feedback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var data struct {
			UserID    int64  `json:"user_id"`
			ItemID    string `json:"item_id"`
			Sentiment string `json:"sentiment"`
		}

		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		e.RecordInteraction(data.UserID, "feedback", map[string]interface{}{
			"item_id":   data.ItemID,
			"sentiment": data.Sentiment,
		})

		w.Write([]byte(`{"status":"ok"}`))
	})

	logger.Info("[RecEngine] API server starting on port %s", port)
	return http.ListenAndServe(":"+port, mux)
}
