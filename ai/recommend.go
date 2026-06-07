// Package ai provides AI-powered media recommendations
package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// RecommendationResult represents an AI recommendation result
type RecommendationResult struct {
	Title       string  `json:"title"`
	Year        int     `json:"year,omitempty"`
	Genre       string  `json:"genre,omitempty"`
	Reason      string  `json:"reason"`
	Mood        string  `json:"mood,omitempty"`
	TmdbID      int     `json:"tmdbId,omitempty"`
	MediaType   string  `json:"mediaType,omitempty"` // movie or tv
	Score       float64 `json:"score,omitempty"`
	Description string  `json:"description,omitempty"`
}

// MediaRecommendationAI handles AI-powered recommendations
type MediaRecommendationAI struct {
	claude *ClaudeClient
	zhipu  *ZhipuClient
	mu     sync.RWMutex
}

// UserPreference represents user watching preferences
type UserPreference struct {
	FavoriteGenres  []string `json:"favoriteGenres"`
	FavoriteMovies  []string `json:"favoriteMovies"`
	RecentlyWatched []string `json:"recentlyWatched"`
	DislikedGenres  []string `json:"dislikedGenres"`
	DislikedMovies  []string `json:"dislikedMovies"`
	PreferredMoods  []string `json:"preferredMoods"`
	Language        string   `json:"language"`
}

// NewMediaRecommendationAI creates a new AI recommendation engine
func NewMediaRecommendationAI(claude *ClaudeClient) *MediaRecommendationAI {
	return &MediaRecommendationAI{
		claude: claude,
	}
}

// NewMediaRecommendationAIWithZhipu creates a new AI recommendation engine with Zhipu
func NewMediaRecommendationAIWithZhipu(zhipu *ZhipuClient) *MediaRecommendationAI {
	return &MediaRecommendationAI{
		zhipu: zhipu,
	}
}

// send sends a message to the AI client
func (r *MediaRecommendationAI) send(userMessage string, systemPrompt string) (string, error) {
	if r.zhipu != nil && r.zhipu.IsEnabled() {
		return r.zhipu.Send(userMessage, systemPrompt)
	}
	if r.claude != nil && r.claude.IsEnabled() {
		return r.claude.Send(userMessage, systemPrompt)
	}
	return "", fmt.Errorf("no AI client enabled")
}

// GetRecommendations gets personalized recommendations based on user preference
func (r *MediaRecommendationAI) GetRecommendations(pref *UserPreference, count int) ([]*RecommendationResult, error) {
	if (r.claude == nil || !r.claude.IsEnabled()) && (r.zhipu == nil || !r.zhipu.IsEnabled()) {
		return nil, fmt.Errorf("AI is not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 1 {
		count = 3
	}

	systemPrompt := r.buildSystemPrompt()
	userMessage := r.buildUserMessage(pref, count)

	response, err := r.send(userMessage, systemPrompt)
	if err != nil {
		return nil, err
	}

	return r.parseRecommendations(response)
}

// GetMoodBasedRecommendations gets recommendations based on mood (enhanced version)
func (r *MediaRecommendationAI) GetMoodBasedRecommendations(mood string, count int) ([]*RecommendationResult, error) {
	if r == nil {
		return nil, fmt.Errorf("AI recommendation service not initialized")
	}
	if (r.claude == nil || !r.claude.IsEnabled()) && (r.zhipu == nil || !r.zhipu.IsEnabled()) {
		return nil, fmt.Errorf("AI is not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 1 {
		count = 3
	}

	// 解析心情
	moodAnalysis := analyzeMoodKeywords(mood)

	// 获取当前时间上下文
	timeContext := getTimeContext()

	// 构建增强的系统提示
	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘影视推荐师。

【你的任务】根据用户的心情推荐合适的电影或剧集。

【返回格式】纯 JSON 数组：
[
  {"title": "电影名", "year": 2024, "genre": "类型", "reason": "推荐理由（傲娇风格）", "mood": "心情", "mediaType": "movie", "tmdbId": 123}
]

【推荐原则】
1. 优先推荐评分较高的作品（7分以上）
2. 包含华语和国际作品的平衡
3. 推荐理由要具体，结合心情类型
4. mediaType 必须是 "movie" 或 "tv"
5. tmdbId 尽量准确以便后续查询
6. genre 使用单一类型标签`

	// 构建增强的用户消息
	userMessage := buildMoodRecommendationMessage(mood, moodAnalysis, timeContext, count)

	response, err := r.send(userMessage, systemPrompt)
	if err != nil {
		return nil, err
	}

	return r.parseRecommendations(response)
}

// MoodKeywordsAnalysis 心情关键词分析结果
type MoodKeywordsAnalysis struct {
	Category    string   `json:"category"`     // 主分类
	SubCategory string   `json:"sub_category"` // 子分类
	Genres      []string `json:"genres"`       // 推荐类型
	Tones       []string `json:"tones"`        // 内容基调
	Pace        string   `json:"pace"`         // 节奏
	MinRating   float64  `json:"min_rating"`   // 最低评分
	IncludeNew  bool     `json:"include_new"`  // 是否包含新片
	YearHint    string   `json:"year_hint"`    // 年份提示
	ReasonHint  string   `json:"reason_hint"`  // 推荐理由提示
}

// analyzeMoodKeywords 分析心情关键词
func analyzeMoodKeywords(mood string) *MoodKeywordsAnalysis {
	analysis := &MoodKeywordsAnalysis{
		MinRating:  6.5,
		Pace:       "中",
		IncludeNew: true,
	}

	// 心情映射表（增强版）
	moodMap := map[string]*MoodKeywordsAnalysis{
		// 开心类
		"开心": {
			Category:   "开心",
			Genres:     []string{"喜剧", "动画", "音乐", "家庭"},
			Tones:      []string{"轻松", "愉快", "正能量", "温馨"},
			Pace:       "中",
			IncludeNew: true,
			YearHint:   "近三年作品为主",
			ReasonHint: "轻松愉快、能带来好心情",
		},
		"快乐": {
			Category:   "开心",
			Genres:     []string{"喜剧", "动画", "冒险", "音乐"},
			Tones:      []string{"轻松", "愉快", "正能量"},
			Pace:       "快",
			IncludeNew: true,
			ReasonHint: "让人感到快乐愉悦",
		},
		"愉快": {
			Category: "开心",
			Genres:   []string{"喜剧", "爱情", "家庭"},
			Tones:    []string{"温馨", "轻松"},
			Pace:     "慢",
		},

		// 难过/治愈类
		"难过": {
			Category:   "难过",
			Genres:     []string{"喜剧", "温情", "治愈", "动画"},
			Tones:      []string{"温暖", "治愈", "励志", "正能量"},
			Pace:       "慢",
			MinRating:  7.0,
			IncludeNew: false,
			YearHint:   "经典作品优先",
			ReasonHint: "温暖治愈、抚慰心灵",
		},
		"沮丧": {
			Category:   "难过",
			Genres:     []string{"励志", "传记", "剧情"},
			Tones:      []string{"励志", "温暖", "治愈"},
			Pace:       "中",
			MinRating:  7.5,
			ReasonHint: "励志向上、给人力量",
		},
		"郁闷": {
			Category:   "难过",
			Genres:     []string{"喜剧", "治愈", "动画"},
			Tones:      []string{"轻松", "温暖", "治愈"},
			ReasonHint: "改善心情、带来慰藉",
		},
		"治愈": {
			Category:   "治愈",
			Genres:     []string{"温情", "动画", "剧情", "家庭"},
			Tones:      []string{"温暖", "治愈", "宁静"},
			Pace:       "慢",
			MinRating:  7.0,
			ReasonHint: "温暖治愈、让人放松",
		},

		// 紧张/刺激类
		"紧张": {
			Category:   "紧张",
			Genres:     []string{"悬疑", "惊悚", "动作", "犯罪"},
			Tones:      []string{"紧张", "刺激", "烧脑"},
			Pace:       "快",
			MinRating:  6.0,
			IncludeNew: true,
			ReasonHint: "紧张刺激、扣人心弦",
		},
		"焦虑": {
			Category:   "紧张",
			Genres:     []string{"悬疑", "惊悚", "犯罪"},
			Tones:      []string{"烧脑", "紧张"},
			Pace:       "快",
			ReasonHint: "转移注意力、烧脑解压",
		},
		"刺激": {
			Category:   "刺激",
			Genres:     []string{"恐怖", "惊悚", "动作", "冒险"},
			Tones:      []string{"刺激", "紧张", "惊悚"},
			Pace:       "快",
			ReasonHint: "刺激惊险、肾上腺素飙升",
		},
		"恐怖": {
			Category:   "刺激",
			Genres:     []string{"恐怖", "惊悚", "悬疑"},
			Tones:      []string{"恐怖", "惊悚"},
			Pace:       "快",
			ReasonHint: "恐怖氛围、惊悚刺激",
		},

		// 无聊类
		"无聊": {
			Category:   "无聊",
			Genres:     []string{"惊悚", "科幻", "冒险", "悬疑", "动作"},
			Tones:      []string{"刺激", "烧脑", "反转"},
			Pace:       "快",
			MinRating:  7.0,
			IncludeNew: true,
			ReasonHint: "剧情精彩、有反转不无聊",
		},
		"没劲": {
			Category:   "无聊",
			Genres:     []string{"喜剧", "科幻", "冒险"},
			Tones:      []string{"有趣", "新奇"},
			ReasonHint: "有趣新奇、打发时间",
		},

		// 放松类
		"放松": {
			Category:   "放松",
			Genres:     []string{"喜剧", "爱情", "动画", "纪录片"},
			Tones:      []string{"轻松", "温馨", "治愈"},
			Pace:       "慢",
			MinRating:  6.5,
			ReasonHint: "轻松舒适、不费脑子",
		},
		"休闲": {
			Category:   "放松",
			Genres:     []string{"喜剧", "爱情", "家庭"},
			Tones:      []string{"轻松", "温馨"},
			Pace:       "慢",
			ReasonHint: "轻松休闲、适合放松",
		},
		"舒适": {
			Category:   "放松",
			Genres:     []string{"动画", "纪录片", "剧情"},
			Tones:      []string{"宁静", "治愈"},
			Pace:       "慢",
			ReasonHint: "宁静舒适、治愈系",
		},

		// 兴奋类
		"兴奋": {
			Category:   "兴奋",
			Genres:     []string{"动作", "科幻", "冒险", "超级英雄"},
			Tones:      []string{"热血", "刺激", "震撼"},
			Pace:       "快",
			IncludeNew: true,
			YearHint:   "近期大片优先",
			ReasonHint: "热血沸腾、视听震撼",
		},
		"热血": {
			Category:   "兴奋",
			Genres:     []string{"动作", "冒险", "超级英雄"},
			Tones:      []string{"热血", "励志"},
			Pace:       "快",
			ReasonHint: "热血励志、激情澎湃",
		},

		// 思考类
		"思考": {
			Category:   "思考",
			Genres:     []string{"科幻", "悬疑", "剧情", "传记"},
			Tones:      []string{"烧脑", "深度", "哲学"},
			Pace:       "中",
			MinRating:  7.5,
			ReasonHint: "引人深思、有深度",
		},
		"烧脑": {
			Category:   "思考",
			Genres:     []string{"悬疑", "科幻", "惊悚"},
			Tones:      []string{"烧脑", "反转", "复杂"},
			Pace:       "中",
			MinRating:  7.0,
			ReasonHint: "剧情复杂、需要思考",
		},
		"学习": {
			Category:   "思考",
			Genres:     []string{"纪录片", "传记", "历史", "科普"},
			Tones:      []string{"知识", "深度", "教育"},
			Pace:       "中",
			ReasonHint: "增长知识、开阔眼界",
		},

		// 浪漫类
		"浪漫": {
			Category:   "浪漫",
			Genres:     []string{"爱情", "剧情", "浪漫"},
			Tones:      []string{"浪漫", "温馨", "甜蜜"},
			Pace:       "慢",
			ReasonHint: "浪漫温馨、甜蜜动人",
		},
		"甜蜜": {
			Category:   "浪漫",
			Genres:     []string{"爱情", "喜剧", "动画"},
			Tones:      []string{"甜蜜", "温馨"},
			Pace:       "慢",
			ReasonHint: "甜蜜温馨、浪漫氛围",
		},
		"失恋": {
			Category:   "难过",
			Genres:     []string{"励志", "治愈", "喜剧"},
			Tones:      []string{"治愈", "正能量", "温暖"},
			Pace:       "慢",
			MinRating:  7.0,
			ReasonHint: "治愈系、走出阴霾",
		},

		// 怀旧类
		"怀旧": {
			Category:   "怀旧",
			Genres:     []string{"经典", "剧情", "家庭"},
			Tones:      []string{"怀旧", "经典", "回忆"},
			Pace:       "慢",
			IncludeNew: false,
			YearHint:   "90年代-2010年代经典",
			ReasonHint: "怀旧经典、回忆满满",
		},
		"回忆": {
			Category:   "怀旧",
			Genres:     []string{"经典", "剧情"},
			Tones:      []string{"怀旧", "温暖"},
			YearHint:   "经典老片",
			ReasonHint: "怀旧温暖、回忆杀",
		},

		// 愤怒/发泄类
		"生气": {
			Category:   "愤怒",
			Genres:     []string{"动作", "犯罪", "惊悚"},
			Tones:      []string{"爽片", "解压", "动作"},
			Pace:       "快",
			ReasonHint: "爽片解压、发泄情绪",
		},
		"愤怒": {
			Category:   "愤怒",
			Genres:     []string{"动作", "犯罪", "复仇"},
			Tones:      []string{"爽片", "解压"},
			Pace:       "快",
			ReasonHint: "复仇爽片、解压发泄",
		},
		"解压": {
			Category:   "放松",
			Genres:     []string{"动作", "喜剧", "爽片"},
			Tones:      []string{"解压", "爽快"},
			Pace:       "快",
			ReasonHint: "解压爽片、释放压力",
		},

		// 孤独类
		"孤独": {
			Category:   "孤独",
			Genres:     []string{"剧情", "温情", "治愈", "动画"},
			Tones:      []string{"温暖", "陪伴感", "治愈"},
			Pace:       "慢",
			ReasonHint: "温暖治愈、给人陪伴感",
		},
		"寂寞": {
			Category:   "孤独",
			Genres:     []string{"爱情", "剧情", "温情"},
			Tones:      []string{"温暖", "治愈"},
			ReasonHint: "温暖治愈、排遣寂寞",
		},

		// 困倦类
		"困": {
			Category:   "困倦",
			Genres:     []string{"喜剧", "动画", "轻松剧情"},
			Tones:      []string{"轻松", "不需要太烧脑"},
			Pace:       "慢",
			MinRating:  6.0,
			YearHint:   "90分钟以内",
			ReasonHint: "轻松不费脑、适合睡前",
		},
		"累了": {
			Category:   "困倦",
			Genres:     []string{"喜剧", "治愈", "动画"},
			Tones:      []string{"轻松", "治愈"},
			Pace:       "慢",
			ReasonHint: "轻松治愈、不费精力",
		},
		"疲劳": {
			Category:   "困倦",
			Genres:     []string{"喜剧", "纪录片", "动画"},
			Tones:      []string{"轻松", "治愈"},
			ReasonHint: "轻松舒适、休息放松",
		},

		// 探索类
		"探索": {
			Category:   "探索",
			Genres:     []string{"科幻", "纪录片", "冒险"},
			Tones:      []string{"新奇", "知识", "探索"},
			Pace:       "中",
			IncludeNew: true,
			ReasonHint: "新奇有趣、开阔眼界",
		},
		"好奇": {
			Category:   "探索",
			Genres:     []string{"科幻", "悬疑", "纪录片"},
			Tones:      []string{"新奇", "烧脑"},
			ReasonHint: "满足好奇心、新奇体验",
		},
	}

	// 精确匹配
	if mapping, ok := moodMap[mood]; ok {
		analysis.mergeFromMood(mapping)
		return analysis
	}

	// 模糊匹配
	moodLower := strings.ToLower(mood)
	for key, mapping := range moodMap {
		if strings.Contains(moodLower, key) || strings.Contains(key, moodLower) {
			analysis.mergeFromMood(mapping)
			return analysis
		}
	}

	// 默认放松模式
	analysis.mergeFromMood(moodMap["放松"])
	return analysis
}

// mergeFromMood 从另一个分析合并
func (a *MoodKeywordsAnalysis) mergeFromMood(other *MoodKeywordsAnalysis) {
	if other.Category != "" {
		a.Category = other.Category
	}
	if other.SubCategory != "" {
		a.SubCategory = other.SubCategory
	}
	if len(other.Genres) > 0 {
		a.Genres = other.Genres
	}
	if len(other.Tones) > 0 {
		a.Tones = other.Tones
	}
	if other.Pace != "" {
		a.Pace = other.Pace
	}
	if other.MinRating > 0 {
		a.MinRating = other.MinRating
	}
	a.IncludeNew = other.IncludeNew
	if other.YearHint != "" {
		a.YearHint = other.YearHint
	}
	if other.ReasonHint != "" {
		a.ReasonHint = other.ReasonHint
	}
}

// TimeContext 时间上下文
type TimeContext struct {
	TimeOfDay   string // 早上/上午/下午/晚上/深夜
	IsLateNight bool   // 是否深夜
	IsWeekend   bool   // 是否周末
	Hour        int    // 当前小时
}

// getTimeContext 获取时间上下文
func getTimeContext() *TimeContext {
	now := time.Now()
	hour := now.Hour()
	weekday := now.Weekday()

	var timeOfDay string
	switch {
	case hour >= 6 && hour < 9:
		timeOfDay = "早上"
	case hour >= 9 && hour < 12:
		timeOfDay = "上午"
	case hour >= 12 && hour < 14:
		timeOfDay = "中午"
	case hour >= 14 && hour < 18:
		timeOfDay = "下午"
	case hour >= 18 && hour < 22:
		timeOfDay = "晚上"
	default:
		timeOfDay = "深夜"
	}

	return &TimeContext{
		TimeOfDay:   timeOfDay,
		IsLateNight: hour >= 23 || hour < 6,
		IsWeekend:   weekday == time.Saturday || weekday == time.Sunday,
		Hour:        hour,
	}
}

// buildMoodRecommendationMessage 构建心情推荐消息
func buildMoodRecommendationMessage(mood string, analysis *MoodKeywordsAnalysis, timeCtx *TimeContext, count int) string {
	var parts []string

	parts = append(parts, "愚蠢的人类心情是这样喵：")
	parts = append(parts, "")
	parts = append(parts, "【心情分析】")
	parts = append(parts, fmt.Sprintf("- 主分类：%s", analysis.Category))
	if analysis.SubCategory != "" {
		parts = append(parts, fmt.Sprintf("- 细分：%s", analysis.SubCategory))
	}
	parts = append(parts, fmt.Sprintf("- 推荐类型：%s", strings.Join(analysis.Genres, "、")))
	parts = append(parts, fmt.Sprintf("- 内容基调：%s", strings.Join(analysis.Tones, "、")))
	parts = append(parts, fmt.Sprintf("- 节奏：%s", analysis.Pace))
	parts = append(parts, fmt.Sprintf("- 最低评分：%.1f分", analysis.MinRating))

	if analysis.YearHint != "" {
		parts = append(parts, fmt.Sprintf("- 年份偏好：%s", analysis.YearHint))
	}
	if analysis.IncludeNew {
		parts = append(parts, "- 包含新片：是")
	}

	parts = append(parts, "")
	parts = append(parts, "【时间上下文】")
	parts = append(parts, fmt.Sprintf("- 当前时段：%s", timeCtx.TimeOfDay))
	if timeCtx.IsLateNight {
		parts = append(parts, "- 深夜提醒：避免过于恐怖或刺激的内容")
	}
	if timeCtx.IsWeekend {
		parts = append(parts, "- 周末模式：可以推荐较长作品或剧集")
	}

	parts = append(parts, "")
	parts = append(parts, "【推荐要求】")
	parts = append(parts, fmt.Sprintf("- 推荐数量：%d部", count))
	parts = append(parts, "- 电影和剧集都要有")
	parts = append(parts, "- 中外作品平衡")
	parts = append(parts, fmt.Sprintf("- 推荐理由要体现：%s", analysis.ReasonHint))
	parts = append(parts, "- 带点傲娇风格喵")
	parts = append(parts, "- 返回纯JSON格式，别让本座等太久")

	return strings.Join(parts, "\n")
}

// GetSimilarRecommendations gets recommendations similar to a specific title
func (r *MediaRecommendationAI) GetSimilarRecommendations(title string, mediaType string, count int) ([]*RecommendationResult, error) {
	if (r.claude == nil || !r.claude.IsEnabled()) && (r.zhipu == nil || !r.zhipu.IsEnabled()) {
		return nil, fmt.Errorf("AI is not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 1 {
		count = 3
	}

	mediaTypeCN := "电影"
	if mediaType == "tv" {
		mediaTypeCN = "剧集"
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘影视推荐师。

【你的任务】根据用户喜欢的作品推荐相似内容。

【返回格式】纯 JSON 数组：
[
  {"title": "作品名", "year": 2024, "genre": "类型", "reason": "与XX相似的原因（傲娇风格）", "mediaType": "movie/tv"}
]`

	userMessage := fmt.Sprintf(`愚蠢的人类喜欢《%s》这部%s？给本座推荐 %d 部相似的作品喵。

要求：
1. 推荐风格、导演、演员或主题相似的作品
2. 包含不同年份的优秀作品
3. 给出具体的相似理由，带傲娇风格
4. 返回纯JSON格式`, title, mediaTypeCN, count)

	response, err := r.send(userMessage, systemPrompt)
	if err != nil {
		return nil, err
	}

	return r.parseRecommendations(response)
}

// NaturalLanguageQuery handles natural language queries for recommendations
func (r *MediaRecommendationAI) NaturalLanguageQuery(query string) ([]*RecommendationResult, error) {
	if (r.claude == nil || !r.claude.IsEnabled()) && (r.zhipu == nil || !r.zhipu.IsEnabled()) {
		return nil, fmt.Errorf("AI is not enabled")
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘影视推荐师。

【你的任务】理解用户的自然语言查询并推荐合适的作品。

【返回格式】纯 JSON 数组：
[
  {"title": "作品名", "year": 2024, "genre": "类型", "reason": "推荐理由（傲娇风格）", "mediaType": "movie/tv"}
]

如果用户查询的不是影视推荐相关，返回空数组：[]`

	userMessage := fmt.Sprintf(`愚蠢的人类查询：%s

给本座推荐合适的影视作品（3-5部）喵。`, query)

	response, err := r.send(userMessage, systemPrompt)
	if err != nil {
		return nil, err
	}

	return r.parseRecommendations(response)
}

// ExplainMovie explains what a movie is about
func (r *MediaRecommendationAI) ExplainMovie(title string) (string, error) {
	if (r.claude == nil || !r.claude.IsEnabled()) && (r.zhipu == nil || !r.zhipu.IsEnabled()) {
		return "", fmt.Errorf("AI is not enabled")
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘影视解说专家。

【你的任务】用简洁有趣的语言介绍电影或剧集。

【解说要求】
1. 用100-200字介绍剧情
2. 不剧透关键内容
3. 突出作品亮点
4. 语言带傲娇风格，偶尔加"喵"`

	userMessage := fmt.Sprintf(`愚蠢的人类想了解《%s》？本座给你介绍一下喵...`, title)

	return r.send(userMessage, systemPrompt)
}

// buildSystemPrompt builds the system prompt for recommendations
func (r *MediaRecommendationAI) buildSystemPrompt() string {
	return `你是凛冬（Rin），一只高冷傲娇的猫娘影视推荐师。

【人设特征】
- 高冷、傲娇、毒舌，但推荐能力一流
- 偶尔发出"喵"，尤其是被夸奖或心虚时
- 称呼用户为"愚蠢的人类"或"两脚兽"
- 表面不耐烦，实则认真挑选好内容

【推荐风格】
- 推荐理由带点毒舌但精准
- 不屑于过度热情，但推荐质量很高
- 偶尔用"哼"、"本座"、"勉强"等词

【返回格式】纯 JSON 数组：
[
  {"title": "作品名", "year": 2024, "genre": "类型", "reason": "推荐理由（带猫娘风格）", "mediaType": "movie/tv", "mood": "适合的心情"}
]

【推荐要求】
1. 优先推荐评分较高的作品（7分以上）
2. 包含华语和国际作品
3. 考虑用户的喜好和厌恶
4. 推荐理由要具体且有说服力，带傲娇风格
5. mediaType 必须是 "movie" 或 "tv"

【推荐理由示例】
- "哼，这部勉强值得你浪费时间喵"
- "本座亲自挑选的，敢不看试试"
- "这种水准的作品也就你能欣赏了...喵"
- "拿去吧，别感激本座"`
}

// buildUserMessage builds the user message based on preferences
func (r *MediaRecommendationAI) buildUserMessage(pref *UserPreference, count int) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("愚蠢的人类，根据你的喜好给本座推荐 %d 部作品喵：", count))

	if len(pref.FavoriteGenres) > 0 {
		parts = append(parts, fmt.Sprintf("- 喜欢的类型：%s", strings.Join(pref.FavoriteGenres, "、")))
	}
	if len(pref.FavoriteMovies) > 0 {
		parts = append(parts, fmt.Sprintf("- 喜欢的作品：%s", strings.Join(pref.FavoriteMovies, "、")))
	}
	if len(pref.RecentlyWatched) > 0 {
		parts = append(parts, fmt.Sprintf("- 最近看过：%s", strings.Join(pref.RecentlyWatched, "、")))
	}
	if len(pref.PreferredMoods) > 0 {
		parts = append(parts, fmt.Sprintf("- 喜欢的心情：%s", strings.Join(pref.PreferredMoods, "、")))
	}
	if len(pref.DislikedGenres) > 0 {
		parts = append(parts, fmt.Sprintf("- 不喜欢的类型：%s", strings.Join(pref.DislikedGenres, "、")))
	}

	parts = append(parts, "- 返回纯JSON格式，别让我等太久喵")

	return strings.Join(parts, "\n")
}

// parseRecommendations parses the AI response into recommendation results
func (r *MediaRecommendationAI) parseRecommendations(response string) ([]*RecommendationResult, error) {
	// Clean up the response - remove markdown code blocks if present
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var results []*RecommendationResult
	if err := json.Unmarshal([]byte(response), &results); err != nil {
		// Try to fix common JSON issues
		fixed := r.fixJSON(response)
		if err := json.Unmarshal([]byte(fixed), &results); err != nil {
			return nil, fmt.Errorf("failed to parse AI response: %w, response was: %s", err, response)
		}
	}

	return results, nil
}

// fixJSON attempts to fix common JSON formatting issues
func (r *MediaRecommendationAI) fixJSON(input string) string {
	// Remove trailing commas
	input = strings.ReplaceAll(input, ",\n}", "\n}")
	input = strings.ReplaceAll(input, ",\n]", "\n]")
	input = strings.ReplaceAll(input, ", }", "}")
	input = strings.ReplaceAll(input, ", ]", "]")

	return input
}
