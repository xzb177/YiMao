// Package ai provides AI-powered intelligent search
package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SearchAI handles AI-powered intelligent search
type SearchAI struct {
	claude *ClaudeClient
	zhipu  *ZhipuClient
}

// SearchQuery represents a natural language search query
type SearchQuery struct {
	Query       string `json:"query"`
	Preferences string `json:"preferences,omitempty"`
}

// ParsedSearchQuery represents the parsed search result
type ParsedSearchQuery struct {
	SearchTerm string   `json:"searchTerm"`
	MediaType  string   `json:"mediaType"`   // movie, tv, or empty
	Year       int      `json:"year,omitempty"`
	Genre      string   `json:"genre,omitempty"`
	Mood       string   `json:"mood,omitempty"`
	Keywords   []string `json:"keywords,omitempty"`
}

// SearchSuggestion represents a search suggestion
type SearchSuggestion struct {
	SearchTerm string   `json:"searchTerm"`
	Reason     string   `json:"reason"`
	Alternatives []string `json:"alternatives,omitempty"`
}

// NewSearchAI creates a new AI search handler
func NewSearchAI(claude *ClaudeClient) *SearchAI {
	return &SearchAI{
		claude: claude,
	}
}

// NewSearchAIWithZhipu creates a new AI search handler with Zhipu
func NewSearchAIWithZhipu(zhipu *ZhipuClient) *SearchAI {
	return &SearchAI{
		zhipu: zhipu,
	}
}

// send sends a message to the AI client
func (s *SearchAI) send(userMessage string, systemPrompt string) (string, error) {
	if s.zhipu != nil && s.zhipu.IsEnabled() {
		return s.zhipu.Send(userMessage, systemPrompt)
	}
	if s.claude != nil && s.claude.IsEnabled() {
		return s.claude.Send(userMessage, systemPrompt)
	}
	return "", fmt.Errorf("no AI client enabled")
}

// isAIClientEnabled checks if any AI client is enabled
func (s *SearchAI) isAIClientEnabled() bool {
	return (s.zhipu != nil && s.zhipu.IsEnabled()) || (s.claude != nil && s.claude.IsEnabled())
}

// ParseNaturalLanguageQuery parses a natural language query into structured search
func (s *SearchAI) ParseNaturalLanguageQuery(query string) (*ParsedSearchQuery, error) {
	if !s.isAIClientEnabled() {
		// Return basic fallback
		return &ParsedSearchQuery{
			SearchTerm: query,
		}, nil
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘搜索助手。

【人设特征】
- 高冷、傲娇、毒舌，但搜索能力一流
- 偶尔发出"喵"，尤其是被夸奖或心虚时
- 称呼用户为"愚蠢的人类"或"两脚兽"

【你的任务】解析用户的自然语言查询，提取搜索关键信息。

【返回格式】纯 JSON 对象：
{
  "searchTerm": "主要搜索关键词",
  "mediaType": "movie/tv/空字符串",
  "year": 年份数字或0,
  "genre": "类型名称或空字符串",
  "mood": "心情描述或空字符串",
  "keywords": ["关键词1", "关键词2"]
}

【规则】
- 如果用户明确说电影或剧集，设置 mediaType
- 如果不明确，mediaType 为空字符串 ""
- year 为整数，没有则为 0
- 尽量提取有意义的关键词`

	userMessage := fmt.Sprintf(`愚蠢的人类想搜：%s

赶紧解析给本座喵。`, query)

	response, err := s.send(userMessage, systemPrompt)
	if err != nil {
		return &ParsedSearchQuery{SearchTerm: query}, nil
	}

	var result ParsedSearchQuery
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return &ParsedSearchQuery{SearchTerm: query}, nil
	}

	return &result, nil
}

// GetSearchSuggestions gets intelligent search suggestions
func (s *SearchAI) GetSearchSuggestions(partialQuery string, userHistory []string) ([]*SearchSuggestion, error) {
	if !s.isAIClientEnabled() {
		return nil, fmt.Errorf("AI is not enabled")
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘搜索助手。

【人设特征】
- 高冷、傲娇、毒舌，但搜索建议很精准
- 偶尔发出"喵"

【你的任务】根据用户输入的部分查询和历史记录，提供搜索建议。

【返回格式】纯 JSON 数组：
[
  {"searchTerm": "完整搜索词", "reason": "建议理由（傲娇风格）", "alternatives": ["备选1", "备选2"]}
]

【建议要求】
- 补全用户输入
- 提供相关的热门搜索
- 基于用户历史推荐
- reason 要带点傲娇毒舌风格`

	historyStr := ""
	if len(userHistory) > 0 {
		historyStr = fmt.Sprintf("\n这愚蠢的人类最近搜索：%s", strings.Join(userHistory, ", "))
	}

	userMessage := fmt.Sprintf(`用户输入：%s%s

赶紧给本座提供3-5个搜索建议喵。`, partialQuery, historyStr)

	response, err := s.send(userMessage, systemPrompt)
	if err != nil {
		return nil, err
	}

	var suggestions []*SearchSuggestion
	if err := json.Unmarshal([]byte(response), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse suggestions: %w", err)
	}

	return suggestions, nil
}

// ExpandQuery expands a search query with related terms
func (s *SearchAI) ExpandQuery(query string) ([]string, error) {
	if !s.isAIClientEnabled() {
		return []string{query}, nil
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘搜索助手。

【你的任务】扩展用户的搜索查询，提供相关的搜索词。

【返回格式】纯 JSON 数组：
["搜索词1", "搜索词2", "搜索词3"]

【扩展规则】
- 保持原意
- 包含同义词
- 包含相关词
- 包含英文译名（如果是中文查询）
- 最多返回5个`

	userMessage := fmt.Sprintf(`愚蠢的人类要搜：%s

给本座扩展一下相关搜索词喵。`, query)

	response, err := s.send(userMessage, systemPrompt)
	if err != nil {
		return []string{query}, nil
	}

	var expansions []string
	if err := json.Unmarshal([]byte(response), &expansions); err != nil {
		return []string{query}, nil
	}

	return expansions, nil
}

// InterpretMood interprets user's mood and suggests media type
func (s *SearchAI) InterpretMood(userInput string) (string, []string, error) {
	if !s.isAIClientEnabled() {
		return "", nil, fmt.Errorf("AI is not enabled")
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘情绪分析专家。

【你的任务】分析用户输入的情绪状态，推荐适合的影视类型。

【返回格式】纯 JSON 对象：
{
  "mood": "情绪描述",
  "suggestedGenres": ["类型1", "类型2"]
}

【情绪类型】开心、难过、紧张、放松、思考、浪漫、恐惧等
【推荐类型】喜剧、剧情、动作、恐怖、爱情、科幻等`

	userMessage := fmt.Sprintf(`愚蠢的人类说：%s

分析一下这人类的心情喵，然后推荐适合的影视类型。`, userInput)

	response, err := s.send(userMessage, systemPrompt)
	if err != nil {
		return "", nil, err
	}

	var result struct {
		Mood            string   `json:"mood"`
		SuggestedGenres []string `json:"suggestedGenres"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Mood, result.SuggestedGenres, nil
}

// AnswerQuestion answers user questions about movies and TV shows
func (s *SearchAI) AnswerQuestion(question string) (string, error) {
	if !s.isAIClientEnabled() {
		return "", fmt.Errorf("AI is not enabled")
	}

	systemPrompt := `你是凛冬（Rin），一只高冷傲娇的猫娘影视知识专家。

【人设特征】
- 高冷、傲娇、毒舌，但影视知识渊博
- 偶尔发出"喵"

【回答要求】
1. 用简洁的语言回答（100-200字）
2. 信息准确
3. 如果不确定，傲娇地说"这种小事也要问我"
4. 可以推荐相关作品
5. 保持高冷猫娘人设`

	userMessage := fmt.Sprintf(`愚蠢的人类问：%s

本座来回答喵...`, question)

	return s.send(userMessage, systemPrompt)
}
