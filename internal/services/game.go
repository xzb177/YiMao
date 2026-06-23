package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"

	_ "modernc.org/sqlite"
)

// ============================================================
//  段位系统 (Rank System)
// ============================================================

// RankTier 段位信息
type RankTier struct {
	Name     string // 段位名称
	Icon     string // 段位图标
	MinScore int    // 最低分数
	Color    string // 颜色标识
}

// RankResult 段位计算结果
type RankResult struct {
	UserName     string   `json:"user_name"`
	Tier         RankTier `json:"tier"`
	Score        int      `json:"score"`
	TotalMovies  int      `json:"total_movies"`
	TotalSeries  int      `json:"total_series"`
	GenreCount   int      `json:"genre_count"`
	AvgRating    float64  `json:"avg_rating"`
	WatchDays    int      `json:"watch_days"`
	Badges       []string `json:"badges"`
	NextTier     string   `json:"next_tier"`
	NextTierDiff int      `json:"next_tier_diff"`
	TopGenre     string   `json:"top_genre"`
}

// Ranks 所有段位定义（从低到高）
var Ranks = []RankTier{
	{"青铜", "🥉", 0, "#CD7F32"},
	{"白银", "⚪", 100, "#C0C0C0"},
	{"黄金", "🟡", 300, "#FFD700"},
	{"铂金", "💎", 600, "#00D4FF"},
	{"钻石", "💠", 1000, "#B9F2FF"},
	{"大师", "👑", 1500, "#FF6B6B"},
	{"王者", "🏆", 2200, "#FFD700"},
}

// RankService 段位服务
type RankService struct {
	embyURL    string
	embyAPIKey string
	httpClient *http.Client
}

// NewRankService 创建段位服务
func NewRankService(embyURL, embyAPIKey string) *RankService {
	return &RankService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// FindEmbyUserByName 通过用户名查找 Emby 用户 ID
func (s *RankService) FindEmbyUserByName(name string) (string, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return "", fmt.Errorf("Emby 未配置")
	}
	url := fmt.Sprintf("%s/Users?IsDisabled=false&api_key=%s", s.embyURL, s.embyAPIKey)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Emby Users API returned %d", resp.StatusCode)
	}
	var users []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", err
	}
	for _, u := range users {
		if strings.EqualFold(u.Name, name) {
			return u.ID, nil
		}
	}
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), strings.ToLower(name)) ||
			strings.Contains(strings.ToLower(name), strings.ToLower(u.Name)) {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("未找到匹配的 Emby 用户: %s", name)
}

// CalculateRank 计算用户段位
func (s *RankService) CalculateRank(embyUserID, userName string) (*RankResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}

	// 并发采集数据
	type fetchResult struct {
		items []PortraitItem
		err   error
		typ   string
	}
	types := []string{"Movie", "Series"}
	ch := make(chan fetchResult, len(types))
	for _, typ := range types {
		go func(t string) {
			items, err := s.fetchItems(embyUserID, t, 200)
			ch <- fetchResult{items: items, err: err, typ: t}
		}(typ)
	}

	var allItems []PortraitItem
	for range types {
		r := <-ch
		if r.err != nil {
			logger.Info("[Rank] Failed to fetch %s: %v", r.typ, r.err)
			continue
		}
		allItems = append(allItems, r.items...)
	}

	if len(allItems) == 0 {
		return nil, fmt.Errorf("未找到观影记录")
	}

	// 去重
	seen := make(map[string]bool)
	var items []PortraitItem
	for _, item := range allItems {
		if item.ID != "" && seen[item.ID] {
			continue
		}
		if item.ID != "" {
			seen[item.ID] = true
		}
		items = append(items, item)
	}

	// 计算分数
	result := &RankResult{
		UserName: userName,
	}

	// 1. 基础分：观影数量
	movieCount, seriesCount := 0, 0
	for _, item := range items {
		switch item.Type {
		case "movie":
			movieCount++
		case "series":
			seriesCount++
		}
	}
	result.TotalMovies = movieCount
	result.TotalSeries = seriesCount
	baseScore := movieCount*2 + seriesCount*3

	// 2. 多样性分：类型数量
	genreSet := make(map[string]int)
	var ratings []float64
	for _, item := range items {
		for _, g := range item.Genres {
			genreSet[g]++
		}
		if item.Rating > 0 {
			ratings = append(ratings, item.Rating)
		}
	}
	result.GenreCount = len(genreSet)
	diversityScore := len(genreSet) * 15

	// 3. 品味分：平均评分
	avgRating := 0.0
	if len(ratings) > 0 {
		sum := 0.0
		for _, r := range ratings {
			sum += r
		}
		avgRating = sum / float64(len(ratings))
	}
	result.AvgRating = avgRating
	tasteScore := int(avgRating * 10)

	// 4. 总分
	totalScore := baseScore + diversityScore + tasteScore
	result.Score = totalScore

	// 确定段位
	for i := len(Ranks) - 1; i >= 0; i-- {
		if totalScore >= Ranks[i].MinScore {
			result.Tier = Ranks[i]
			if i < len(Ranks)-1 {
				result.NextTier = Ranks[i+1].Name
				result.NextTierDiff = Ranks[i+1].MinScore - totalScore
			}
			break
		}
	}

	// Top genre
	type gc struct {
		g string
		c int
	}
	var gcs []gc
	for g, c := range genreSet {
		gcs = append(gcs, gc{g, c})
	}
	sort.Slice(gcs, func(i, j int) bool { return gcs[i].c > gcs[j].c })
	if len(gcs) > 0 {
		result.TopGenre = gcs[0].g
	}

	// 成就
	result.Badges = s.calculateBadges(items, genreSet, avgRating, movieCount, seriesCount)

	return result, nil
}

func (s *RankService) calculateBadges(items []PortraitItem, genres map[string]int, avgRating float64, movies, series int) []string {
	var badges []string
	if movies >= 100 {
		badges = append(badges, "🎬 百影达人")
	}
	if movies >= 200 {
		badges = append(badges, "🎥 影史学者")
	}
	if series >= 50 {
		badges = append(badges, "📺 追剧狂魔")
	}
	if len(genres) >= 10 {
		badges = append(badges, "🌈 全能影迷")
	}
	if avgRating >= 8.0 {
		badges = append(badges, "⭐ 品味大师")
	}
	if _, ok := genres["恐怖"]; ok && genres["恐怖"] >= 20 {
		badges = append(badges, "👻 恐怖片收割者")
	}
	if _, ok := genres["科幻"]; ok && genres["科幻"] >= 20 {
		badges = append(badges, "🚀 科幻探险家")
	}
	if _, ok := genres["动画"]; ok && genres["动画"] >= 20 {
		badges = append(badges, "🎨 动画守护者")
	}
	if _, ok := genres["纪录"]; ok && genres["纪录"] >= 15 {
		badges = append(badges, "📖 纪录片行者")
	}
	if _, ok := genres["喜剧"]; ok && genres["喜剧"] >= 20 {
		badges = append(badges, "😂 快乐源泉")
	}
	if len(badges) == 0 {
		badges = append(badges, "🌱 影坛新秀")
	}
	return badges
}

func (s *RankService) fetchItems(userID, itemType string, limit int) ([]PortraitItem, error) {
	url := fmt.Sprintf("%s/Users/%s/Items/Latest?IncludeItemTypes=%s&Limit=%d&Fields=Genres,CommunityRating&api_key=%s",
		s.embyURL, userID, itemType, limit, s.embyAPIKey)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby API returned %d", resp.StatusCode)
	}
	var raw []struct {
		ID              string   `json:"Id"`
		Name            string   `json:"Name"`
		ProductionYear  int      `json:"ProductionYear"`
		Genres          []string `json:"Genres"`
		CommunityRating float64  `json:"CommunityRating"`
		SeriesName      string   `json:"SeriesName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	var items []PortraitItem
	for _, r := range raw {
		name := r.Name
		if r.SeriesName != "" {
			name = r.SeriesName
		}
		items = append(items, PortraitItem{
			ID: r.ID, Name: name, Year: r.ProductionYear,
			Genres: r.Genres, Rating: r.CommunityRating, Type: strings.ToLower(itemType),
		})
	}
	return items, nil
}

// ============================================================
//  性格测试 (Personality Test)
// ============================================================

// PersonalityResult 性格测试结果
type PersonalityResult struct {
	UserName    string       `json:"user_name"`
	Type        string       `json:"type"`        // 如 "ISTJ-A"
	TypeName    string       `json:"type_name"`   // 如 "冷酷理性派"
	Description string       `json:"description"` // 总结描述
	Dimensions  []PDimension `json:"dimensions"`  // 四个维度
	TopTrait    string       `json:"top_trait"`
	MatchRate   string       `json:"match_rate"` // 和大众的匹配度
}

// PDimension 性格维度
type PDimension struct {
	Name    string  `json:"name"`    // 维度名
	Left    string  `json:"left"`    // 左极
	Right   string  `json:"right"`   // 右极
	Score   float64 `json:"score"`   // 0-100，<50偏左，>50偏右
	Result  string  `json:"result"`  // 最终判定
	Icon    string  `json:"icon"`
}

// PersonalityService 性格测试服务
type PersonalityService struct {
	embyURL    string
	embyAPIKey string
	httpClient *http.Client
}

// NewPersonalityService 创建性格测试服务
func NewPersonalityService(embyURL, embyAPIKey string) *PersonalityService {
	return &PersonalityService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// FindEmbyUserByName 通过用户名查找 Emby 用户 ID
func (s *PersonalityService) FindEmbyUserByName(name string) (string, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return "", fmt.Errorf("Emby 未配置")
	}
	url := fmt.Sprintf("%s/Users?IsDisabled=false&api_key=%s", s.embyURL, s.embyAPIKey)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Emby Users API returned %d", resp.StatusCode)
	}
	var users []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", err
	}
	for _, u := range users {
		if strings.EqualFold(u.Name, name) {
			return u.ID, nil
		}
	}
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), strings.ToLower(name)) ||
			strings.Contains(strings.ToLower(name), strings.ToLower(u.Name)) {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("未找到匹配的 Emby 用户: %s", name)
}

// AnalyzePersonality 分析用户性格
func (s *PersonalityService) AnalyzePersonality(embyUserID, userName string) (*PersonalityResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}

	// 采集数据
	rankSvc := NewRankService(s.embyURL, s.embyAPIKey)
	types := []string{"Movie", "Series", "Episode"}
	type fr struct {
		items []PortraitItem
		err   error
		typ   string
	}
	ch := make(chan fr, len(types))
	for _, typ := range types {
		go func(t string) {
			items, err := rankSvc.fetchItems(embyUserID, t, 100)
			ch <- fr{items: items, err: err, typ: t}
		}(typ)
	}
	var allItems []PortraitItem
	for range types {
		r := <-ch
		if r.err == nil {
			allItems = append(allItems, r.items...)
		}
	}
	if len(allItems) == 0 {
		return nil, fmt.Errorf("未找到观影记录")
	}

	// 统计
	genreCount := make(map[string]int)
	var ratings []float64
	for _, item := range allItems {
		for _, g := range item.Genres {
			genreCount[g]++
		}
		if item.Rating > 0 {
			ratings = append(ratings, item.Rating)
		}
	}
	total := len(allItems)

	// 计算四个维度
	result := &PersonalityResult{UserName: userName}

	// 维度1: 类型偏好 (E外向型 vs I内向型)
	actionCount := genreCount["动作"] + genreCount["冒险"] + genreCount["科幻"]
	quietCount := genreCount["剧情"] + genreCount["纪录"] + genreCount["音乐"]
	eScore := 50.0
	if actionCount+quietCount > 0 {
		eScore = float64(actionCount) / float64(actionCount+quietCount) * 100
	}
	eLabel := "I"
	if eScore > 50 {
		eLabel = "E"
	}
	result.Dimensions = append(result.Dimensions, PDimension{
		Name: "能量来源", Left: "I 内向沉浸", Right: "E 外向刺激",
		Score: eScore, Result: eLabel, Icon: "⚡",
	})

	// 维度2: 深度偏好 (S实感型 vs N直觉型)
	deepCount := genreCount["悬疑"] + genreCount["科幻"] + genreCount["奇幻"] + genreCount["纪录"]
	lightCount := genreCount["喜剧"] + genreCount["动作"] + genreCount["爱情"]
	sScore := 50.0
	if deepCount+lightCount > 0 {
		sScore = float64(deepCount) / float64(deepCount+lightCount) * 100
	}
	sLabel := "S"
	if sScore > 50 {
		sLabel = "N"
	}
	result.Dimensions = append(result.Dimensions, PDimension{
		Name: "观影深度", Left: "S 实感享受", Right: "N 直觉探索",
		Score: sScore, Result: sLabel, Icon: "🔍",
	})

	// 维度3: 评判标准 (T思考型 vs F情感型)
	thinkCount := genreCount["犯罪"] + genreCount["悬疑"] + genreCount["战争"] + genreCount["科幻"]
	feelCount := genreCount["爱情"] + genreCount["家庭"] + genreCount["剧情"] + genreCount["动画"]
	tScore := 50.0
	if thinkCount+feelCount > 0 {
		tScore = float64(thinkCount) / float64(thinkCount+feelCount) * 100
	}
	tLabel := "F"
	if tScore > 50 {
		tLabel = "T"
	}
	result.Dimensions = append(result.Dimensions, PDimension{
		Name: "评判标准", Left: "F 情感共情", Right: "T 理性分析",
		Score: tScore, Result: tLabel, Icon: "⚖️",
	})

	// 维度4: 观影节奏 (J计划型 vs P随意型)
	// 用剧集/电影比例判断：追剧多=J计划型，电影多=P随意型
	movieCount := 0
	seriesCount := 0
	for _, item := range allItems {
		if item.Type == "movie" {
			movieCount++
		} else if item.Type == "series" {
			seriesCount++
		}
	}
	jScore := 50.0
	if movieCount+seriesCount > 0 {
		jScore = float64(seriesCount) / float64(movieCount+seriesCount) * 100
	}
	jLabel := "P"
	if jScore > 40 {
		jLabel = "J"
	}
	result.Dimensions = append(result.Dimensions, PDimension{
		Name: "观影节奏", Left: "P 随性探索", Right: "J 计划追剧",
		Score: jScore, Result: jLabel, Icon: "🎯",
	})

	// 组合类型
	typeStr := ""
	for _, d := range result.Dimensions {
		typeStr += d.Result
	}
	typeStr += "-A" // Assertive
	result.Type = typeStr
	result.TypeName = s.getTypeName(typeStr)
	result.Description = s.getDescription(typeStr, genreCount, total, avgRating(ratings))
	result.TopTrait = s.getTopTrait(genreCount)

	return result, nil
}

func avgRating(ratings []float64) float64 {
	if len(ratings) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range ratings {
		sum += r
	}
	return sum / float64(len(ratings))
}

func (s *PersonalityService) getTypeName(t string) string {
	names := map[string]string{
		"ISTJ-A": "冷酷理性派", "ISFJ-A": "温柔守护者", "INFJ-A": "神秘洞察者", "INTJ-A": "战略大师",
		"ISTP-A": "冷静冒险家", "ISFP-A": "文艺灵魂", "INFP-A": "梦想编织者", "INTP-A": "逻辑侦探",
		"ESTP-A": "肾上腺素猎手", "ESFP-A": "快乐制造机", "ENFP-A": "灵感喷泉", "ENTP-A": "辩论艺术家",
		"ESTJ-A": "秩序维护者", "ESFJ-A": "社交蝴蝶", "ENFJ-A": "灵魂导师", "ENTJ-A": "霸道总裁",
	}
	if n, ok := names[t]; ok {
		return n
	}
	return "独特观影者"
}

func (s *PersonalityService) getDescription(t string, genres map[string]int, total int, avg float64) string {
	topGenre := ""
	topCount := 0
	for g, c := range genres {
		if c > topCount {
			topCount = c
			topGenre = g
		}
	}
	return fmt.Sprintf("你是一个%s，看了%d部作品，最爱%s，平均评分%.1f",
		s.getTypeName(t), total, topGenre, avg)
}

func (s *PersonalityService) getTopTrait(genres map[string]int) string {
	traitMap := map[string]string{
		"恐怖": "掌控欲", "惊悚": "刺激寻求", "悬疑": "逻辑驱动", "科幻": "未来思维",
		"奇幻": "想象力丰富", "动画": "审美敏感", "剧情": "共情力强", "喜剧": "压力释放型",
		"爱情": "情感丰富", "动作": "肾上腺素型", "犯罪": "规则意识", "战争": "历史感强",
		"纪录": "求知欲旺盛", "音乐": "感官丰富", "家庭": "归属感强", "冒险": "探索欲强",
	}
	topG := ""
	topC := 0
	for g, c := range genres {
		if c > topC {
			topC = c
			topG = g
		}
	}
	if t, ok := traitMap[topG]; ok {
		return t
	}
	return "全能影迷"
}

// ============================================================
//  AI 电影解说员 (AI Movie Narrator)
// ============================================================

// NarratorResult 解说结果
type NarratorResult struct {
	Title       string   `json:"title"`
	Year        int      `json:"year"`
	Summary     string   `json:"summary"`      // 无剧透概要
	KeyPoints   []string `json:"key_points"`   // 关键看点
	Mood        string   `json:"mood"`          // 适合心情
	Similar     []string `json:"similar"`       // 类似电影
	Rating      float64  `json:"rating"`
	Genres      []string `json:"genres"`
	SpoilerMode bool     `json:"spoiler_mode"` // 是否剧透模式
}

// NarratorService AI解说服务
type NarratorService struct {
	embyURL    string
	embyAPIKey string
	openaiKey  string
	openaiBase string
	model      string
	httpClient *http.Client
}

// NewNarratorService 创建解说服务
func NewNarratorService(embyURL, embyAPIKey, openaiKey, openaiBase, model string) *NarratorService {
	if openaiBase == "" {
		openaiBase = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &NarratorService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		openaiKey:  openaiKey,
		openaiBase: strings.TrimRight(openaiBase, "/"),
		model:      model,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// GenerateNarration 生成电影解说
func (s *NarratorService) GenerateNarration(title string, year int, spoilerMode bool) (*NarratorResult, error) {
	if s.openaiKey == "" {
		return nil, fmt.Errorf("AI 服务未配置（缺少 OpenAI API Key）")
	}

	mode := "无剧透"
	if spoilerMode {
		mode = "剧透模式，可以讲述完整剧情"
	}

	prompt := fmt.Sprintf(`你是一个专业的电影解说员，风格简洁有趣。请为电影《%s》(%d) 生成解说。

模式：%s

要求：
- summary：精炼剧情概要，200字以内，突出核心冲突和亮点，不要流水账
- key_points：3个看点，每个15字以内，一针见血
- mood：一句话描述适合心情
- similar：3部类似电影，只写片名

请严格用JSON格式返回：
{
  "summary": "...",
  "key_points": ["看点1", "看点2", "看点3"],
  "mood": "一句话",
  "similar": ["片名1", "片名2", "片名3"]
}`, title, year, mode)

	resp, err := s.callOpenAI(prompt)
	if err != nil {
		return nil, fmt.Errorf("AI 生成失败: %w", err)
	}

	result := &NarratorResult{
		Title:       title,
		Year:        year,
		SpoilerMode: spoilerMode,
	}

	// 尝试解析JSON
	var parsed struct {
		Summary   string   `json:"summary"`
		KeyPoints []string `json:"key_points"`
		Mood      string   `json:"mood"`
		Similar   []string `json:"similar"`
	}

	// 清理AI返回的markdown代码块包裹
	cleaned := resp
	cleaned = strings.TrimSpace(cleaned)
	if strings.HasPrefix(cleaned, "```") {
		// 去掉 ```json 和 ```
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
	// 尝试提取 { ... } 块
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(cleaned, "}"); endIdx > idx {
			cleaned = cleaned[idx : endIdx+1]
		}
	}

	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		// 解析失败，用原文作为summary
		result.Summary = resp
	} else {
		result.Summary = parsed.Summary
		result.KeyPoints = parsed.KeyPoints
		result.Mood = parsed.Mood
		result.Similar = parsed.Similar
	}

	return result, nil
}

// SearchMovie 搜索电影信息（从Emby）
func (s *NarratorService) SearchMovie(query string) (string, int, []string, float64, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return "", 0, nil, 0, fmt.Errorf("Emby 未配置")
	}

	url := fmt.Sprintf("%s/Items?SearchTerm=%s&IncludeItemTypes=Movie&Limit=5&Fields=Genres,CommunityRating&api_key=%s",
		s.embyURL, url.QueryEscape(query), s.embyAPIKey)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return "", 0, nil, 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Items []struct {
			Name            string   `json:"Name"`
			ProductionYear  int      `json:"ProductionYear"`
			Genres          []string `json:"Genres"`
			CommunityRating float64  `json:"CommunityRating"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, nil, 0, err
	}
	if len(result.Items) == 0 {
		return "", 0, nil, 0, fmt.Errorf("未找到电影: %s", query)
	}
	item := result.Items[0]
	return item.Name, item.ProductionYear, item.Genres, item.CommunityRating, nil
}

func (s *NarratorService) callOpenAI(prompt string) (string, error) {
	body := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是专业的电影解说员，用中文回答。只返回JSON格式数据。"},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  1500,
		"temperature": 0.7,
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

// ============================================================
//  盲盒交易所 (Blind Box)
// ============================================================

// BlindBoxItem 盲盒物品
type BlindBoxItem struct {
	ID          int      `json:"id"`
	TMDBID      int      `json:"tmdb_id"`
	Title       string   `json:"title"`
	Year        int      `json:"year"`
	Genres      []string `json:"genres"`
	Rating      float64  `json:"rating"`
	Overview    string   `json:"overview"`
	PosterURL   string   `json:"poster_url"`
	MediaType   string   `json:"media_type"` // movie / tv
	Rarity      string   `json:"rarity"`     // N/R/SR/SSR
	IsRevealed  bool     `json:"is_revealed"`
}

// BlindBoxService 盲盒服务
type BlindBoxService struct {
	embyURL    string
	embyAPIKey string
	tmdbAPIKey string
	httpClient *http.Client
}

// NewBlindBoxService 创建盲盒服务
func NewBlindBoxService(embyURL, embyAPIKey, tmdbAPIKey string) *BlindBoxService {
	return &BlindBoxService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		tmdbAPIKey: tmdbAPIKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// OpenBlindBox 开盲盒（从TMDB随机推荐）
func (s *BlindBoxService) OpenBlindBox(genre string, count int) ([]BlindBoxItem, error) {
	if count <= 0 || count > 5 {
		count = 3
	}

	// 使用 TMDB discover API 随机发现电影
	page := rand.Intn(20) + 1
	url := fmt.Sprintf("https://api.themoviedb.org/3/discover/movie?api_key=%s&language=zh-CN&page=%d&sort_by=popularity.desc&vote_count.gte=100",
		s.tmdbAPIKey, page)
	if genre != "" {
		url += "&with_genres=" + genre
	}

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("TMDB 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API returned %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ID            int     `json:"id"`
			Title         string  `json:"title"`
			OriginalTitle string  `json:"original_title"`
			ReleaseDate   string  `json:"release_date"`
			GenreIDs      []int   `json:"genre_ids"`
			VoteAverage   float64 `json:"vote_average"`
			Overview      string  `json:"overview"`
			PosterPath    string  `json:"poster_path"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Results) == 0 {
		return nil, fmt.Errorf("未找到电影")
	}

	// 随机选取
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(result.Results), func(i, j int) {
		result.Results[i], result.Results[j] = result.Results[j], result.Results[i]
	})

	var items []BlindBoxItem
	for i, r := range result.Results {
		if i >= count {
			break
		}
		year := 0
		if len(r.ReleaseDate) >= 4 {
			fmt.Sscanf(r.ReleaseDate[:4], "%d", &year)
		}
		rarity := "N"
		if r.VoteAverage >= 7.5 {
			rarity = "R"
		}
		if r.VoteAverage >= 8.0 {
			rarity = "SR"
		}
		if r.VoteAverage >= 8.5 {
			rarity = "SSR"
		}

		posterURL := ""
		if r.PosterPath != "" {
			posterURL = "https://image.tmdb.org/t/p/w500" + r.PosterPath
		}

		items = append(items, BlindBoxItem{
			TMDBID:    r.ID,
			Title:     r.Title,
			Year:      year,
			Rating:    r.VoteAverage,
			Overview:  r.Overview,
			PosterURL: posterURL,
			MediaType: "movie",
			Rarity:    rarity,
		})
	}

	return items, nil
}

// ============================================================
//  社交网络 (Social Network)
// ============================================================

// Review 影评
type Review struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	UserName  string    `json:"user_name"`
	MovieName string    `json:"movie_name"`
	TMDBID    int       `json:"tmdb_id"`
	Rating    int       `json:"rating"`    // 1-5星
	Content   string    `json:"content"`   // 短评内容
	Likes     int       `json:"likes"`
	CreatedAt time.Time `json:"created_at"`
}

// SocialEvent 动态事件
type SocialEvent struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	UserName  string    `json:"user_name"`
	EventType string    `json:"event_type"` // review/achievement/watch
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// SocialDB 社交数据库
type SocialDB struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSocialDB 创建社交数据库
func NewSocialDB(dataDir string) (*SocialDB, error) {
	dbPath := fmt.Sprintf("%s/social.db", dataDir)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open social database: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS reviews (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			movie_name TEXT NOT NULL,
			tmdb_id INTEGER DEFAULT 0,
			rating INTEGER NOT NULL CHECK(rating >= 1 AND rating <= 5),
			content TEXT NOT NULL,
			likes INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_reviews_user ON reviews(user_id);
		CREATE INDEX IF NOT EXISTS idx_reviews_movie ON reviews(movie_name);
		CREATE INDEX IF NOT EXISTS idx_reviews_time ON reviews(created_at DESC);

		CREATE TABLE IF NOT EXISTS review_likes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			review_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(review_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS social_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			event_type TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_events_user ON social_events(user_id);
		CREATE INDEX IF NOT EXISTS idx_events_time ON social_events(created_at DESC);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create social tables: %w", err)
	}

	logger.Info("[SocialDB] Initialized database at %s", dbPath)
	return &SocialDB{db: db}, nil
}

// Close 关闭数据库
func (s *SocialDB) Close() error {
	return s.db.Close()
}

// AddReview 添加影评
func (s *SocialDB) AddReview(userID int64, userName, movieName string, tmdbID, rating int, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rating < 1 || rating > 5 {
		return fmt.Errorf("评分必须在1-5之间")
	}
	if len([]rune(content)) > 500 {
		return fmt.Errorf("影评内容不能超过500字")
	}

	_, err := s.db.Exec(
		"INSERT INTO reviews (user_id, user_name, movie_name, tmdb_id, rating, content) VALUES (?, ?, ?, ?, ?, ?)",
		userID, userName, movieName, tmdbID, rating, content,
	)
	if err != nil {
		return fmt.Errorf("添加影评失败: %w", err)
	}

	// 记录动态
	s.addEvent(userID, userName, "review", fmt.Sprintf("评价了《%s》%s", movieName, stars(rating)))

	return nil
}

// GetRecentReviews 获取最近影评
func (s *SocialDB) GetRecentReviews(limit int) ([]Review, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > 50 {
		limit = 10
	}

	rows, err := s.db.Query(
		"SELECT id, user_id, user_name, movie_name, tmdb_id, rating, content, likes, created_at FROM reviews ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.UserID, &r.UserName, &r.MovieName, &r.TMDBID, &r.Rating, &r.Content, &r.Likes, &r.CreatedAt); err != nil {
			continue
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}

// GetUserReviews 获取用户影评
func (s *SocialDB) GetUserReviews(userID int64, limit int) ([]Review, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.Query(
		"SELECT id, user_id, user_name, movie_name, tmdb_id, rating, content, likes, created_at FROM reviews WHERE user_id = ? ORDER BY created_at DESC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.UserID, &r.UserName, &r.MovieName, &r.TMDBID, &r.Rating, &r.Content, &r.Likes, &r.CreatedAt); err != nil {
			continue
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}

// LikeReview 点赞影评
func (s *SocialDB) LikeReview(reviewID, userID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO review_likes (review_id, user_id) VALUES (?, ?)",
		reviewID, userID,
	)
	if err != nil {
		return false, err
	}

	// 更新点赞数
	_, err = s.db.Exec(
		"UPDATE reviews SET likes = (SELECT COUNT(*) FROM review_likes WHERE review_id = ?) WHERE id = ?",
		reviewID, reviewID,
	)
	return err == nil, err
}

// GetRecentEvents 获取最近动态
func (s *SocialDB) GetRecentEvents(limit int) ([]SocialEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > 50 {
		limit = 20
	}

	rows, err := s.db.Query(
		"SELECT id, user_id, user_name, event_type, content, created_at FROM social_events ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SocialEvent
	for rows.Next() {
		var e SocialEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.EventType, &e.Content, &e.CreatedAt); err != nil {
			continue
		}
		events = append(events, e)
	}
	return events, nil
}

func (s *SocialDB) addEvent(userID int64, userName, eventType, content string) {
	s.db.Exec(
		"INSERT INTO social_events (user_id, user_name, event_type, content) VALUES (?, ?, ?, ?)",
		userID, userName, eventType, content,
	)
}

func stars(n int) string {
	if n < 1 {
		n = 1
	}
	if n > 5 {
		n = 5
	}
	return strings.Repeat("⭐", n)
}

// ============================================================
//  命运轮盘 (Destiny Roulette)
// ============================================================

// RouletteResult 轮盘结果
type RouletteResult struct {
	Title      string   `json:"title"`
	Year       int      `json:"year"`
	Genres     []string `json:"genres"`
	Rating     float64  `json:"rating"`
	Overview   string   `json:"overview"`
	MediaType  string   `json:"media_type"`
	SpinCount  int      `json:"spin_count"`  // 今日已转次数
	MaxSpins   int      `json:"max_spins"`   // 每日最大次数
}

// RouletteService 轮盘服务
type RouletteService struct {
	embyURL    string
	embyAPIKey string
	tmdbAPIKey string
	httpClient *http.Client
	spinCounts map[string]int // "userID:date" -> spin count
	mu         sync.Mutex
}

// NewRouletteService 创建轮盘服务
func NewRouletteService(embyURL, embyAPIKey, tmdbAPIKey string) *RouletteService {
	return &RouletteService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		tmdbAPIKey: tmdbAPIKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		spinCounts: make(map[string]int),
	}
}

// Spin 转轮盘
func (s *RouletteService) Spin(userID int64, genre string) (*RouletteResult, error) {
	// 检查每日限制（原子操作）
	s.mu.Lock()
	today := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("%d:%s", userID, today)
	count := s.spinCounts[key]
	if count >= 3 {
		s.mu.Unlock()
		return nil, fmt.Errorf("今日已转 %d 次，每天最多 3 次哦～明天再来吧！", count)
	}
	s.spinCounts[key] = count + 1
	spinNum := count + 1
	s.mu.Unlock()

	// 随机选电影
	rand.Seed(time.Now().UnixNano())
	page := rand.Intn(50) + 1
	url := fmt.Sprintf("https://api.themoviedb.org/3/discover/movie?api_key=%s&language=zh-CN&page=%d&sort_by=vote_average.desc&vote_count.gte=50&vote_average.gte=6",
		s.tmdbAPIKey, page)
	if genre != "" {
		url += "&with_genres=" + genre
	}

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("TMDB 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API returned %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			Title       string  `json:"title"`
			ReleaseDate string  `json:"release_date"`
			GenreIDs    []int   `json:"genre_ids"`
			VoteAverage float64 `json:"vote_average"`
			Overview    string  `json:"overview"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("轮盘空了...没找到电影")
	}

	// 随机选一部
	pick := result.Results[rand.Intn(len(result.Results))]
	year := 0
	if len(pick.ReleaseDate) >= 4 {
		fmt.Sscanf(pick.ReleaseDate[:4], "%d", &year)
	}

	return &RouletteResult{
		Title:     pick.Title,
		Year:      year,
		Rating:    pick.VoteAverage,
		Overview:  pick.Overview,
		MediaType: "movie",
		SpinCount: spinNum,
		MaxSpins:  3,
	}, nil
}
