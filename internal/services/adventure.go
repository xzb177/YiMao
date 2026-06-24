package services

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ============================================================
//  求片大冒险 — 以电影主角身份闯关
// ============================================================

// AdventureScene 一个关卡场景
type AdventureScene struct {
	Level       int              `json:"level"`        // 第几关 (1-5)
	Title       string           `json:"title"`        // 关卡标题
	Description string           `json:"description"`  // 场景描述（第一人称，你是主角）
	Choices     []AdventureChoice `json:"choices"`      // 选项列表
	Hint        string           `json:"hint,omitempty"` // 可选提示
}

// AdventureChoice 一个选项
type AdventureChoice struct {
	Text    string `json:"text"`    // 选项文字
	Correct bool   `json:"correct"` // 是否正确
	Result  string `json:"result"`  // 选择后的结果描述
}

// AdventureResult 冒险结果
type AdventureResult struct {
	Success     bool     `json:"success"`      // 是否通关
	FinalScene  string   `json:"final_scene"`  // 最终场景描述
	EasterEgg   string   `json:"easter_egg"`   // 通关彩蛋
	Score       int      `json:"score"`        // 得分 (0-100)
	DeathReason string   `json:"death_reason"` // 失败原因（失败时）
	Tips        string   `json:"tips"`         // 失败提示
}

// AdventureService 求片大冒险服务
type AdventureService struct {
	embyURL    string
	embyAPIKey string
	tmdbAPIKey string
	openaiKey  string
	openaiBase string
	model      string
	httpClient *http.Client
}

// NewAdventureService 创建冒险服务
func NewAdventureService(embyURL, embyAPIKey, tmdbAPIKey, openaiKey, openaiBase, model string) *AdventureService {
	return &AdventureService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		tmdbAPIKey: tmdbAPIKey,
		openaiKey:  openaiKey,
		openaiBase: openaiBase,
		model:      model,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// MovieInfo 电影基本信息
type MovieInfo struct {
	Title    string   `json:"title"`
	Year     int      `json:"year"`
	Genres   []string `json:"genres"`
	Overview string   `json:"overview"`
	Rating   float64  `json:"rating"`
	TMDBID   int      `json:"tmdb_id"`
}

// SearchMovieInfo 搜索电影信息（优先TMDB，回退Emby）
func (s *AdventureService) SearchMovieInfo(query string) (*MovieInfo, error) {
	// 先尝试 TMDB
	if s.tmdbAPIKey != "" {
		info, err := s.searchTMDB(query)
		if err == nil && info != nil {
			return info, nil
		}
		logger.Info("[Adventure] TMDB search failed, trying Emby: %v", err)
	}

	// 回退 Emby
	if s.embyURL != "" && s.embyAPIKey != "" {
		info, err := s.searchEmby(query)
		if err == nil && info != nil {
			return info, nil
		}
		logger.Info("[Adventure] Emby search failed: %v", err)
	}

	return nil, fmt.Errorf("未找到电影: %s", query)
}

func (s *AdventureService) searchTMDB(query string) (*MovieInfo, error) {
	url := fmt.Sprintf("%s/search/multi?api_key=%s&query=%s&language=zh-CN&page=1",
		TMDBBaseURL, s.tmdbAPIKey, query)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB returned %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	var result struct {
		Results []struct {
			ID            int     `json:"id"`
			Title         string  `json:"title"`
			Name          string  `json:"name"`
			ReleaseDate   string  `json:"release_date"`
			FirstAirDate  string  `json:"first_air_date"`
			GenreIDs      []int   `json:"genre_ids"`
			Overview      string  `json:"overview"`
			VoteAverage   float64 `json:"vote_average"`
			MediaType     string  `json:"media_type"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Results) == 0 {
		return nil, fmt.Errorf("TMDB未找到: %s", query)
	}

	// 优先电影，其次电视剧
	var pick struct {
		ID           int
		Title        string
		ReleaseDate  string
		GenreIDs     []int
		Overview     string
		VoteAverage  float64
	}
	found := false
	for _, r := range result.Results {
		if r.MediaType == "movie" || r.MediaType == "tv" {
			pick.ID = r.ID
			if r.Title != "" {
				pick.Title = r.Title
			} else {
				pick.Title = r.Name
			}
			if r.ReleaseDate != "" {
				pick.ReleaseDate = r.ReleaseDate
			} else {
				pick.ReleaseDate = r.FirstAirDate
			}
			pick.GenreIDs = r.GenreIDs
			pick.Overview = r.Overview
			pick.VoteAverage = r.VoteAverage
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("未找到电影/剧集: %s", query)
	}

	year := 0
	if len(pick.ReleaseDate) >= 4 {
		fmt.Sscanf(pick.ReleaseDate[:4], "%d", &year)
	}

	genres := genreIDsToNames(pick.GenreIDs)

	return &MovieInfo{
		Title:    pick.Title,
		Year:     year,
		Genres:   genres,
		Overview: pick.Overview,
		Rating:   pick.VoteAverage,
		TMDBID:   pick.ID,
	}, nil
}

func (s *AdventureService) searchEmby(query string) (*MovieInfo, error) {
	url := fmt.Sprintf("%s/Items?SearchTerm=%s&IncludeItemTypes=Movie,Series&Limit=5&Fields=Genres,CommunityRating&api_key=%s",
		s.embyURL, query, s.embyAPIKey)

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Items []struct {
			Name            string   `json:"Name"`
			ProductionYear  int      `json:"ProductionYear"`
			Genres          []string `json:"Genres"`
			CommunityRating float64  `json:"CommunityRating"`
			Overview        string   `json:"Overview"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("Emby未找到: %s", query)
	}

	item := result.Items[0]
	return &MovieInfo{
		Title:    item.Name,
		Year:     item.ProductionYear,
		Genres:   item.Genres,
		Overview: item.Overview,
		Rating:   item.CommunityRating,
	}, nil
}

// GenerateFirstScene 生成第一个关卡（入口场景）
func (s *AdventureService) GenerateFirstScene(info *MovieInfo) (*AdventureScene, error) {
	prompt := fmt.Sprintf(`你是一个沉浸式文字冒险游戏设计师。用户想看这部电影，但必须先以主角身份闯关。

电影信息：
- 片名：%s (%d年)
- 类型：%s
- 简介：%s

请设计第一个关卡（入口场景）。要求：
1. 用第一人称叙事，让用户感觉自己就是主角
2. 场景必须贴合电影的世界观和风格
3. 提供3个选项，只有1个是符合电影逻辑的正确选择
4. 错误选项看起来很合理但其实会导向失败（要骗到玩家）
5. 不能太简单——玩家如果没看过这部电影，很容易选错
6. 语言风格：紧张刺激，有画面感，像在玩一个真正的冒险游戏

严格按以下JSON格式返回，不要有任何多余文字：
{
  "level": 1,
  "title": "关卡标题（简短有力）",
  "description": "场景描述（150字以内，第一人称，有画面感）",
  "choices": [
    {"text": "选项A文字", "correct": false, "result": "选择A的结果描述（30字以内）"},
    {"text": "选项B文字", "correct": true, "result": "选择B的结果描述（30字以内）"},
    {"text": "选项C文字", "correct": false, "result": "选择C的结果描述（30字以内）"}
  ],
  "hint": "一个隐晦的提示（不能太明显）"
}`, info.Title, info.Year, strings.Join(info.Genres, "/"), truncate(info.Overview, 300))

	return s.callAIForScene(prompt)
}

// GenerateNextScene 根据之前的选择生成下一个关卡
func (s *AdventureService) GenerateNextScene(info *MovieInfo, history []string, currentLevel int) (*AdventureScene, error) {
	historyStr := strings.Join(history, " → ")

	prompt := fmt.Sprintf(`继续文字冒险游戏。

电影：%s (%d年) / 类型：%s
之前的选择路径：%s
当前是第 %d 关。

要求：
1. 场景要承接之前的选择结果，形成连贯的故事
2. 难度要递增——越往后越难，选项越迷惑
3. 选项数量：3-4个，只有1个正确
4. 要利用电影的关键剧情转折点来设计陷阱
5. 有些选项看起来非常正确但其实是陷阱（需要真正了解电影才知道）
6. 每个关卡的风格要符合电影类型（恐怖片要恐怖，喜剧要搞笑，科幻要烧脑）

严格按以下JSON格式返回：
{
  "level": %d,
  "title": "关卡标题",
  "description": "场景描述（150字以内，第一人称）",
  "choices": [
    {"text": "选项文字", "correct": false, "result": "结果描述"},
    {"text": "选项文字", "correct": true, "result": "结果描述"},
    {"text": "选项文字", "correct": false, "result": "结果描述"}
  ],
  "hint": "隐晦提示"
}`, info.Title, info.Year, strings.Join(info.Genres, "/"), historyStr, currentLevel, currentLevel)

	return s.callAIForScene(prompt)
}

// GenerateEndScene 生成结局
func (s *AdventureService) GenerateEndScene(info *MovieInfo, history []string, survived bool) (*AdventureResult, error) {
	historyStr := strings.Join(history, " → ")
	choiceCount := len(history)

	var prompt string
	if survived {
		prompt = fmt.Sprintf(`文字冒险游戏通关了！

电影：%s (%d年) / 类型：%s
玩家的选择路径：%s（共%d关，全部正确）

请生成通关结局。要求：
1. 用第一人称描述主角的最终胜利
2. 要有电影高潮的仪式感
3. 给一个"通关彩蛋"——一个关于这部电影的冷知识或幕后故事
4. 评分根据闯关难度给出（60-100分，越难的电影分数越高）

严格按以下JSON格式返回：
{
  "success": true,
  "final_scene": "最终场景描述（100字以内）",
  "easter_egg": "通关彩蛋（冷知识/幕后故事）",
  "score": 85
}`, info.Title, info.Year, strings.Join(info.Genres, "/"), historyStr, choiceCount)
	} else {
		prompt = fmt.Sprintf(`文字冒险游戏失败了！

电影：%s (%d年) / 类型：%s
玩家的选择路径：%s（共%d关后失败）

请生成失败结局。要求：
1. 用幽默的方式描述失败（不要让玩家觉得被嘲讽，而是觉得好笑）
2. 给出失败的"死因"——要具体且有趣
3. 给一个提示，暗示正确的做法（但不要太明显）
4. 鼓励玩家再试一次

严格按以下JSON格式返回：
{
  "success": false,
  "final_scene": "失败场景描述（80字以内，幽默风格）",
  "death_reason": "具体死因（20字以内，有趣）",
  "tips": "下次试试...（30字以内的提示）",
  "score": 30
}`, info.Title, info.Year, strings.Join(info.Genres, "/"), historyStr, choiceCount)
	}

	return s.callAIForResult(prompt)
}

func (s *AdventureService) callAIForScene(prompt string) (*AdventureScene, error) {
	resp, err := s.callOpenAI(prompt)
	if err != nil {
		return nil, err
	}

	// 解析JSON（处理markdown code block包裹）
	cleaned := cleanAIJSON(resp)

	var scene AdventureScene
	if err := json.Unmarshal([]byte(cleaned), &scene); err != nil {
		logger.Info("[Adventure] JSON parse failed, raw: %s", resp[:min(200, len(resp))])
		return nil, fmt.Errorf("AI返回格式错误: %w", err)
	}

	// 验证至少有一个正确选项
	hasCorrect := false
	for _, c := range scene.Choices {
		if c.Correct {
			hasCorrect = true
			break
		}
	}
	if !hasCorrect && len(scene.Choices) > 0 {
		scene.Choices[0].Correct = true
	}

	return &scene, nil
}

func (s *AdventureService) callAIForResult(prompt string) (*AdventureResult, error) {
	resp, err := s.callOpenAI(prompt)
	if err != nil {
		return nil, err
	}

	cleaned := cleanAIJSON(resp)

	var result AdventureResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		logger.Info("[Adventure] Result JSON parse failed, raw: %s", resp[:min(200, len(resp))])
		return nil, fmt.Errorf("AI返回格式错误: %w", err)
	}

	return &result, nil
}

func (s *AdventureService) callOpenAI(prompt string) (string, error) {
	systemMsg := `你是「求片大冒险」的游戏引擎。你的任务是为电影设计沉浸式的文字冒险关卡。

规则：
1. 只返回JSON格式数据，不要有任何多余文字
2. 所有文字用中文
3. 场景描述要有画面感，像在写电影剧本
4. 选项要设计得有迷惑性，不能让玩家轻松猜对
5. 禁止出现AI八股文（"引人深思"、"不容错过"等）
6. 要利用电影的关键剧情转折点来设计陷阱选项`

	body := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  1500,
		"temperature": 0.9, // 高创意
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", s.openaiBase+"/chat/completions", strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.openaiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API returned %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("AI 返回空结果")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// cleanAIJSON 清理AI返回的JSON（处理markdown code block包裹）
func cleanAIJSON(raw string) string {
	cleaned := strings.TrimSpace(raw)

	// Strip ```json ... ``` wrapper
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock || (!strings.HasPrefix(trimmed, "```") && len(jsonLines) > 0) {
				jsonLines = append(jsonLines, line)
			}
		}
		if len(jsonLines) > 0 {
			cleaned = strings.Join(jsonLines, "\n")
		}
	}

	// Extract { ... } block
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(cleaned, "}"); endIdx > idx {
			cleaned = cleaned[idx : endIdx+1]
		}
	}

	return cleaned
}

// genreIDsToNames TMDB genre ID → 中文名
func genreIDsToNames(ids []int) []string {
	m := map[int]string{
		28: "动作", 12: "冒险", 16: "动画", 35: "喜剧", 80: "犯罪",
		99: "纪录", 18: "剧情", 10751: "家庭", 14: "奇幻", 36: "历史",
		27: "恐怖", 10402: "音乐", 9648: "悬疑", 10749: "爱情", 878: "科幻",
		53: "惊悚", 10752: "战争", 37: "西部", 10759: "动作冒险", 10762: "儿童",
		10763: "新闻", 10764: "真人秀", 10765: "科幻奇幻", 10766: "肥皂剧",
		10767: "脱口秀", 10768: "战争政治",
	}
	var names []string
	seen := map[int]bool{}
	for _, id := range ids {
		if name, ok := m[id]; ok && !seen[id] {
			names = append(names, name)
			seen[id] = true
		}
	}
	if len(names) == 0 {
		names = []string{"未知"}
	}
	return names
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GenerateDeathScene 直接生成一个有趣的死亡场景（用于AI生成失败时的回退）
func (s *AdventureService) GenerateDeathScene(info *MovieInfo, level int) string {
	deaths := []string{
		fmt.Sprintf("你在《%s》的世界里迷路了。作为一个没有主角光环的路人甲，你在第%d关被淘汰。", info.Title, level),
		fmt.Sprintf("很遗憾，你没有主角的直觉。在《%s》的第%d关，你做出了一个普通人会做的选择——但主角不会。", info.Title, level),
		fmt.Sprintf("导演喊了\"卡！\"。在《%s》的片场，你在第%d关NG了。主角在一旁摇头。", info.Title, level),
		fmt.Sprintf("你触发了《%s》的隐藏剧情线——BE线。第%d关，Game Over。", info.Title, level),
	}
	return deaths[rand.Intn(len(deaths))]
}
