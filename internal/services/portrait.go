package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ==================== 数据结构 ====================

// PortraitData 采集到的原始观影数据
type PortraitData struct {
	Items []PortraitItem
}

// PortraitItem 单部作品信息
type PortraitItem struct {
	ID      string // Emby Item ID（用于去重）
	Name    string
	Year    int
	Genres  []string
	Rating  float64
	Type    string // "movie", "series", "episode"
}

// PortraitResult 完整的灵魂画像分析结果
type PortraitResult struct {
	UserName     string             `json:"user_name"`
	TotalItems   int                `json:"total_items"`
	GenrePct     map[string]float64 `json:"genre_pct"`      // 类型→百分比
	TopGenres    []string           `json:"top_genres"`     // 前3类型
	AvgRating    float64            `json:"avg_rating"`     // 平均评分
	RatingCount  int                `json:"rating_count"`   // 有效评分数量
	TasteLevel   string             `json:"taste_level"`    // 品味标尺
	TasteDesc    string             `json:"taste_desc"`     // 品味描述
	RhythmType   string             `json:"rhythm_type"`    // 观影节奏
	RhythmDesc   string             `json:"rhythm_desc"`    // 节奏描述
	PsychTraits  []PsychTrait       `json:"psych_traits"`   // 心理特质
	Surprises    []string           `json:"surprises"`      // 反直觉发现
	BlindSpots   []string           `json:"blind_spots"`    // 盲区
	GenreBar     []GenreBar         `json:"genre_bar"`      // 类型条形图数据
}

// PsychTrait 心理特质
type PsychTrait struct {
	Genre string `json:"genre"`
	Trait string `json:"trait"`
	Desc  string `json:"desc"`
}

// GenreBar 类型偏好条形图
type GenreBar struct {
	Genre string  `json:"genre"`
	Pct   float64 `json:"pct"`
	Bar   string  `json:"bar"`
}

// ==================== 类型→心理映射 ====================

var genrePsychology = map[string]PsychTrait{
	"恐怖": {Genre: "恐怖", Trait: "掌控欲", Desc: "通过虚构恐惧获得掌控感，在安全环境中体验极限情绪"},
	"惊悚": {Genre: "惊悚", Trait: "刺激寻求", Desc: "大脑需要高强度悬念和反转来保持兴奋"},
	"悬疑": {Genre: "悬疑", Trait: "逻辑驱动", Desc: "享受解谜过程，讨厌被蒙在鼓里"},
	"科幻": {Genre: "科幻", Trait: "未来思维", Desc: "对未知世界充满好奇，喜欢推演可能性"},
	"奇幻": {Genre: "奇幻", Trait: "想象力丰富", Desc: "不愿被现实束缚，渴望超越物理法则的世界"},
	"动画": {Genre: "动画", Trait: "审美敏感", Desc: "对视觉艺术有天然亲和力，不拘泥于形式"},
	"剧情": {Genre: "剧情", Trait: "共情力强", Desc: "善于代入他人视角，对人性有深度理解"},
	"喜剧": {Genre: "喜剧", Trait: "压力释放型", Desc: "用幽默对抗生活压力，乐观主义者"},
	"爱情": {Genre: "爱情", Trait: "情感丰富", Desc: "内心柔软，对亲密关系有深层渴望"},
	"动作": {Genre: "动作", Trait: "肾上腺素型", Desc: "追求直接的感官刺激，不喜欢拖沓"},
	"犯罪": {Genre: "犯罪", Trait: "规则意识", Desc: "对秩序与混乱的边界感兴趣"},
	"战争": {Genre: "战争", Trait: "历史感强", Desc: "对宏大叙事和人类命运有思考"},
	"纪录": {Genre: "纪录", Trait: "求知欲旺盛", Desc: "相信真实比虚构更有力量"},
	"音乐": {Genre: "音乐", Trait: "感官丰富", Desc: "对声音和节奏有特殊敏感度"},
	"家庭": {Genre: "家庭", Trait: "归属感强", Desc: "重视家庭关系和温情"},
	"冒险": {Genre: "冒险", Trait: "探索欲强", Desc: "渴望未知体验，不惧风险"},
	"科幻奇幻": {Genre: "科幻奇幻", Trait: "未来思维", Desc: "对未知世界充满好奇，喜欢推演可能性"},
	"动作冒险": {Genre: "动作冒险", Trait: "肾上腺素型", Desc: "追求直接的感官刺激，不喜欢拖沓"},
}

// ==================== PortraitService ====================

// PortraitService 灵魂画像服务
type PortraitService struct {
	embyURL    string
	embyAPIKey string
	httpClient *http.Client
}

// NewPortraitService 创建画像服务
func NewPortraitService(embyURL, embyAPIKey string) *PortraitService {
	return &PortraitService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// FindEmbyUserByName 通过用户名查找 Emby 用户 ID
func (s *PortraitService) FindEmbyUserByName(name string) (string, error) {
	url := fmt.Sprintf("%s/Users?IsDisabled=false&api_key=%s", s.embyURL, s.embyAPIKey)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Emby Users API 返回 %d", resp.StatusCode)
	}

	var users []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return "", err
	}

	// 精确匹配
	for _, u := range users {
		if strings.EqualFold(u.Name, name) {
			return u.ID, nil
		}
	}
	// 模糊匹配（包含）
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), strings.ToLower(name)) ||
			strings.Contains(strings.ToLower(name), strings.ToLower(u.Name)) {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("未找到匹配的 Emby 用户: %s", name)
}

// GeneratePortrait 生成灵魂画像
func (s *PortraitService) GeneratePortrait(embyUserID, userName string) (*PortraitResult, error) {
	if s.embyURL == "" || s.embyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}

	// 1. 采集数据
	data, err := s.collectData(embyUserID)
	if err != nil {
		return nil, fmt.Errorf("数据采集失败: %w", err)
	}

	if len(data.Items) == 0 {
		return nil, fmt.Errorf("未找到观影记录")
	}

	// 2. 分析
	result := s.analyze(data, userName)
	return result, nil
}

// collectData 从 Emby 采集用户观影数据（并发拉取三种类型）
func (s *PortraitService) collectData(userID string) (*PortraitData, error) {
	data := &PortraitData{}
	itemSet := make(map[string]bool) // 用 Item ID 去重

	types := []string{"Movie", "Series", "Episode"}
	type fetchResult struct {
		items []PortraitItem
		err   error
		typ   string
	}

	ch := make(chan fetchResult, len(types))
	for _, typ := range types {
		go func(t string) {
			items, err := s.fetchLatest(userID, t, 50)
			ch <- fetchResult{items: items, err: err, typ: t}
		}(typ)
	}

	for range types {
		r := <-ch
		if r.err != nil {
			logger.Info("[Portrait] Failed to fetch %s: %v", r.typ, r.err)
			continue
		}
		for _, item := range r.items {
			if item.ID != "" && itemSet[item.ID] {
				continue
			}
			if item.ID != "" {
				itemSet[item.ID] = true
			}
			data.Items = append(data.Items, item)
		}
	}

	return data, nil
}

// fetchLatest 从 Emby API 获取最近观看
func (s *PortraitService) fetchLatest(userID, itemType string, limit int) ([]PortraitItem, error) {
	url := fmt.Sprintf("%s/Users/%s/Items/Latest?IncludeItemTypes=%s&Limit=%d&Fields=Genres,CommunityRating,UserData&api_key=%s",
		s.embyURL, userID, itemType, limit, s.embyAPIKey)

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Emby Items/Latest API 返回 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID              string   `json:"Id"`
		Name            string   `json:"Name"`
		ProductionYear  int      `json:"ProductionYear"`
		Genres          []string `json:"Genres"`
		CommunityRating float64  `json:"CommunityRating"`
		SeriesName      string   `json:"SeriesName"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode %s: %w", itemType, err)
	}

	var items []PortraitItem
	for _, r := range raw {
		name := r.Name
		if r.SeriesName != "" {
			name = r.SeriesName
		}
		items = append(items, PortraitItem{
			ID:     r.ID,
			Name:   name,
			Year:   r.ProductionYear,
			Genres: r.Genres,
			Rating: r.CommunityRating,
			Type:   strings.ToLower(itemType),
		})
	}
	return items, nil
}

// analyze 分析观影数据
func (s *PortraitService) analyze(data *PortraitData, userName string) *PortraitResult {
	r := &PortraitResult{
		UserName:   userName,
		TotalItems: len(data.Items),
		GenrePct:   make(map[string]float64),
	}

	// 统计类型
	genreCount := make(map[string]int)
	var ratings []float64

	for _, item := range data.Items {
		for _, g := range item.Genres {
			genreCount[g]++
		}
		if item.Rating > 0 {
			ratings = append(ratings, item.Rating)
		}
	}

	// 计算百分比
	total := 0
	for _, c := range genreCount {
		total += c
	}
	for g, c := range genreCount {
		r.GenrePct[g] = float64(c) / float64(total) * 100
	}

	// 排序取 Top
	type genreCountPair struct {
		genre string
		count int
	}
	var pairs []genreCountPair
	for g, c := range genreCount {
		pairs = append(pairs, genreCountPair{g, c})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].count > pairs[j].count })

	for i, p := range pairs {
		if i >= 10 {
			break
		}
		pct := float64(p.count) / float64(total) * 100
		barLen := int(pct / 3)
		if barLen < 1 {
			barLen = 1
		}
		r.GenreBar = append(r.GenreBar, GenreBar{
			Genre: p.genre,
			Pct:   pct,
			Bar:   strings.Repeat("█", barLen),
		})
	}

	// Top 3 类型
	for i, p := range pairs {
		if i >= 3 {
			break
		}
		r.TopGenres = append(r.TopGenres, p.genre)
	}

	// 平均评分
	if len(ratings) > 0 {
		sum := 0.0
		for _, rat := range ratings {
			sum += rat
		}
		r.AvgRating = sum / float64(len(ratings))
		r.RatingCount = len(ratings)
	}

	// 心理特质（Top 3 类型）
	for _, g := range r.TopGenres {
		if pt, ok := genrePsychology[g]; ok {
			r.PsychTraits = append(r.PsychTraits, pt)
		}
	}

	// 品味标尺
	r.TasteLevel, r.TasteDesc = s.tasteLevel(r.AvgRating, r.RatingCount)

	// 观影节奏
	r.RhythmType, r.RhythmDesc = s.viewingRhythm(r.TotalItems)

	// 反直觉发现
	r.Surprises = s.findSurprises(genreCount, data.Items)

	// 盲区
	r.BlindSpots = s.findBlindSpots(genreCount)

	// 如果没有评分数据，把 AvgRating 标记为 -1（卡片层据此显示"暂无"）
	if r.RatingCount == 0 {
		r.AvgRating = -1
	}

	return r
}

func (s *PortraitService) tasteLevel(avg float64, count int) (string, string) {
	if count == 0 {
		return "未知", "数据不足"
	}
	switch {
	case avg >= 8.0:
		return "🎯 经典猎手", "只看高分，品味精准到可怕"
	case avg >= 7.0:
		return "👑 品质至上", "有品味追求，不轻易妥协"
	case avg >= 6.0:
		return "🌍 兼容并蓄", "愿意尝试各种水平的作品"
	case avg >= 5.0:
		return "🌊 随遇而安", "不挑食，看什么都行"
	default:
		return "🌋 深渊探险家", "专门挑战低分作品的勇士"
	}
}

func (s *PortraitService) viewingRhythm(count int) (string, string) {
	switch {
	case count >= 40:
		return "🔥 影视成瘾者", "你的屏幕时间报告一定很精彩"
	case count >= 20:
		return "📺 日刷型", "几乎每天都在看，真正的影视重度用户"
	case count >= 10:
		return "⚔️ 周末战士", "工作日偶尔看，周末集中刷片"
	case count >= 5:
		return "☕ 休闲观众", "有空才看，不紧不慢"
	default:
		return "🧘 佛系观影", "看得不多，但每部都是精选"
	}
}

func (s *PortraitService) findSurprises(genreCount map[string]int, items []PortraitItem) []string {
	var surprises []string

	if genreCount["恐怖"] >= 2 && genreCount["喜剧"] >= 2 {
		surprises = append(surprises, "你同时是恐怖片狂热者和喜剧爱好者——白天笑嘻嘻，晚上鬼片看不停")
	}
	if genreCount["动画"] >= 2 && genreCount["战争"] >= 2 {
		surprises = append(surprises, "你既能沉浸在动画的梦幻世界，也能直面战争的残酷现实——审美弹性极大")
	}

	// 评分跨度大
	var high, low int
	for _, item := range items {
		if item.Rating >= 8 {
			high++
		} else if item.Rating > 0 && item.Rating < 5 {
			low++
		}
	}
	if high >= 3 && low >= 2 {
		surprises = append(surprises, fmt.Sprintf("你既看%d部神作，也敢碰%d部烂片——好奇心战胜了评分系统", high, low))
	}

	// 文艺+动作
	if genreCount["剧情"] >= 3 && genreCount["动作"] >= 2 {
		surprises = append(surprises, "你既能静下心品味剧情，也能肾上腺素飙升——动静皆宜")
	}

	return surprises
}

func (s *PortraitService) findBlindSpots(genreCount map[string]int) []string {
	allGenres := []string{"恐怖", "惊悚", "悬疑", "科幻", "奇幻", "动画", "剧情", "喜剧", "爱情", "动作", "犯罪", "战争", "纪录", "音乐", "家庭", "冒险"}
	var blind []string
	for _, g := range allGenres {
		if genreCount[g] == 0 {
			blind = append(blind, g)
		}
	}
	if len(blind) > 5 {
		blind = blind[:5]
	}
	return blind
}
