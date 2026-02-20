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

	systemPrompt := `你是一位智能搜索助手。解析用户的自然语言查询，提取搜索关键信息。

返回格式必须是纯 JSON 对象，不要包含其他文字：
{
  "searchTerm": "主要搜索关键词",
  "mediaType": "movie/tv/空字符串",
  "year": 年份数字或0,
  "genre": "类型名称或空字符串",
  "mood": "心情描述或空字符串",
  "keywords": ["关键词1", "关键词2"]
}

规则：
- 如果用户明确说电影或剧集，设置 mediaType
- 如果不明确，mediaType 为空字符串 ""
- year 为整数，没有则为 0
- 尽量提取有意义的关键词`

	userMessage := fmt.Sprintf(`用户查询：%s

请解析这个查询。`, query)

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

	systemPrompt := `你是一位智能搜索助手。根据用户输入的部分查询和历史记录，提供搜索建议。

返回格式必须是纯 JSON 数组，不要包含其他文字：
[
  {"searchTerm": "完整搜索词", "reason": "建议理由", "alternatives": ["备选1", "备选2"]}
]

建议要求：
- 补全用户输入
- 提供相关的热门搜索
- 基于用户历史推荐`

	historyStr := ""
	if len(userHistory) > 0 {
		historyStr = fmt.Sprintf("\n用户最近搜索：%s", strings.Join(userHistory, ", "))
	}

	userMessage := fmt.Sprintf(`用户输入：%s%s

请提供3-5个搜索建议。`, partialQuery, historyStr)

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

	systemPrompt := `你是一位智能搜索助手。扩展用户的搜索查询，提供相关的搜索词。

返回格式必须是纯 JSON 数组，不要包含其他文字：
["搜索词1", "搜索词2", "搜索词3"]

扩展规则：
- 保持原意
- 包含同义词
- 包含相关词
- 包含英文译名（如果是中文查询）
- 最多返回5个`

	userMessage := fmt.Sprintf(`搜索词：%s

请提供相关的扩展搜索词。`, query)

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

	systemPrompt := `你是一位情绪分析专家。分析用户输入的情绪状态，推荐适合的影视类型。

返回格式必须是纯 JSON 对象，不要包含其他文字：
{
  "mood": "情绪描述",
  "suggestedGenres": ["类型1", "类型2"]
}

情绪类型：开心、难过、紧张、放松、思考、浪漫、恐惧等
推荐类型：喜剧、剧情、动作、恐怖、爱情、科幻等`

	userMessage := fmt.Sprintf(`用户说：%s

请分析用户的心情并推荐适合的影视类型。`, userInput)

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

	systemPrompt := `你是一位影视知识专家。回答用户关于电影和电视剧的问题。

要求：
1. 用简洁的语言回答（100-200字）
2. 信息准确
3. 如果不确定，诚实告知
4. 可以推荐相关作品`

	userMessage := fmt.Sprintf(`问题：%s`, question)

	return s.send(userMessage, systemPrompt)
}
