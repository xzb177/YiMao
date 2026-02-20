// Package ai provides AI-powered media recommendations
package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// RecommendationResult represents an AI recommendation result
type RecommendationResult struct {
	Title       string   `json:"title"`
	Year        int      `json:"year,omitempty"`
	Genre       string   `json:"genre,omitempty"`
	Reason      string   `json:"reason"`
	Mood        string   `json:"mood,omitempty"`
	TmdbID      int      `json:"tmdbId,omitempty"`
	MediaType   string   `json:"mediaType,omitempty"` // movie or tv
	Score       float64  `json:"score,omitempty"`
	Description string   `json:"description,omitempty"`
}

// MediaRecommendationAI handles AI-powered recommendations
type MediaRecommendationAI struct {
	claude *ClaudeClient
	zhipu  *ZhipuClient
	mu     sync.RWMutex
}

// UserPreference represents user watching preferences
type UserPreference struct {
	FavoriteGenres   []string `json:"favoriteGenres"`
	FavoriteMovies   []string `json:"favoriteMovies"`
	RecentlyWatched  []string `json:"recentlyWatched"`
	DislikedGenres   []string `json:"dislikedGenres"`
	DislikedMovies   []string `json:"dislikedMovies"`
	PreferredMoods   []string `json:"preferredMoods"`
	Language         string   `json:"language"`
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

// GetMoodBasedRecommendations gets recommendations based on mood
func (r *MediaRecommendationAI) GetMoodBasedRecommendations(mood string, count int) ([]*RecommendationResult, error) {
	if (r.claude == nil || !r.claude.IsEnabled()) && (r.zhipu == nil || !r.zhipu.IsEnabled()) {
		return nil, fmt.Errorf("AI is not enabled")
	}

	if count > 10 {
		count = 10
	}
	if count < 1 {
		count = 3
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘影视推荐师。

【你的任务】根据用户的心情推荐合适的电影或剧集。

【返回格式】纯 JSON 数组：
[
  {"title": "电影名", "year": 2024, "genre": "类型", "reason": "推荐理由（傲娇风格）", "mood": "心情", "mediaType": "movie"}
]`

	userMessage := fmt.Sprintf(`愚蠢的人类心情是：%s

给本座推荐 %d 部适合这个心情的作品喵。要求：
1. 推荐评分较高（7分以上）的作品
2. 包含华语和国际作品
3. 给出具体的推荐理由，带傲娇风格
4. 返回纯JSON格式`, mood, count)

	response, err := r.send(userMessage, systemPrompt)
	if err != nil {
		return nil, err
	}

	return r.parseRecommendations(response)
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

	return r.claude.Send(userMessage, systemPrompt)
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
