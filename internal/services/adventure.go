package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ============================================================
//  电影冒险 v2 — 电影互动关卡引擎
// ============================================================

// AdventureScene 一个关卡场景
type AdventureScene struct {
	Level       int               `json:"level"`
	TotalLevels int               `json:"total_levels"`
	Title       string            `json:"title"`
	StageName   string            `json:"stage_name"` // 阶段名：试炼/抉择/深渊/审判/终局
	Description string            `json:"description"`
	Atmosphere  string            `json:"atmosphere"` // 氛围词：紧张/诡异/压迫/绝望/史诗
	Choices     []AdventureChoice `json:"choices"`
	Hint        string            `json:"hint,omitempty"`
	Trap        string            `json:"trap,omitempty"` // 陷阱提示（暗示某选项是陷阱但不指明）
}

// AdventureChoice 一个选项
type AdventureChoice struct {
	Text       string `json:"text"`
	Correct    bool   `json:"correct"`
	Result     string `json:"result"`
	IsTrap     bool   `json:"is_trap"`     // 看起来正确但其实是陷阱
	IsWildcard bool   `json:"is_wildcard"` // 看起来疯狂但其实是正确答案
	HPChange   int    `json:"hp_change"`   // 自定义HP变化（0=默认扣血）
}

// AdventureResult 冒险结果
type AdventureResult struct {
	Success     bool   `json:"success"`
	FinalScene  string `json:"final_scene"`
	EasterEgg   string `json:"easter_egg"`
	Score       int    `json:"score"`
	Grade       string `json:"grade"` // SSS/SS/S/A/B/C/D
	DeathReason string `json:"death_reason"`
	Tips        string `json:"tips"`
	Stats       string `json:"stats"` // 结算统计
}

// ValidateAdventureScene enforces server invariants. HPChange is accepted for
// wire compatibility but handlers deliberately ignore it when applying damage.
func ValidateAdventureScene(scene *AdventureScene) error {
	if scene == nil {
		return fmt.Errorf("scene is nil")
	}
	if len(scene.Choices) != 4 {
		return fmt.Errorf("adventure scene must have exactly 4 choices")
	}
	correct := 0
	seen := make(map[string]struct{}, 4)
	for i, c := range scene.Choices {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			return fmt.Errorf("choice %d is empty", i)
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("choice %d is duplicated", i)
		}
		seen[key] = struct{}{}
		if c.Correct {
			correct++
			if c.IsTrap {
				return fmt.Errorf("correct choice cannot be a trap")
			}
		}
	}
	if correct != 1 {
		return fmt.Errorf("adventure scene must have exactly one correct choice")
	}
	return nil
}

// AdventureService 电影冒险服务
type AdventureService struct {
	embyURL    string
	embyAPIKey string
	tmdbAPIKey string
	openaiKey  string
	openaiBase string
	model      string
	httpClient *http.Client
	// 性能优化：电影信息缓存（同一部电影不重复请求TMDB）
	movieCache   map[string]*movieCacheEntry
	movieCacheMu sync.RWMutex
}

type movieCacheEntry struct {
	info      *MovieInfo
	expiresAt time.Time
}

// NewAdventureService 创建冒险服务
func NewAdventureService(embyURL, embyAPIKey, tmdbAPIKey, openaiKey, openaiBase, model string) *AdventureService {
	// 共享连接池：复用TCP连接，减少握手开销
	transport := &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &AdventureService{
		embyURL:    strings.TrimRight(embyURL, "/"),
		embyAPIKey: embyAPIKey,
		tmdbAPIKey: tmdbAPIKey,
		openaiKey:  openaiKey,
		openaiBase: openaiBase,
		model:      model,
		httpClient: &http.Client{Timeout: 90 * time.Second, Transport: transport},
		movieCache: make(map[string]*movieCacheEntry),
	}
}

// MovieInfo 电影基本信息
type MovieInfo struct {
	Title     string   `json:"title"`
	MediaType string   `json:"media_type,omitempty"`
	Year      int      `json:"year"`
	Genres    []string `json:"genres"`
	Overview  string   `json:"overview"`
	Rating    float64  `json:"rating"`
	TMDBID    int      `json:"tmdb_id"`
	Keywords  []string `json:"keywords,omitempty"`   // 剧情关键词
	Cast      []string `json:"cast,omitempty"`       // 主要演员
	Similar   []string `json:"similar,omitempty"`    // 类似电影
	Director  string   `json:"director,omitempty"`   // 导演
	Tagline   string   `json:"tagline,omitempty"`    // 一句话宣传语
	VoteCount int      `json:"vote_count,omitempty"` // 评价人数
}

// SearchMovieInfo 搜索电影信息（带缓存）
func (s *AdventureService) SearchMovieInfo(query string) (*MovieInfo, error) {
	// 缓存命中：同一部电影1小时内不重复请求
	cacheKey := strings.ToLower(strings.TrimSpace(query))
	s.movieCacheMu.RLock()
	if entry, ok := s.movieCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		s.movieCacheMu.RUnlock()
		return entry.info, nil
	}
	s.movieCacheMu.RUnlock()

	var info *MovieInfo
	var err error

	if s.tmdbAPIKey != "" {
		info, err = s.searchTMDB(query)
		if err == nil && info != nil {
			s.cacheMovie(cacheKey, info)
			return info, nil
		}
	}
	if s.embyURL != "" && s.embyAPIKey != "" {
		info, err = s.searchEmby(query)
		if err == nil && info != nil {
			s.cacheMovie(cacheKey, info)
			return info, nil
		}
	}
	return nil, fmt.Errorf("未找到电影: %s", query)
}

// cacheMovie 缓存电影信息（1小时TTL）
func (s *AdventureService) cacheMovie(key string, info *MovieInfo) {
	s.movieCacheMu.Lock()
	defer s.movieCacheMu.Unlock()
	// 缓存清理：超过100条删最旧的
	if len(s.movieCache) > 100 {
		oldest := ""
		for k, v := range s.movieCache {
			if oldest == "" || v.expiresAt.Before(s.movieCache[oldest].expiresAt) {
				oldest = k
			}
		}
		if oldest != "" {
			delete(s.movieCache, oldest)
		}
	}
	s.movieCache[key] = &movieCacheEntry{
		info:      info,
		expiresAt: time.Now().Add(1 * time.Hour),
	}
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
			ID           int     `json:"id"`
			Title        string  `json:"title"`
			Name         string  `json:"name"`
			ReleaseDate  string  `json:"release_date"`
			FirstAirDate string  `json:"first_air_date"`
			GenreIDs     []int   `json:"genre_ids"`
			Overview     string  `json:"overview"`
			VoteAverage  float64 `json:"vote_average"`
			VoteCount    int     `json:"vote_count"`
			MediaType    string  `json:"media_type"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("TMDB未找到: %s", query)
	}

	var picked *struct {
		ID          int
		Title       string
		ReleaseDate string
		GenreIDs    []int
		Overview    string
		VoteAverage float64
		VoteCount   int
		MediaType   string
	}
	for _, r := range result.Results {
		if r.MediaType == "movie" || r.MediaType == "tv" {
			picked = &struct {
				ID          int
				Title       string
				ReleaseDate string
				GenreIDs    []int
				Overview    string
				VoteAverage float64
				VoteCount   int
				MediaType   string
			}{r.ID, func() string {
				if r.Title != "" {
					return r.Title
				}
				return r.Name
			}(), func() string {
				if r.ReleaseDate != "" {
					return r.ReleaseDate
				}
				return r.FirstAirDate
			}(), r.GenreIDs, r.Overview, r.VoteAverage, r.VoteCount, r.MediaType}
			break
		}
	}
	if picked == nil {
		return nil, fmt.Errorf("未找到电影/剧集")
	}

	year := 0
	if len(picked.ReleaseDate) >= 4 {
		fmt.Sscanf(picked.ReleaseDate[:4], "%d", &year)
	}

	info := &MovieInfo{
		Title:     picked.Title,
		MediaType: picked.MediaType,
		Year:      year,
		Genres:    genreIDsToNames(picked.GenreIDs),
		Overview:  picked.Overview,
		Rating:    picked.VoteAverage,
		TMDBID:    picked.ID,
		VoteCount: picked.VoteCount,
	}

	// 获取详细信息：关键词、演员、导演、类似电影
	s.enrichMovieDetails(info, picked.MediaType)

	return info, nil
}

// enrichMovieDetails 获取电影详细信息（关键词/演员/类似电影）
func (s *AdventureService) enrichMovieDetails(info *MovieInfo, mediaType string) {
	var detailURL string
	if mediaType == "tv" {
		detailURL = fmt.Sprintf("%s/tv/%d?api_key=%s&language=zh-CN&append_to_response=keywords,credits,similar", TMDBBaseURL, info.TMDBID, s.tmdbAPIKey)
	} else {
		detailURL = fmt.Sprintf("%s/movie/%d?api_key=%s&language=zh-CN&append_to_response=keywords,credits,similar", TMDBBaseURL, info.TMDBID, s.tmdbAPIKey)
	}

	req, err := http.NewRequest("GET", detailURL, nil)
	if err != nil {
		return
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var detail struct {
		Tagline  string `json:"tagline"`
		Keywords struct {
			Keywords []struct {
				Name string `json:"name"`
			} `json:"keywords"`
		} `json:"keywords"`
		Credits struct {
			Cast []struct {
				Name string `json:"name"`
			} `json:"cast"`
			Crew []struct {
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
		Similar struct {
			Results []struct {
				Title string `json:"title"`
				Name  string `json:"name"`
			} `json:"results"`
		} `json:"similar"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return
	}

	info.Tagline = detail.Tagline

	// 关键词
	for _, kw := range detail.Keywords.Keywords {
		if kw.Name != "" {
			info.Keywords = append(info.Keywords, kw.Name)
		}
	}

	// 主要演员（前5）
	for i, c := range detail.Credits.Cast {
		if i >= 5 {
			break
		}
		if c.Name != "" {
			info.Cast = append(info.Cast, c.Name)
		}
	}

	// 导演
	for _, c := range detail.Credits.Crew {
		if c.Job == "Director" && c.Name != "" {
			info.Director = c.Name
			break
		}
	}

	// 类似电影（前5）
	for i, s2 := range detail.Similar.Results {
		if i >= 5 {
			break
		}
		title := s2.Title
		if title == "" {
			title = s2.Name
		}
		if title != "" {
			info.Similar = append(info.Similar, title)
		}
	}
}

func (s *AdventureService) searchEmby(query string) (*MovieInfo, error) {
	path := fmt.Sprintf("/emby/Items?SearchTerm=%s&IncludeItemTypes=Movie,Series&Limit=5&Fields=Genres,CommunityRating",
		query)
	resp, err := embydoGet(s.httpClient, s.embyURL, s.embyAPIKey, path)
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
		return nil, fmt.Errorf("Emby未找到")
	}
	item := result.Items[0]
	return &MovieInfo{
		Title: item.Name, MediaType: "movie", Year: item.ProductionYear, Genres: item.Genres,
		Overview: item.Overview, Rating: item.CommunityRating,
	}, nil
}

// ============================================================
//  AI 场景生成 — 核心难度引擎
// ============================================================

// GenerateScene 生成指定关卡的场景
func (s *AdventureService) GenerateScene(info *MovieInfo, level int, totalLevels int, history []string, hp int) (*AdventureScene, error) {
	stageNames := map[int]string{1: "序章·发现", 2: "转折·选择", 3: "冲突·判断", 4: "抉择·理解", 5: "终章·回响"}
	stageName := stageNames[level]
	if stageName == "" {
		stageName = fmt.Sprintf("第%d关", level)
	}

	historyStr := ""
	if len(history) > 0 {
		historyStr = fmt.Sprintf("\n玩家之前的路径：%s", strings.Join(history, " → "))
	}

	// 构建电影详细信息块
	movieDetail := fmt.Sprintf("片名：%s (%d年)\n类型：%s", info.Title, info.Year, strings.Join(info.Genres, "/"))
	if info.Director != "" {
		movieDetail += fmt.Sprintf("\n导演：%s", info.Director)
	}
	if len(info.Cast) > 0 {
		movieDetail += fmt.Sprintf("\n主演：%s", strings.Join(info.Cast, "、"))
	}
	if info.Tagline != "" {
		movieDetail += fmt.Sprintf("\n宣传语：%s", info.Tagline)
	}
	if len(info.Keywords) > 0 {
		movieDetail += fmt.Sprintf("\n剧情关键词：%s", strings.Join(info.Keywords, "、"))
	}
	if info.Overview != "" {
		movieDetail += fmt.Sprintf("\n剧情简介：%s", truncate(info.Overview, 400))
	}

	// 每关对应电影叙事结构的不同阶段 + 独立心理学设计
	levelFocus := ""
	switch level {
	case 1:
		levelFocus = `【叙事弧：发现】
聚焦电影的**开端世界**——主角还没被卷入风暴时的日常。
场景必须基于电影的前15分钟：主角的身份、职业、性格、生活状态。
关键是**反差**：现在有多平静，后面就有多惨烈。

心理学设计：
- 陷阱选项要利用"没看过这部电影的人的常识"——他们不知道主角的性格
- 正确选项要体现主角**独特的性格特征**（比如主角是个疯子/理性人/浪漫主义者）
- 让玩家觉得"这关很简单"→建立虚假自信→第2关打脸`
	case 2:
		levelFocus = `【叙事弧：裂变】
聚焦电影的**第一个命运转折点**——主角的日常被打破的那个瞬间。
场景必须基于电影中一个**有名的情节节点**（影迷津津乐道的那个场景）。
这个场景要让看过电影的人会心一笑，没看过的人一脸懵。

心理学设计：
- 陷阱选项利用"类型片套路"：恐怖片的"别回头"、动作片的"硬刚"、爱情片的"表白"——但这部电影偏偏不按套路
- 加入一个"半真半假"选项：一半情节是真的，一半是编的，让看过但记不清的人上当
- 让玩家第一次感受到"这部电影比我想象的复杂"`
	case 3:
		levelFocus = `【叙事弧：深渊】
聚焦电影的**核心冲突爆发**——那个让观众集体"卧槽"的时刻。
场景必须涉及电影中**最具争议性的情节转折**——有人觉得合理，有人觉得扯淡的那个。
这是全剧最关键的一幕，也是最容易暴露"你到底看没看过"的一关。

心理学设计：
- 选项难度陡升：4个选项中有2个都"看起来非常对"
- 陷阱选项利用"情感绑架"：用道德高地（牺牲/正义/爱）诱惑——但电影主角做的更冷酷或更复杂
- 加入一个wildcard：看起来疯狂但恰恰是主角的真实选择
- 这一关要有"认知冲突"——玩家会纠结很久`
	case 4:
		levelFocus = `【叙事弧：窒息】
聚焦电影的**高潮前夕**——主角做出最艰难决定的时刻。
场景必须基于电影中**最反直觉、最不被理解**的情节。
正常人绝不会做的选择，主角偏偏做了——为什么？

心理学设计：
- 难度接近不可能：4个选项中有3个都"非常合理"
- 陷阱选项利用"近因效应"：包含电影中的真实台词/关键词，但用在了错误语境
- 正确选项必须是"最不像答案的那个"——需要真正理解主角的动机才能选对
- 这一关的正确率目标：25%
- hint要更隐晦："想想主角为什么与众不同"`
	case 5:
		levelFocus = `【叙事弧：终局】
聚焦电影的**结局与主题**——不只是"发生了什么"，而是"导演想说什么"。
场景必须涉及电影的**最终选择或结局的深层含义**。
这一关考验的不是记忆，而是理解——你真的看懂了这部电影吗？

心理学设计：
- 这是"认知战"而非"记忆战"：即使背熟了剧情也可能选错
- 陷阱选项利用"观众期望"：大多数观众希望的结局 ≠ 电影实际表达的
- 正确选项必须触及电影的**核心主题**（存在主义/自由意志/人性黑暗/爱的本质）
- 让通关的人有"顿悟感"——"原来这部电影讲的是这个！"
- 这一关的正确率目标：20%`
	}

	prompt := fmt.Sprintf(`你是「电影冒险」的互动关卡引擎。请设计有辨识度、逐步深入且公平可理解的五关体验。

## 你的设计哲学
- 你设计的每一关都必须**只属于这部电影**——换一部电影就不成立
- 禁止生成通用模板场景
- 必须引用电影中的**具体情节、具体角色、具体场景、具体台词**
- 陷阱选项要利用**看过电影但记不清细节的人的模糊记忆**

## 电影完整资料
%s

## 当前关卡
第 %d/%d 关：%s
玩家剩余生命：%d%%

## 本关聚焦
%s
%s

## 🔥 终极难度要求（这是最重要的部分）

### 选项设计原则（严格遵守！！！）
1. **必须生成4个选项**，不是3个
2. **4个选项必须全部看起来非常合理**——让玩家觉得每个都可能是对的
3. **正确选项不能是"最明显"的那个**——它应该是看起来"有点奇怪但又说不出为什么"的那个
4. **陷阱选项必须是"看起来最合理、最像标准答案"的**——利用大多数人的直觉偏差
5. **每个错误选项的扣血不同**：
   - is_trap=true：最危险的陷阱（看起来最正确但实际最错）→ 扣60HP
   - 普通错误 → 扣45HP
   - hp_change=-70：终极陷阱（只有Boss关用，看起来绝对正确但致命）
6. **4个选项之间的区别必须极其微妙**——不能一眼看出哪个对哪个错

### 干扰项设计的7种方法（每个关卡可选用2种）
1. **记忆模糊陷阱**：电影中确实发生过类似的事，但发生在不同的角色/时间线上
2. **直觉反转陷阱**：大多数人会本能选择的选项，恰恰是电影中主角没选的
3. **类型套路陷阱**：利用该类型电影的常见套路（恐怖片选"别回头"、动作片选"硬刚"），但这部电影偏偏不按套路
4. **半真半假陷阱**：选项中一半符合电影剧情，一半是编的，让玩家以为全对
5. **情感绑架陷阱**：用"牺牲自我""保护同伴""做正确的事"这类道德高地选项诱惑——但电影主角的实际选择更复杂
6. **细节替换陷阱**：把电影中的A场景的细节安到B场景上，看起来非常像真的
7. **近因效应陷阱**：选项文字里包含电影中的真实台词或关键词，但用在了错误的语境里

### 场景描述要求
- 必须描述电影中的**一个具体场景/时刻/地点**
- 用第二人称"你"，让玩家感觉自己就是主角
- 描述要包含**具体的环境细节**（天气、声音、气味、光线）
- 150字以内，但要有画面感

### result描述要求
- 选对的result：用电影中的台词风格描述胜利（30字以内）
- 选错的result：简洁说明选择与剧情的偏差，让玩家理解线索（30字以内）
- 所有result都要有电影感，不能是通用的"你失败了"

### hint要求
- 20字以内
- 像一个死忠粉在偷偷提醒朋友
- 不能直接说出答案，但要暗示思考方向

## 禁止
- 禁止"你睁开眼睛发现自己在..."这种开头
- 禁止"你面临一个选择"这种废话
- 禁止3个选项——必须4个
- 禁止让正确选项看起来"最安全"或"最合理"
- 禁止让错误选项看起来"明显是错的"
- 禁止通用的冒险场景

严格按JSON格式返回：
{
  "level": %d,
  "total_levels": %d,
  "title": "4字以内的关卡标题",
  "stage_name": "%s",
  "description": "场景描述（150字以内）",
  "atmosphere": "氛围词",
  "choices": [
    {"text": "选项A（15-25字）", "correct": false, "result": "结果（30字）", "is_trap": true, "is_wildcard": false, "hp_change": 0},
    {"text": "选项B", "correct": false, "result": "结果", "is_trap": false, "is_wildcard": false, "hp_change": 0},
    {"text": "选项C", "correct": true, "result": "结果", "is_trap": false, "is_wildcard": false, "hp_change": 0},
    {"text": "选项D", "correct": false, "result": "结果", "is_trap": false, "is_wildcard": false, "hp_change": 0}
  ],
  "hint": "隐晦提示（20字以内）",
  "trap": "陷阱分析（不展示给玩家）"
}`, movieDetail,
		level, totalLevels, stageName, hp,
		levelFocus, historyStr,
		level, totalLevels, stageName)

	return s.callAIForScene(prompt)
}

// GenerateEndScene 生成结局
func (s *AdventureService) GenerateEndScene(info *MovieInfo, history []string, survived bool, hp int, maxCombo int, totalLevels int) (*AdventureResult, error) {
	historyStr := strings.Join(history, " → ")

	var prompt string
	if survived {
		prompt = fmt.Sprintf(`电影冒险通关！玩家以主角身份活了下来。

电影：%s (%d年) / 类型：%s
路径：%s
最终生命：%d%%，最高连击：%d
共%d关

生成通关结局。要求：
1. 用史诗感的语言描述胜利（100字以内）
2. 通关彩蛋：这部电影的一个真正冷知识（不是编的，要是真的幕后故事）
3. 根据表现评分：
   - 生命>80 + 连击>=3 → SSS (95-100)
   - 生命>60 + 连击>=2 → SS (85-94)
   - 生命>40 → S (75-84)
   - 其他 → A (60-74)

严格按JSON格式返回：
{
  "success": true,
  "final_scene": "最终场景（100字以内，史诗感）",
  "easter_egg": "冷知识（真实的幕后故事）",
  "score": 88,
  "grade": "SS",
  "stats": "一句话总结这次冒险",
  "death_reason": "",
  "tips": ""
}`, info.Title, info.Year, strings.Join(info.Genres, "/"), historyStr, hp, maxCombo, totalLevels)
	} else {
		prompt = fmt.Sprintf(`本次电影冒险结束，玩家的生命值归零。

电影：%s (%d年) / 类型：%s
路径：%s
最终生命：0%%，最高连击：%d
本次完成进度

生成本次结局。要求：
1. 用有电影感且克制的语言描述结局（80字以内），不嘲讽、不羞辱玩家
2. 用15字以内说明本次结束原因，具体但不过度渲染伤亡
3. 给一个有效提示——暗示正确答案的判断逻辑（30字以内）
4. 评分：根据到达的关卡
   - 第4-5关结束 → B (45-55)
   - 第2-3关结束 → C (25-40)
   - 第1关结束 → D (10-20)

严格按JSON格式返回：
{
  "success": false,
  "final_scene": "本次结局（80字以内，有电影感）",
  "death_reason": "结束原因（15字以内）",
  "tips": "提示（30字以内）",
  "score": 35,
  "grade": "C",
  "stats": "一句话总结",
  "easter_egg": ""
}`, info.Title, info.Year, strings.Join(info.Genres, "/"), historyStr, maxCombo)
	}

	return s.callAIForResult(prompt)
}

// ============================================================
//  AI 调用层
// ============================================================

func (s *AdventureService) callAIForScene(prompt string) (*AdventureScene, error) {
	resp, err := s.callOpenAI(prompt)
	if err != nil {
		return nil, err
	}
	cleaned := cleanAIJSON(resp)
	var scene AdventureScene
	if err := json.Unmarshal([]byte(cleaned), &scene); err != nil {
		logger.Info("[Adventure] Scene JSON parse failed: %s", truncate(resp, 200))
		return nil, fmt.Errorf("AI返回格式错误: %w", err)
	}
	if err := ValidateAdventureScene(&scene); err != nil {
		return nil, err
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
		logger.Info("[Adventure] Result JSON parse failed: %s", truncate(resp, 200))
		return nil, fmt.Errorf("AI返回格式错误: %w", err)
	}
	return &result, nil
}

func (s *AdventureService) callOpenAI(prompt string) (string, error) {
	systemMsg := `你是「电影冒险」的互动关卡引擎。你要让玩家在具体剧情线索中完成一段紧张但公平的五关体验。

核心设计原则：
1. 每关必须4个选项，选项都应合理，但可通过剧情线索判断
2. 正确选项需要理解电影情节或人物动机，而不是依赖文字套路
3. 干扰项应来自相近情节、角色或类型惯例，避免刻意误导
4. 每关可使用记忆偏差、类型反转、相近细节等方式增加辨识度
5. 难度逐级递增，但每关都应提供足以判断的有效线索
6. 场景描述要有电影感——引用具体地点、天气、声音、角色名
7. 文字风格：有电影感、有张力、克制，失败反馈不嘲讽、不羞辱玩家

关键认知：
- 玩家可能看过这部电影，但大概率记不清所有细节
- 玩家会根据"直觉"选——你的陷阱要利用这种直觉
- 玩家选错后会排除一个选项——剩下3个依然很难选
- 生命值归零后本次挑战结束，但反馈应清楚、友好，并给出再次尝试所需的线索

禁止出现：「深刻探讨」「引人深思」「不容错过」「演技炸裂」「教科书级别」
只返回JSON格式数据，不要有任何多余文字。`

	body := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  4000,
		"temperature": 0.95,
	}
	jsonBody, _ := json.Marshal(body)

	// 重试一次（超时场景）
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			logger.Info("[Adventure] AI call retry #%d", attempt)
			time.Sleep(2 * time.Second)
		}

		req, err := http.NewRequest("POST", s.openaiBase+"/chat/completions", strings.NewReader(string(jsonBody)))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.openaiKey)

		resp, err := s.httpClient.Do(req)
		if err != nil {
			if attempt == 0 && isTimeoutError(err) {
				continue // 超时 → 重试
			}
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
	return "", fmt.Errorf("AI call failed after retry")
}

// ============================================================
//  工具函数
// ============================================================

func cleanAIJSON(raw string) string {
	cleaned := strings.TrimSpace(raw)
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
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(cleaned, "}"); endIdx > idx {
			cleaned = cleaned[idx : endIdx+1]
		} else {
			// JSON被截断，尝试补全
			cleaned = cleaned[idx:]
			cleaned = completeTruncatedJSON(cleaned)
		}
	}
	return cleaned
}

// completeTruncatedJSON 尝试补全被截断的JSON
func completeTruncatedJSON(s string) string {
	openBraces := 0
	openBrackets := 0
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			openBraces++
		} else if c == '}' {
			openBraces--
		} else if c == '[' {
			openBrackets++
		} else if c == ']' {
			openBrackets--
		}
	}

	if inString {
		s += "\""
	}

	for i := 0; i < openBrackets; i++ {
		s += "]"
	}

	for i := 0; i < openBraces; i++ {
		s += "}"
	}

	var test interface{}
	if json.Unmarshal([]byte(s), &test) == nil {
		return s
	}

	return aggressiveJSONFix(s)
}

// aggressiveJSONFix 更激进的JSON修复
func aggressiveJSONFix(s string) string {
	lastComma := strings.LastIndex(s, ",")
	if lastComma > 0 {
		s = s[:lastComma]
	}

	lastQuote := strings.LastIndex(s, "\"")
	if lastQuote > 0 {
		beforeQuote := s[:lastQuote]
		if strings.HasSuffix(beforeQuote, ": ") || strings.HasSuffix(beforeQuote, ":") {
			s = beforeQuote
			lastComma = strings.LastIndex(s, ",")
			if lastComma > 0 {
				s = s[:lastComma]
			}
		}
	}

	openBraces := 0
	openBrackets := 0
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			openBraces++
		} else if c == '}' {
			openBraces--
		} else if c == '[' {
			openBrackets++
		} else if c == ']' {
			openBrackets--
		}
	}

	if inString {
		s += "\""
	}

	for i := 0; i < openBrackets; i++ {
		s += "]"
	}
	for i := 0; i < openBraces; i++ {
		s += "}"
	}

	return s
}
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

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := err.(interface{ Timeout() bool }); ok {
		return netErr.Timeout()
	}
	return strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout")
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// GenerateFallbackScene 模板兜底场景（类型感知版）
func (s *AdventureService) GenerateFallbackScene(info *MovieInfo, level int, totalLevels int) *AdventureScene {
	stageNames := map[int]string{1: "序章·发现", 2: "转折·选择", 3: "冲突·判断", 4: "抉择·理解", 5: "终章·回响"}
	stageName := stageNames[level]
	if stageName == "" {
		stageName = fmt.Sprintf("第%d关", level)
	}

	title := info.Title
	genre := ""
	if len(info.Genres) > 0 {
		genre = info.Genres[0]
	}
	director := info.Director
	cast := ""
	if len(info.Cast) > 0 {
		cast = info.Cast[0]
	}

	// 根据类型生成不同的场景氛围和陷阱风格
	genreAtmosphere := map[string]string{
		"科幻": "未来感", "悬疑": "诡异", "惊悚": "窒息", "恐怖": "阴森",
		"动作": "紧绷", "犯罪": "压抑", "喜剧": "荒诞", "爱情": "暧昧",
		"动画": "奇幻", "剧情": "沉重", "战争": "残酷", "奇幻": "迷幻",
	}
	atmosphere := genreAtmosphere[genre]
	if atmosphere == "" {
		atmosphere = "紧张"
	}

	// 根据类型生成不同的场景描述前缀
	genrePrefix := map[string]string{
		"科幻": "霓虹灯在头顶闪烁，空气中弥漫着金属和臭氧的味道",
		"悬疑": "走廊尽头的灯忽明忽暗，墙上挂着一幅歪斜的画",
		"惊悚": "你的后背发凉，总觉得有人在暗处盯着你",
		"恐怖": "远处传来低沉的呻吟，地板上的血迹还没有干透",
		"动作": "肾上腺素飙升，你的心跳快到能听到自己的脉搏",
		"犯罪": "巷子里弥漫着烟味和潮湿的气息，远处传来警笛",
		"喜剧": "一切看起来都很正常——但你总觉得哪里不对劲",
		"爱情": "空气中飘着花香，但你的心里却有一丝不安",
		"动画": "周围的色彩突然变得鲜艳得不真实，像走进了一幅画",
		"剧情": "沉默笼罩着一切，你能感受到空气中未说出口的秘密",
	}
	prefix := genrePrefix[genre]
	if prefix == "" {
		prefix = "空气凝固了，你能感觉到命运的齿轮开始转动"
	}

	// 根据类型生成不同的陷阱风格
	genreTraps := map[string]struct {
		trap1, trap2, trap3, trap4         string
		result1, result2, result3, result4 string
	}{
		"科幻": {
			"按照逻辑推演做出最优选择", "相信科技能解决一切问题", "凭直觉选最不合理的那个", "模仿电影中AI的决策模式",
			"逻辑在这里是最危险的陷阱。", "科技有时候是最大的幻觉。", "直觉这次救了你一命。", "你读懂了这部电影的AI。",
		},
		"悬疑": {
			"相信你看到的第一条线索", "跟着最可疑的人走", "停下来重新审视所有细节", "相信那个看起来最无辜的人",
			"第一条线索往往是诱饵。", "最可疑的人恰恰是烟雾弹。", "你的耐心让你发现了真相。", "天真在这里是最危险的。",
		},
		"恐怖": {
			"立刻逃跑，远离危险源", "躲在看起来最安全的地方", "面对恐惧，走向声源", "装死，等待危险过去",
			"逃跑只会让你陷入更大的陷阱。", "安全的地方往往是最危险的。", "面对恐惧是你唯一的出路。", "装死？在这部电影里没有用。",
		},
		"动作": {
			"正面硬刚，用力量碾压", "先撤退，等援军到了再打", "用最出人意料的方式反击", "尝试谈判，避免正面冲突",
			"硬刚在这里是最蠢的选择。", "撤退？时间不站在你这边。", "你找到了主角的节奏！", "谈判？敌人从不讲道理。",
		},
		"犯罪": {
			"跟着证据走，相信法律", "信任那个给你承诺的人", "用非常规手段接近真相", "保持沉默，等待时机",
			"证据有时候是被精心设计的。", "承诺在这里是最廉价的货币。", "你理解了这个世界的规则。", "沉默不会保护你。",
		},
		"爱情": {
			"勇敢表白，说出心里话", "默默守护，等待时机", "做出牺牲来成全对方", "放下一切，跟随感觉走",
			"表白有时候是最自私的选择。", "等待只会让你错过最后的机会。", "牺牲？这部电影要的不是牺牲。", "感觉把你带到了正确的方向。",
		},
	}
	trap := genreTraps[genre]
	if trap.trap1 == "" {
		trap = struct {
			trap1, trap2, trap3, trap4         string
			result1, result2, result3, result4 string
		}{
			"选择看起来最安全的路", "凭直觉选最不显眼的", "先观察再行动", "跟着大多数人的选择",
			"安全的表象下藏着最深的陷阱。", "直觉这次背叛了你。", "你的细心救了你一命。", "从众心理让你错过了答案。",
		}
	}

	// 根据导演/演员生成个性化 hint
	directorHint := ""
	if director != "" {
		directorHints := map[string]string{
			"诺兰":       "时间从来不是线性的",
			"昆汀":       "暴力也可以很美学",
			"王家卫":      "留白比台词更重要",
			"大卫·芬奇":    "真相藏在细节里",
			"奉俊昊":      "阶级是看不见的墙",
			"克里斯托弗·诺兰": "时间从来不是线性的",
			"昆汀·塔伦蒂诺":  "暴力也可以很美学",
		}
		directorHint = directorHints[director]
	}
	if directorHint == "" && cast != "" {
		directorHint = fmt.Sprintf("想想%s在这部电影里的表演风格", cast)
	}
	if directorHint == "" {
		directorHint = "这部电影的主角从不走寻常路"
	}

	scenes := map[int]*AdventureScene{
		1: {
			Level: 1, TotalLevels: totalLevels, Title: "迷雾初现", StageName: stageName,
			Atmosphere:  atmosphere,
			Description: fmt.Sprintf(`你站在《%s》的起点。%s。导演%s的镜头下，一切都暗藏玄机。你面前有四条路，每一条都似曾相识，但你总觉得哪里不对。`, title, prefix, director),
			Choices: []AdventureChoice{
				{Text: trap.trap1, Correct: false, Result: trap.result1, IsTrap: true},
				{Text: trap.trap2, Correct: false, Result: trap.result2},
				{Text: trap.trap3, Correct: true, Result: trap.result3},
				{Text: trap.trap4, Correct: false, Result: trap.result4},
			},
			Hint: directorHint,
		},
		2: {
			Level: 2, TotalLevels: totalLevels, Title: "致命诱惑", StageName: stageName,
			Atmosphere:  atmosphere,
			Description: fmt.Sprintf(`你深入了《%s》的核心地带。%s。每一个选项都像是正确答案——但只有一个能让你活着走出去。`, title, prefix),
			Choices: []AdventureChoice{
				{Text: trap.trap1, Correct: false, Result: trap.result1, IsTrap: true},
				{Text: trap.trap2, Correct: false, Result: trap.result2},
				{Text: trap.trap3, Correct: true, Result: trap.result3},
				{Text: trap.trap4, Correct: false, Result: trap.result4},
			},
			Hint: fmt.Sprintf("想想%s面对类似情况时的第一反应", cast),
		},
		3: {
			Level: 3, TotalLevels: totalLevels, Title: "深渊凝视", StageName: stageName,
			Atmosphere:  atmosphere,
			Description: fmt.Sprintf(`你终于来到了《%s》最黑暗的时刻。%s。四面八方都是镜子，每一面都映照出不同的你。但只有一个影像是真实的——你需要找到它。`, title, prefix),
			Choices: []AdventureChoice{
				{Text: trap.trap1, Correct: false, Result: trap.result1},
				{Text: trap.trap2, Correct: false, Result: trap.result2, IsTrap: true},
				{Text: trap.trap3, Correct: true, Result: trap.result3},
				{Text: trap.trap4, Correct: false, Result: trap.result4},
			},
			Hint: "在这部电影里，眼见不一定为实",
		},
		4: {
			Level: 4, TotalLevels: totalLevels, Title: "关键抉择", StageName: stageName,
			Atmosphere:  atmosphere,
			Description: fmt.Sprintf(`《%s》的终章前奏。%s。你手中握着改变局面的线索，每个选项都对应不同的人物动机。先回想角色一路以来的选择，再作判断。`, title, prefix),
			Choices: []AdventureChoice{
				{Text: trap.trap1, Correct: false, Result: trap.result1, IsTrap: true},
				{Text: trap.trap2, Correct: false, Result: trap.result2},
				{Text: trap.trap3, Correct: false, Result: trap.result3},
				{Text: trap.trap4, Correct: true, Result: trap.result4},
			},
			Hint: directorHint,
		},
		5: {
			Level: 5, TotalLevels: totalLevels, Title: "终章回响", StageName: stageName,
			Atmosphere:  atmosphere,
			Description: fmt.Sprintf(`最后的时刻。《%s》的一切都汇聚于此。%s。之前出现过的线索会在这里彼此照应；理解角色真正想守住的东西，再作出最后选择。`, title, prefix),
			Choices: []AdventureChoice{
				{Text: trap.trap1, Correct: false, Result: trap.result1, IsTrap: true},
				{Text: trap.trap2, Correct: false, Result: trap.result2},
				{Text: trap.trap3, Correct: true, Result: trap.result3},
				{Text: trap.trap4, Correct: false, Result: trap.result4},
			},
			Hint: fmt.Sprintf("这部电影的核心主题是%s", genre),
		},
	}

	if scene, ok := scenes[level]; ok {
		return scene
	}
	return scenes[1]
}

// GenerateFallbackResult 模板兜底结局
func (s *AdventureService) GenerateFallbackResult(info *MovieInfo, survived bool, hp int, level int, totalLevels int) *AdventureResult {
	if survived {
		grade := "A"
		score := 70
		if hp > 80 {
			grade = "SS"
			score = 90
		} else if hp > 60 {
			grade = "S"
			score = 80
		}
		return &AdventureResult{
			Success:    true,
			FinalScene: fmt.Sprintf("你以主角的身份，走完了《%s》的每一个关键时刻。当最后一幕落下，你终于明白——这部电影讲的不只是故事，是选择。", info.Title),
			Score:      score,
			Grade:      grade,
			EasterEgg:  fmt.Sprintf("《%s》的导演曾说过，这部电影最初的结局完全不同。", info.Title),
			Stats:      fmt.Sprintf("剩余生命 %d%%，经历了%d道关卡的考验", hp, totalLevels),
		}
	}
	return &AdventureResult{
		Success:     false,
		FinalScene:  fmt.Sprintf("你在《%s》的世界里倒下了。也许下一次，你会做出不同的选择。", info.Title),
		DeathReason: "做出了普通人会做的选择",
		Score:       20 + level*5,
		Grade:       "D",
		Tips:        "这部电影的主角从不按常理出牌",
		Stats:       fmt.Sprintf("完成至第%d关，距离终点还有%d关", level, totalLevels-level),
	}
}
