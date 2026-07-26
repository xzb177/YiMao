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

// findEmbyUserByName 通过用户名查找 Emby 用户 ID（共享函数）
func findEmbyUserByName(client *http.Client, embyURL, embyAPIKey, name string) (string, error) {
	if embyURL == "" || embyAPIKey == "" {
		return "", fmt.Errorf("Emby 未配置")
	}
	embyURL = strings.TrimRight(embyURL, "/")
	resp, err := embydoGet(client, embyURL, embyAPIKey, "/Users?IsDisabled=false")
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

// fetchEmbyItems 从 Emby 获取用户媒体列表（共享函数）
func fetchEmbyItems(client *http.Client, embyURL, embyAPIKey, userID, itemType string, limit int) ([]PortraitItem, error) {
	if embyURL == "" || embyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}
	embyURL = strings.TrimRight(embyURL, "/")
	path := fmt.Sprintf("/Users/%s/Items/Latest?IncludeItemTypes=%s&Limit=%d&Fields=Genres,CommunityRating",
		userID, itemType, limit)
	resp, err := embydoGet(client, embyURL, embyAPIKey, path)
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

// FindEmbyUserByName 通过用户名查找 Emby 用户 ID（RankService 方法）
func (s *RankService) FindEmbyUserByName(name string) (string, error) {
	return findEmbyUserByName(s.httpClient, s.embyURL, s.embyAPIKey, name)
}

// fetchItems 从 Emby 获取用户媒体列表（RankService 方法，委托给共享函数）
func (s *RankService) fetchItems(userID, itemType string, limit int) ([]PortraitItem, error) {
	return fetchEmbyItems(s.httpClient, s.embyURL, s.embyAPIKey, userID, itemType, limit)
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
	Name   string  `json:"name"`   // 维度名
	Left   string  `json:"left"`   // 左极
	Right  string  `json:"right"`  // 右极
	Score  float64 `json:"score"`  // 0-100，<50偏左，>50偏右
	Result string  `json:"result"` // 最终判定
	Icon   string  `json:"icon"`
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
	return findEmbyUserByName(s.httpClient, s.embyURL, s.embyAPIKey, name)
}

// AnalyzePersonality 分析用户性格
func (s *PersonalityService) AnalyzePersonality(embyUserID, userName string) (*PersonalityResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}

	// 采集数据
	types := []string{"Movie", "Series", "Episode"}
	type fr struct {
		items []PortraitItem
		err   error
		typ   string
	}
	ch := make(chan fr, len(types))
	for _, typ := range types {
		go func(t string) {
			items, err := fetchEmbyItems(s.httpClient, s.embyURL, s.embyAPIKey, embyUserID, t, 100)
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
	Summary     string   `json:"summary"`    // 无剧透概要
	KeyPoints   []string `json:"key_points"` // 关键看点
	Mood        string   `json:"mood"`       // 适合心情
	Similar     []string `json:"similar"`    // 类似电影
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

	prompt := fmt.Sprintf(`帮解说一下《%s》(%d)这部电影。

模式：%s

你就跟你朋友聊天一样说就行，别端着。

要求：
- summary：用你自己的话讲这部电影，就像你在B站录视频那样。有节奏感，有情绪起伏，该快的地方快，该慢的地方慢。250字以内。别平铺直叙，要有你的理解和态度。
- key_points：3个你真心觉得值得说的点。不要说什么"画面精美"这种废话，要说具体的——比如"那场雨中追逐戏，镜头晃得人喘不过气来"。
- mood：用一句大白话描述。比如"失恋的时候看，哭完反而舒服了"，别写"适合在放松时观看"。
- similar：推荐3部真正像的片子，不是同类型就行，得是那种"看完这个意犹未尽可以接着看"的。

JSON格式返回：
{
  "summary": "...",
  "key_points": ["...", "...", "..."],
  "mood": "...",
  "similar": ["...", "...", "..."]
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

	path := fmt.Sprintf("/Items?SearchTerm=%s&IncludeItemTypes=Movie&Limit=5&Fields=Genres,CommunityRating",
		url.QueryEscape(query))
	resp, err := embydoGet(s.httpClient, s.embyURL, s.embyAPIKey, path)
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
			{"role": "system", "content": "你是「毒舌影帝」，一个在B站做了8年电影解说的UP主。你的风格：\n\n1. 说人话。不要用「该片」「本作」「值得一提的是」这种AI腔。就像你坐在朋友旁边聊天一样。\n2. 有自己的态度。喜欢就夸，烂就骂，不要什么都「各有千秋」。\n3. 善用比喻和生活化的例子。比如「这片的节奏就像坐过山车刚到顶突然熄火了」。\n4. 偶尔抖个机灵，但不尬。幽默感是自然流露的，不是硬塞的。\n5. 关键处要有画面感。让读者仿佛看到了那个镜头。\n\n禁止出现：「深刻探讨」「引人深思」「不容错过」「演技炸裂」「教科书级别」这些AI八股文。\n只返回JSON格式数据，不要任何多余文字。"},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  2000,
		"temperature": 0.85,
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
	ID         int      `json:"id"`
	TMDBID     int      `json:"tmdb_id"`
	Title      string   `json:"title"`
	Year       int      `json:"year"`
	Genres     []string `json:"genres"`
	Rating     float64  `json:"rating"`
	Overview   string   `json:"overview"`
	PosterURL  string   `json:"poster_url"`
	MediaType  string   `json:"media_type"` // movie / tv
	Rarity     string   `json:"rarity"`     // N/R/SR/SSR
	IsRevealed bool     `json:"is_revealed"`
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
func (s *BlindBoxService) OpenBlindBox(genreName string, count int) ([]BlindBoxItem, error) {
	if count <= 0 || count > 5 {
		count = 3
	}

	// TMDB genre name → ID 映射
	tmdbGenreMap := map[string]string{
		"动作": "28", "冒险": "12", "动画": "16", "喜剧": "35", "犯罪": "80",
		"纪录": "99", "剧情": "18", "家庭": "10751", "奇幻": "14", "历史": "36",
		"恐怖": "27", "音乐": "10402", "悬疑": "9648", "浪漫": "10749", "科幻": "878",
		"惊悚": "53", "战争": "10752", "西部": "37",
	}

	// 使用 TMDB discover API 随机发现电影
	page := rand.Intn(20) + 1
	url := fmt.Sprintf("https://api.themoviedb.org/3/discover/movie?api_key=%s&language=zh-CN&page=%d&sort_by=popularity.desc&vote_count.gte=100",
		s.tmdbAPIKey, page)
	if genreName != "" {
		if genreID, ok := tmdbGenreMap[genreName]; ok {
			url += "&with_genres=" + genreID
		}
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
	Rating    int       `json:"rating"`  // 1-5星
	Content   string    `json:"content"` // 短评内容
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

		CREATE TABLE IF NOT EXISTS contracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			movie_name TEXT NOT NULL,
			challenge TEXT NOT NULL,
			deadline DATETIME NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			completed_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_contracts_user ON contracts(user_id);
		CREATE INDEX IF NOT EXISTS idx_contracts_status ON contracts(status);
		CREATE TABLE IF NOT EXISTS daily_challenges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			challenge_date TEXT NOT NULL UNIQUE,
			challenge_type TEXT NOT NULL,
			challenge_desc TEXT NOT NULL,
			reward_xp INTEGER DEFAULT 5,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS challenge_completions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			challenge_date TEXT NOT NULL,
			completed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, challenge_date)
		);
		CREATE INDEX IF NOT EXISTS idx_daily_challenges_date ON daily_challenges(challenge_date);
		CREATE INDEX IF NOT EXISTS idx_challenge_completions_user ON challenge_completions(user_id, challenge_date);

		CREATE TABLE IF NOT EXISTS adventure_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			user_name TEXT NOT NULL,
			movie_name TEXT NOT NULL,
			movie_year INTEGER DEFAULT 0,
			score INTEGER DEFAULT 0,
			grade TEXT DEFAULT '',
			max_combo INTEGER DEFAULT 0,
			hp_remaining INTEGER DEFAULT 0,
			levels_completed INTEGER DEFAULT 0,
			total_levels INTEGER DEFAULT 5,
			perfect_run INTEGER DEFAULT 0,
			success INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_adv_user ON adventure_stats(user_id);
		CREATE INDEX IF NOT EXISTS idx_adv_time ON adventure_stats(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_adv_success ON adventure_stats(user_id, success);

		CREATE TABLE IF NOT EXISTS adventure_sessions (
			user_id INTEGER PRIMARY KEY,
			state_json TEXT NOT NULL,
			movie_name TEXT NOT NULL DEFAULT '',
			level INTEGER NOT NULL DEFAULT 1,
			hp INTEGER NOT NULL DEFAULT 100,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS gamble_stash (
			user_id INTEGER PRIMARY KEY,
			items_json TEXT NOT NULL,
			grade TEXT DEFAULT '',
			movie_name TEXT DEFAULT '',
			movie_year INTEGER DEFAULT 0,
			tmdb_id INTEGER DEFAULT 0,
			genres_json TEXT DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS weekly_leaderboard (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			week_start TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			category TEXT NOT NULL,
			value INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(week_start, user_id, category)
		);

		CREATE TABLE IF NOT EXISTS weekly_boss (
			week_start TEXT PRIMARY KEY,
			movie_name TEXT NOT NULL,
			movie_year INTEGER NOT NULL,
			tmdb_id INTEGER NOT NULL,
			genres_json TEXT DEFAULT '[]',
			poster_url TEXT DEFAULT '',
			difficulty_mod REAL DEFAULT 1.3,
			first_clear_user_id INTEGER DEFAULT 0,
			total_attempts INTEGER DEFAULT 0,
			total_clears INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create social tables: %w", err)
	}

	// Migration: add poster_url column if not exists (for existing DBs)
	socialDB := &SocialDB{db: db}
	if err := socialDB.migrateWeeklyBossPosterURL(); err != nil {
		logger.Info("[SocialDB] weekly_boss migration warning: %v", err)
	}

	logger.Info("[SocialDB] Initialized database at %s", dbPath)
	return socialDB, nil
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

// Contract 契约记录
type Contract struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	UserName    string    `json:"user_name"`
	MovieName   string    `json:"movie_name"`
	Challenge   string    `json:"challenge"`
	Deadline    time.Time `json:"deadline"`
	Status      string    `json:"status"` // pending / completed / expired
	CompletedAt time.Time `json:"completed_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// AddContract 添加契约
func (s *SocialDB) AddContract(userID int64, userName, movieName, challenge string, deadline time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"INSERT INTO contracts (user_id, user_name, movie_name, challenge, deadline) VALUES (?, ?, ?, ?, ?)",
		userID, userName, movieName, challenge, deadline.Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("添加契约失败: %w", err)
	}

	id, _ := result.LastInsertId()

	// 记录动态
	s.addEvent(userID, userName, "contract", fmt.Sprintf("签了命运契约：《%s》", movieName))

	return id, nil
}

// CompleteContract 完成契约
func (s *SocialDB) CompleteContract(contractID, userID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"UPDATE contracts SET status = 'completed', completed_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND status = 'pending'",
		contractID, userID,
	)
	if err != nil {
		return false, err
	}

	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

// GetPendingContracts 获取用户的待完成契约
func (s *SocialDB) GetPendingContracts(userID int64) ([]Contract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT id, user_id, user_name, movie_name, challenge, deadline, status, created_at FROM contracts WHERE user_id = ? AND status = 'pending' ORDER BY created_at DESC LIMIT 5",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contracts []Contract
	for rows.Next() {
		var c Contract
		var deadlineStr, createdAtStr string
		if err := rows.Scan(&c.ID, &c.UserID, &c.UserName, &c.MovieName, &c.Challenge, &deadlineStr, &c.Status, &createdAtStr); err != nil {
			continue
		}
		c.Deadline, _ = time.Parse(time.RFC3339, deadlineStr)
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		contracts = append(contracts, c)
	}
	return contracts, nil
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
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Genres    []string `json:"genres"`
	Rating    float64  `json:"rating"`
	Overview  string   `json:"overview"`
	MediaType string   `json:"media_type"`
	SpinCount int      `json:"spin_count"` // 今日已转次数
	MaxSpins  int      `json:"max_spins"`  // 每日最大次数
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

	// 定期清理过期 key（超过1000条时清理非今天的）
	go func() {
		s.mu.Lock()
		if len(s.spinCounts) > 1000 {
			for k := range s.spinCounts {
				if !strings.HasSuffix(k, today) {
					delete(s.spinCounts, k)
				}
			}
		}
		s.mu.Unlock()
	}()

	// 随机选电影
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

	// TMDB genre ID → 中文名映射
	tmdbIDToName := map[int]string{
		28: "动作", 12: "冒险", 16: "动画", 35: "喜剧", 80: "犯罪",
		99: "纪录", 18: "剧情", 10751: "家庭", 14: "奇幻", 36: "历史",
		27: "恐怖", 10402: "音乐", 9648: "悬疑", 10749: "爱情", 878: "科幻",
		53: "惊悚", 10752: "战争", 37: "西部",
	}
	var genres []string
	for _, id := range pick.GenreIDs {
		if name, ok := tmdbIDToName[id]; ok {
			genres = append(genres, name)
		}
	}

	return &RouletteResult{
		Title:     pick.Title,
		Year:      year,
		Genres:    genres,
		Rating:    pick.VoteAverage,
		Overview:  pick.Overview,
		MediaType: "movie",
		SpinCount: spinNum,
		MaxSpins:  3,
	}, nil
}

// ============================================================
//  每日挑战系统 (Daily Challenge)
// ============================================================

// DailyChallenge 每日挑战
type DailyChallenge struct {
	ID          int64  `json:"id"`
	Date        string `json:"date"`
	Type        string `json:"type"`
	Description string `json:"description"`
	RewardXP    int    `json:"reward_xp"`
	Completed   bool   `json:"completed"`
}

// GetDailyChallenge 获取今日挑战
func (s *SocialDB) GetDailyChallenge(userID int64) (*DailyChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	today := time.Now().Format("2006-01-02")

	// 检查是否已有今日挑战
	var challenge DailyChallenge
	err := s.db.QueryRow(
		"SELECT id, challenge_date, challenge_type, challenge_desc, reward_xp FROM daily_challenges WHERE challenge_date = ?",
		today,
	).Scan(&challenge.ID, &challenge.Date, &challenge.Type, &challenge.Description, &challenge.RewardXP)

	if err == sql.ErrNoRows {
		// 生成新挑战
		challengeType, challengeDesc, rewardXP := generateChallenge()
		result, err := s.db.Exec(
			"INSERT INTO daily_challenges (challenge_date, challenge_type, challenge_desc, reward_xp) VALUES (?, ?, ?, ?)",
			today, challengeType, challengeDesc, rewardXP,
		)
		if err != nil {
			return nil, fmt.Errorf("创建挑战失败: %w", err)
		}
		challenge.ID, _ = result.LastInsertId()
		challenge.Date = today
		challenge.Type = challengeType
		challenge.Description = challengeDesc
		challenge.RewardXP = rewardXP
	} else if err != nil {
		return nil, err
	}

	// 检查是否已完成
	var completionID int64
	err = s.db.QueryRow(
		"SELECT id FROM challenge_completions WHERE user_id = ? AND challenge_date = ?",
		userID, today,
	).Scan(&completionID)
	challenge.Completed = (err == nil)

	return &challenge, nil
}

// CompleteDailyChallenge 完成每日挑战
func (s *SocialDB) CompleteDailyChallenge(userID int64, userName string) (bool, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	today := time.Now().Format("2006-01-02")

	// 检查挑战是否存在
	var rewardXP int
	err := s.db.QueryRow(
		"SELECT reward_xp FROM daily_challenges WHERE challenge_date = ?",
		today,
	).Scan(&rewardXP)
	if err != nil {
		return false, 0, fmt.Errorf("今日挑战不存在")
	}

	// 检查是否已完成
	var existing int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM challenge_completions WHERE user_id = ? AND challenge_date = ?",
		userID, today,
	).Scan(&existing)
	if err == nil && existing > 0 {
		return false, 0, nil // 已完成
	}

	// 记录完成
	_, err = s.db.Exec(
		"INSERT INTO challenge_completions (user_id, user_name, challenge_date) VALUES (?, ?, ?)",
		userID, userName, today,
	)
	if err != nil {
		return false, 0, fmt.Errorf("记录完成失败: %w", err)
	}

	// 记录动态
	s.addEvent(userID, userName, "challenge", fmt.Sprintf("完成了今日挑战！+%dXP", rewardXP))

	return true, rewardXP, nil
}

// generateChallenge 生成随机挑战
func generateChallenge() (string, string, int) {
	challenges := []struct {
		Type string
		Desc string
		XP   int
	}{
		{"watch", "看一部你从未看过的类型的电影", 10},
		{"watch", "看一部评分8.0以上的电影", 8},
		{"watch", "看一部2000年以前的经典老片", 8},
		{"watch", "看一部纪录片", 10},
		{"watch", "看一部动画电影", 8},
		{"watch", "看一部超过2小时的电影", 6},
		{"watch", "看一部你朋友推荐的电影", 10},
		{"review", "给一部电影写超过50字的影评", 12},
		{"review", "给一部老电影重新评分", 6},
		{"social", "在影友圈分享一部冷门好片", 10},
		{"social", "挑战：和朋友看同一部电影", 15},
		{"explore", "开一个恐怖盲盒", 8},
		{"explore", "转一次命运轮盘", 8},
		{"explore", "测一下今日情绪画像", 8},
	}

	pick := challenges[rand.Intn(len(challenges))]
	return pick.Type, pick.Desc, pick.XP
}

// ============================================================
//  冒险记录 (Adventure Stats)
// ============================================================

// AdventureRecord 冒险记录
type AdventureRecord struct {
	ID              int
	UserID          int64
	UserName        string
	MovieName       string
	MovieYear       int
	Score           int
	Grade           string
	MaxCombo        int
	HPRemaining     int
	LevelsCompleted int
	TotalLevels     int
	PerfectRun      bool
	Success         bool
	CreatedAt       time.Time
}

// AdventureUserStats 用户冒险统计
type AdventureUserStats struct {
	TotalChallenges int
	TotalSuccess    int
	BestScore       int
	BestGrade       string
	BestCombo       int
	PerfectRuns     int
	RecentRecords   []AdventureRecord
}

// SaveAdventureRecord 保存冒险记录

// AdventureRankEntry 排行榜条目
type AdventureRankEntry struct {
	Rank         int
	UserID       int64
	UserName     string
	MovieName    string
	MovieYear    int
	BestScore    int
	BestGrade    string
	BestCombo    int
	TotalSuccess int
	PerfectRuns  int
}

func (s *SocialDB) SaveAdventureRecord(userID int64, userName, movieName string, movieYear, score int, grade string, maxCombo, hp, levelsCompleted, totalLevels int, perfectRun, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO adventure_stats (user_id, user_name, movie_name, movie_year, score, grade, max_combo, hp_remaining, levels_completed, total_levels, perfect_run, success)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, userName, movieName, movieYear, score, grade, maxCombo, hp, levelsCompleted, totalLevels, boolToInt(perfectRun), boolToInt(success),
	)
	if err != nil {
		return fmt.Errorf("保存冒险记录失败: %w", err)
	}

	// 记录社交动态
	if success {
		s.addEvent(userID, userName, "adventure", fmt.Sprintf("在《%s》大冒险中通关！评级 %s", movieName, grade))
	}

	return nil
}

// SaveAdventureSession 持久化进行中的冒险状态
func (s *SocialDB) SaveAdventureSession(userID int64, stateJSON string, movieName string, level, hp int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO adventure_sessions (user_id, state_json, movie_name, level, hp, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id) DO UPDATE SET
		 state_json=excluded.state_json, movie_name=excluded.movie_name,
		 level=excluded.level, hp=excluded.hp, updated_at=excluded.updated_at`,
		userID, stateJSON, movieName, level, hp,
	)
	if err != nil {
		return fmt.Errorf("保存冒险会话失败: %w", err)
	}
	return nil
}

// LoadAdventureSession 从DB恢复进行中的冒险状态
func (s *SocialDB) LoadAdventureSession(userID int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stateJSON string
	err := s.db.QueryRow(
		"SELECT state_json FROM adventure_sessions WHERE user_id = ?", userID,
	).Scan(&stateJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // 没有进行中的冒险
		}
		return "", fmt.Errorf("加载冒险会话失败: %w", err)
	}
	return stateJSON, nil
}

// DeleteAdventureSession 清除冒险会话（结束/退出时调用）
func (s *SocialDB) DeleteAdventureSession(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM adventure_sessions WHERE user_id = ?", userID)
	if err != nil {
		return fmt.Errorf("删除冒险会话失败: %w", err)
	}
	return nil
}

// CleanStaleAdventureSessions 清理超时的冒险会话（超过2小时视为过期）
func (s *SocialDB) CleanStaleAdventureSessions() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"DELETE FROM adventure_sessions WHERE updated_at < datetime('now', '-2 hours')",
	)
	if err != nil {
		return 0, fmt.Errorf("清理过期冒险会话失败: %w", err)
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// GetAdventureStats 获取用户冒险统计
func (s *SocialDB) GetAdventureStats(userID int64) (*AdventureUserStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &AdventureUserStats{}

	// 总挑战次数和成功次数
	err := s.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(success), 0) FROM adventure_stats WHERE user_id = ?",
		userID,
	).Scan(&stats.TotalChallenges, &stats.TotalSuccess)
	if err != nil {
		return nil, err
	}

	// 最高分、最高评级、最高连击、完美通关
	s.db.QueryRow(
		`SELECT COALESCE(MAX(score), 0), COALESCE(MAX(max_combo), 0), COALESCE(SUM(perfect_run), 0)
		 FROM adventure_stats WHERE user_id = ?`,
		userID,
	).Scan(&stats.BestScore, &stats.BestCombo, &stats.PerfectRuns)

	// 最高评级
	s.db.QueryRow(
		"SELECT COALESCE(grade, '') FROM adventure_stats WHERE user_id = ? ORDER BY score DESC LIMIT 1",
		userID,
	).Scan(&stats.BestGrade)

	// 最近5条记录
	rows, err := s.db.Query(
		`SELECT id, user_id, user_name, movie_name, movie_year, score, grade, max_combo, hp_remaining, levels_completed, total_levels, perfect_run, success, created_at
		 FROM adventure_stats WHERE user_id = ? ORDER BY created_at DESC LIMIT 5`,
		userID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r AdventureRecord
			var perfectInt, successInt int
			err := rows.Scan(&r.ID, &r.UserID, &r.UserName, &r.MovieName, &r.MovieYear, &r.Score, &r.Grade, &r.MaxCombo, &r.HPRemaining, &r.LevelsCompleted, &r.TotalLevels, &perfectInt, &successInt, &r.CreatedAt)
			if err == nil {
				r.PerfectRun = perfectInt != 0
				r.Success = successInt != 0
				stats.RecentRecords = append(stats.RecentRecords, r)
			}
		}
	}

	return stats, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetAdventureLeaderboard 获取冒险排行榜（按最高分排序）
func (s *SocialDB) GetAdventureLeaderboard(limit int) ([]*AdventureRankEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.Query(`
		SELECT user_id,
			   (SELECT user_name FROM adventure_stats n WHERE n.user_id = a1.user_id ORDER BY n.created_at DESC, n.id DESC LIMIT 1) AS user_name,
			   MAX(score) as best_score,
			   (SELECT grade FROM adventure_stats a2 WHERE a2.user_id = a1.user_id ORDER BY score DESC, id DESC LIMIT 1) as best_grade,
			   MAX(max_combo) as best_combo,
			   SUM(success) as total_success,
			   SUM(perfect_run) as perfect_runs
		FROM adventure_stats a1
		WHERE success = 1
		GROUP BY user_id
		ORDER BY best_score DESC, total_success DESC, user_id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []*AdventureRankEntry
	rank := 1
	for rows.Next() {
		var p AdventureRankEntry
		err := rows.Scan(&p.UserID, &p.UserName, &p.BestScore, &p.BestGrade, &p.BestCombo, &p.TotalSuccess, &p.PerfectRuns)
		if err != nil {
			continue
		}
		p.Rank = rank
		players = append(players, &p)
		rank++
	}

	return players, nil
}

// HasDailyChallenge checks whether the user cleared today's recommended movie.
func (s *SocialDB) HasDailyChallenge(userID int64, dateStr, movieName string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM adventure_stats WHERE user_id = ? AND success = 1 AND date(created_at) = ? AND movie_name = ?",
		userID, dateStr, movieName,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AdventureStreak 连胜数据
type AdventureStreak struct {
	CurrentStreak int    // 当前连胜天数
	BestStreak    int    // 最佳连胜天数
	LastPlayDate  string // 最后一次游玩日期
	TodayPlayed   bool   // 今天是否已游玩
}

// GetAdventureStreak 获取用户连胜数据（优化：单次查询）
func (s *SocialDB) GetAdventureStreak(userID int64) (*AdventureStreak, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 单次查询获取所有有游玩记录的日期
	rows, err := s.db.Query(`
		SELECT DISTINCT date(created_at) as play_date
		FROM adventure_stats
		WHERE user_id = ?
		ORDER BY play_date DESC
		LIMIT 30
	`, userID)
	if err != nil {
		return &AdventureStreak{}, nil
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			dates = append(dates, d)
		}
	}

	if len(dates) == 0 {
		return &AdventureStreak{}, nil
	}

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	streak := &AdventureStreak{
		LastPlayDate: dates[0],
		TodayPlayed:  dates[0] == today,
	}

	// 计算当前连胜和最佳连胜（简化：单次遍历）
	currentStreak := 0
	bestStreak := 0

	// 从今天/昨天开始算连续日期
	expectedDate := today
	if !streak.TodayPlayed && len(dates) > 0 && dates[0] == yesterday {
		expectedDate = yesterday
	}

	for _, d := range dates {
		if d == expectedDate {
			currentStreak++
			t, _ := time.Parse("2006-01-02", expectedDate)
			expectedDate = t.AddDate(0, 0, -1).Format("2006-01-02")
		} else {
			break // 断签
		}
	}

	// 最佳连胜 = max(当前连胜, 从历史中断点计算的最大值)
	tempStreak := 0
	expDate := today
	for _, d := range dates {
		if d == expDate {
			tempStreak++
			t, _ := time.Parse("2006-01-02", expDate)
			expDate = t.AddDate(0, 0, -1).Format("2006-01-02")
		} else if d == yesterday && expDate == today && !streak.TodayPlayed {
			expDate = yesterday
			tempStreak = 1
			t, _ := time.Parse("2006-01-02", expDate)
			expDate = t.AddDate(0, 0, -1).Format("2006-01-02")
		} else {
			if tempStreak > bestStreak {
				bestStreak = tempStreak
			}
			tempStreak = 0
			expDate = ""
		}
	}
	if tempStreak > bestStreak {
		bestStreak = tempStreak
	}
	streak.CurrentStreak = currentStreak
	streak.BestStreak = bestStreak
	if currentStreak > bestStreak {
		streak.BestStreak = currentStreak
	}

	return streak, nil
}

// GetRecentWins 获取最近的冒险通关记录（排除指定用户）
func (s *SocialDB) GetRecentWins(excludeUserID int64, since time.Time, limit int) ([]*AdventureRankEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(`
		SELECT user_name, movie_name, movie_year, score, grade, max_combo, perfect_run
		FROM adventure_stats
		WHERE success = 1 AND user_id != ? AND created_at >= ?
		ORDER BY created_at DESC
		LIMIT ?
	`, excludeUserID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AdventureRankEntry
	for rows.Next() {
		var e AdventureRankEntry
		var perfectInt int
		err := rows.Scan(&e.UserName, &e.MovieName, &e.MovieYear, &e.BestScore, &e.BestGrade, &e.BestCombo, &perfectInt)
		if err != nil {
			continue
		}
		e.PerfectRuns = perfectInt
		entries = append(entries, &e)
	}
	return entries, nil
}

// IsFirstSuccess 检查用户是否是首次通关（adventure_stats表中只有一条success=1的记录）
func (s *SocialDB) IsFirstSuccess(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM adventure_stats WHERE user_id = ? AND success = 1",
		userID,
	).Scan(&count)
	if err != nil {
		return false
	}
	return count == 1
}

// IsFirstSuccessThisWeek 检查是否是本周首次通关
func (s *SocialDB) IsFirstSuccessThisWeek(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// 获取本周一日期
	now := time.Now()
	weekday := now.Weekday()
	daysSinceMonday := int(weekday) - 1
	if weekday == time.Sunday {
		daysSinceMonday = 6
	}
	monday := now.AddDate(0, 0, -daysSinceMonday).Format("2006-01-02")

	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM adventure_stats WHERE user_id = ? AND success = 1 AND date(created_at) >= ?",
		userID, monday,
	).Scan(&count)
	if err != nil {
		return false
	}
	return count == 1
}

// GetLastFailedLevel 获取用户在指定电影上最后一次失败的关卡数（用于复仇模式）
func (s *SocialDB) GetLastFailedLevel(userID int64, movieName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var level int
	err := s.db.QueryRow(
		`SELECT COALESCE(levels_completed + 1, 0) FROM adventure_stats
		 WHERE user_id = ? AND movie_name = ? AND success = 0
		 ORDER BY created_at DESC LIMIT 1`,
		userID, movieName,
	).Scan(&level)
	if err != nil {
		return 0
	}
	return level
}

// GetNemesisCount 获取用户被某部电影击败的次数（用于宿敌墙）
func (s *SocialDB) GetNemesisCount(userID int64, movieName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM adventure_stats
		 WHERE user_id = ? AND movie_name = ? AND success = 0`,
		userID, movieName,
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// GetMoviePassRate 获取某部电影的全球通关率（总挑战次数, 通关次数, 通关率%）
func (s *SocialDB) GetMoviePassRate(movieName string) (int, int, float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total, success int
	err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(success), 0) FROM adventure_stats WHERE movie_name = ?`,
		movieName,
	).Scan(&total, &success)
	if err != nil || total == 0 {
		return 0, 0, 0
	}
	rate := float64(success) / float64(total) * 100
	return total, success, rate
}

// GetAdventureGlobalPassRate 获取冒险全局通关率
func (s *SocialDB) GetAdventureGlobalPassRate() (int, int, float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total, success int
	err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(success), 0) FROM adventure_stats`,
	).Scan(&total, &success)
	if err != nil || total == 0 {
		return 0, 0, 0
	}
	rate := float64(success) / float64(total) * 100
	return total, success, rate
}

// ============================================================
//  Gamble Stash 持久化
// ============================================================

// SaveGambleStash 保存赌博暂存（通关后双倍/三倍选择前的等待期）
func (s *SocialDB) SaveGambleStash(userID int64, itemsJSON, grade, movieName string, movieYear, tmdbID int, genresJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO gamble_stash (user_id, items_json, grade, movie_name, movie_year, tmdb_id, genres_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		userID, itemsJSON, grade, movieName, movieYear, tmdbID, genresJSON,
	)
	return err
}

// LoadGambleStash 加载赌博暂存
func (s *SocialDB) LoadGambleStash(userID int64) (itemsJSON, grade, movieName string, movieYear, tmdbID int, genresJSON string, createdAt time.Time, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var t string
	err = s.db.QueryRow(
		`SELECT items_json, grade, movie_name, movie_year, tmdb_id, genres_json, created_at
		 FROM gamble_stash WHERE user_id = ?`, userID,
	).Scan(&itemsJSON, &grade, &movieName, &movieYear, &tmdbID, &genresJSON, &t)
	if err != nil {
		return
	}
	createdAt, _ = time.Parse("2006-01-02 15:04:05", t)
	// 过期检查：超过1小时自动作废
	if time.Since(createdAt) > time.Hour {
		s.DeleteGambleStash(userID)
		return "", "", "", 0, 0, "", time.Time{}, sql.ErrNoRows
	}
	return
}

// DeleteGambleStash 删除赌博暂存
func (s *SocialDB) DeleteGambleStash(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM gamble_stash WHERE user_id = ?`, userID)
	return err
}

// ============================================================
//  连胜烈焰之路 (Streak Rewards)
// ============================================================

// StreakRewards 连胜奖励
type StreakRewards struct {
	FlameLevel    string // 火焰等级名称
	FlameIcon     string // 火焰图标
	BonusHP       int    // 额外初始HP
	FreeSkipTraps int    // 免费跳过陷阱次数
	BossDR        int    // Boss伤害减免%
	ExtraBlindBox bool   // 通关后额外盲盒
	NextLevelDays int    // 距离下一级还需天数
}

// GetStreakRewards 根据连胜天数计算奖励
func GetStreakRewards(streakDays int) *StreakRewards {
	r := &StreakRewards{}
	switch {
	case streakDays >= 7:
		r.FlameLevel = "星焰"
		r.FlameIcon = "🔥"
		r.BonusHP = 10
		r.FreeSkipTraps = 1
		r.BossDR = 30
		r.ExtraBlindBox = true
		r.NextLevelDays = 0
	case streakDays >= 5:
		r.FlameLevel = "金焰"
		r.FlameIcon = "🔥"
		r.BonusHP = 7
		r.FreeSkipTraps = 1
		r.BossDR = 20
		r.NextLevelDays = 7 - streakDays
	case streakDays >= 3:
		r.FlameLevel = "烈焰"
		r.FlameIcon = "🔥"
		r.BonusHP = 3
		r.FreeSkipTraps = 1
		r.NextLevelDays = 5 - streakDays
	case streakDays >= 2:
		r.FlameLevel = "小火苗"
		r.FlameIcon = "🔥"
		r.BonusHP = 1
		r.NextLevelDays = 3 - streakDays
	default:
		r.FlameLevel = "火星"
		r.FlameIcon = "🔥"
		r.NextLevelDays = 2 - streakDays
	}
	return r
}
