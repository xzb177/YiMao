package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xzb177/yimao/pkg/logger"
)

// ============================================================
//  求片大冒险 v2 — 上瘾机制引擎
// ============================================================

// AdventureScene 一个关卡场景
type AdventureScene struct {
	Level       int              `json:"level"`
	TotalLevels int              `json:"total_levels"`
	Title       string           `json:"title"`
	StageName   string           `json:"stage_name"`   // 阶段名：试炼/抉择/深渊/审判/终局
	Description string           `json:"description"`
	Atmosphere  string           `json:"atmosphere"`   // 氛围词：紧张/诡异/压迫/绝望/史诗
	Choices     []AdventureChoice `json:"choices"`
	Hint        string           `json:"hint,omitempty"`
	Trap        string           `json:"trap,omitempty"` // 陷阱提示（暗示某选项是陷阱但不指明）
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
	Grade       string `json:"grade"`       // SSS/SS/S/A/B/C/D
	DeathReason string `json:"death_reason"`
	Tips        string `json:"tips"`
	Stats       string `json:"stats"`       // 结算统计
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
	Title      string   `json:"title"`
	Year       int      `json:"year"`
	Genres     []string `json:"genres"`
	Overview   string   `json:"overview"`
	Rating     float64  `json:"rating"`
	TMDBID     int      `json:"tmdb_id"`
	Keywords   []string `json:"keywords,omitempty"`   // 剧情关键词
	Cast       []string `json:"cast,omitempty"`       // 主要演员
	Similar    []string `json:"similar,omitempty"`     // 类似电影
	Director   string   `json:"director,omitempty"`   // 导演
	Tagline    string   `json:"tagline,omitempty"`    // 一句话宣传语
	VoteCount  int      `json:"vote_count,omitempty"` // 评价人数
}

// SearchMovieInfo 搜索电影信息
func (s *AdventureService) SearchMovieInfo(query string) (*MovieInfo, error) {
	if s.tmdbAPIKey != "" {
		info, err := s.searchTMDB(query)
		if err == nil && info != nil {
			return info, nil
		}
	}
	if s.embyURL != "" && s.embyAPIKey != "" {
		info, err := s.searchEmby(query)
		if err == nil && info != nil {
			return info, nil
		}
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
		return nil, fmt.Errorf("Emby未找到")
	}
	item := result.Items[0]
	return &MovieInfo{
		Title: item.Name, Year: item.ProductionYear, Genres: item.Genres,
		Overview: item.Overview, Rating: item.CommunityRating,
	}, nil
}

// ============================================================
//  AI 场景生成 — 核心难度引擎
// ============================================================

// GenerateScene 生成指定关卡的场景
func (s *AdventureService) GenerateScene(info *MovieInfo, level int, totalLevels int, history []string, hp int) (*AdventureScene, error) {
	stageNames := map[int]string{1: "序章·试炼", 2: "迷局·抉择", 3: "深渊·审判", 4: "黑暗·献祭", 5: "终局·命运"}
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

	// 每关对应电影叙事结构的不同阶段
	levelFocus := ""
	switch level {
	case 1:
		levelFocus = `聚焦电影的**开端**：主角的身份、处境、世界观设定。
场景必须基于电影的前1/3——主角还没遇到核心冲突时的状态。
考验：玩家是否了解这部电影的基本设定和主角的性格特征。
陷阱设计：用"合理但不符合主角性格"的选项来迷惑。`
	case 2:
		levelFocus = fmt.Sprintf(`聚焦电影的**第一个关键转折**：主角面临的第一个重大选择或遭遇。
场景必须基于电影中一个具体的、有名的情节节点。
考验：玩家是否记得这部电影的具体剧情发展。
陷阱设计：选项中加入一个"大多数%s片都会这样发展"的通用选项——但这部电影偏偏不按套路来。`, strings.Join(info.Genres[:min(2, len(info.Genres))], "/"))
	case 3:
		levelFocus = `聚焦电影的**核心冲突**：主角面对的最大困境或反派。
场景必须涉及电影中最关键的情节转折——那个让观众"卧槽"的时刻。
考验：玩家是否真正理解这部电影的叙事逻辑。
陷阱设计：加入一个wildcard选项——看起来疯狂但恰恰是主角在电影里的真实选择。`
	case 4:
		levelFocus = `聚焦电影的**高潮前夕**：主角做出最艰难决定的时刻。
场景必须基于电影中最具争议性或最反直觉的情节。
考验：玩家能否抛弃"正常人"的思维，用主角的逻辑来思考。
陷阱设计：一个选项代表"道德正确"，一个选项代表"实用主义"，正确选项是"主角实际做的"——往往是最不被理解的那个。`
	case 5:
		levelFocus = `聚焦电影的**结局/主题**：整部电影要表达什么。
场景必须涉及电影的最终选择或结局的深层含义。
考验：玩家是否理解这部电影的核心主题——不只是剧情，而是导演想说什么。
陷阱设计：一个选项是"观众希望的结局"，一个选项是"看起来合理的结局"，正确选项是"电影实际表达的"。`
	}

	prompt := fmt.Sprintf(`你是「求片大冒险」的终极游戏引擎。

## 你的设计哲学
- 你设计的每一关都必须**只属于这部电影**——换一部电影就不成立
- 禁止生成通用模板场景（"你睁开眼睛发现自己在一个世界里"这种废话）
- 必须引用电影中的**具体情节、具体角色、具体场景**
- 选项文字要像电影台词一样——有那部电影的味道
- 陷阱要利用**这部电影的独特逻辑**，不是通用的"认知偏差"

## 电影完整资料
%s

## 当前关卡
第 %d/%d 关：%s
玩家剩余生命：%d%%

## 本关聚焦
%s
%s

## 严格要求
1. 场景描述（150字以内）：
   - 必须提到电影中的具体地点/场景/情境
   - 用第一人称，让玩家感觉真的在那部电影里
   - 有画面感，像在描述一个电影镜头
2. 选项（每个15-25字）：
   - 每个选项都要有具体的行动描述，不能是抽象的"选择A"
   - 正确选项必须对应电影中的真实情节
   - 陷阱选项要利用电影类型特有的套路（比如恐怖片的"别回头"、科幻片的"信任AI"）
   - result描述要有画面感，像电影旁白
3. hint（20字以内）：像一个看过这部电影的人在偷偷提醒朋友
4. atmosphere：从 紧张/诡异/压迫/绝望/史诗/窒息/癫狂 中选一个

## 禁止
- 禁止"你睁开眼睛发现自己在..."这种开头
- 禁止"你面临一个选择"这种废话
- 禁止与电影无关的通用冒险场景
- 禁止选项用"选项A/B/C"这种无聊标签

严格按JSON格式返回：
{
  "level": %d,
  "total_levels": %d,
  "title": "4字以内的关卡标题",
  "stage_name": "%s",
  "description": "场景描述（150字以内，必须涉及电影具体情节）",
  "atmosphere": "氛围词",
  "choices": [
    {"text": "选项文字（15-25字）", "correct": false, "result": "结果描述（30字以内）", "is_trap": true, "is_wildcard": false, "hp_change": 0},
    {"text": "选项文字", "correct": true, "result": "结果描述", "is_trap": false, "is_wildcard": false, "hp_change": 0},
    {"text": "选项文字", "correct": false, "result": "结果描述", "is_trap": false, "is_wildcard": false, "hp_change": 0}
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
		prompt = fmt.Sprintf(`电影冒险失败了。玩家没能活到最后。

电影：%s (%d年) / 类型：%s
路径：%s
最终生命：0%%，最高连击：%d
倒在第几关后失败

生成失败结局。要求：
1. 用黑色幽默描述失败（80字以内）——不要嘲笑玩家，而是让他们觉得"好吧确实是我选错了"
2. 死因要具体且有画面感（15字以内）
3. 给一个真正的提示——暗示正确答案的逻辑（30字以内）
4. 评分：根据到达的关卡
   - 第4-5关失败 → B (45-55)
   - 第2-3关失败 → C (25-40)
   - 第1关就死 → D (10-20)

严格按JSON格式返回：
{
  "success": false,
  "final_scene": "失败场景（80字以内，黑色幽默）",
  "death_reason": "死因（15字以内）",
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
	// 验证
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
		logger.Info("[Adventure] Result JSON parse failed: %s", truncate(resp, 200))
		return nil, fmt.Errorf("AI返回格式错误: %w", err)
	}
	return &result, nil
}

func (s *AdventureService) callOpenAI(prompt string) (string, error) {
	systemMsg := `你是「求片大冒险」的终极游戏引擎。你设计的关卡让玩家又爱又恨。

核心设计原则：
1. 陷阱选项要利用"没看过电影的人的直觉"——越合理的选项越可能是陷阱
2. Wildcard选项要利用"电影和现实的差异"——电影里主角往往做出反直觉的选择
3. 正确答案需要真正理解电影的逻辑——不能靠蒙
4. 每一关的难度必须递增，让玩家感受到"越来越难"
5. 场景描述要有电影感——让人感觉真的在那部电影里
6. 文字风格：紧张、有压迫感、偶尔黑色幽默

禁止出现：「深刻探讨」「引人深思」「不容错过」「演技炸裂」「教科书级别」
只返回JSON格式数据，不要有任何多余文字。`

	body := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  2000,
		"temperature": 0.92,
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
		}
	}
	return cleaned
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

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// GenerateFallbackScene 模板兜底场景
func (s *AdventureService) GenerateFallbackScene(info *MovieInfo, level int, totalLevels int) *AdventureScene {
	stageNames := map[int]string{1: "序章·试炼", 2: "迷局·抉择", 3: "深渊·审判", 4: "黑暗·献祭", 5: "终局·命运"}
	stageName := stageNames[level]
	if stageName == "" {
		stageName = fmt.Sprintf("第%d关", level)
	}

	scenes := map[int]*AdventureScene{
		1: {
			Level: 1, TotalLevels: totalLevels, Title: "初入迷境", StageName: stageName,
			Atmosphere: "紧张",
			Description: fmt.Sprintf("你睁开眼，发现自己身处《%s》的世界。一切如此真实——空气中的气味、远处的声音、脚下土地的触感。一个陌生的声音响起：「你只有一次机会。选错了，就永远留在这里。」", info.Title),
			Choices: []AdventureChoice{
				{Text: "先观察周围环境再行动", Correct: true, Result: "你的谨慎救了你一命。"},
				{Text: "凭直觉选一个方向走", Correct: false, Result: "直觉把你带进了死胡同。", IsTrap: true},
				{Text: "大声呼喊看有没有人", Correct: false, Result: "你的声音引来了不该来的东西。"},
			},
			Hint: "主角一开始也会这样做",
		},
		2: {
			Level: 2, TotalLevels: totalLevels, Title: "致命抉择", StageName: stageName,
			Atmosphere: "压迫",
			Description: fmt.Sprintf("你深入了《%s》的核心区域。前方有两条路，身后传来脚步声。你必须立刻做出决定——但这两条路看起来都不太对劲。", info.Title),
			Choices: []AdventureChoice{
				{Text: "走看起来安全的那条", Correct: false, Result: "安全的路是最危险的陷阱。", IsTrap: true},
				{Text: "走看起来危险的那条", Correct: false, Result: "危险就是危险，不是看起来。"},
				{Text: "不走任何一条，找第三条路", Correct: true, Result: "你发现了隐藏的通道！"},
				{Text: "原路返回", Correct: false, Result: "回头路已经被封死了。"},
			},
			Hint: "在这部电影里，主角从不按常理出牌",
		},
		3: {
			Level: 3, TotalLevels: totalLevels, Title: "深渊凝视", StageName: stageName,
			Atmosphere: "绝望",
			Description: fmt.Sprintf("你终于来到了《%s》最黑暗的时刻。眼前的一切都在考验你的信念。你必须做出一个决定——而这个决定将定义你是谁。", info.Title),
			Choices: []AdventureChoice{
				{Text: "做正确的事，即使代价很大", Correct: false, Result: "你以为的'正确'并不适用于这个世界。", IsTrap: true},
				{Text: "为了生存放弃原则", Correct: false, Result: "没有原则的生存毫无意义。"},
				{Text: "用主角的方式解决问题", Correct: true, Result: "你理解了主角真正的选择。"},
			},
			Hint: "想想主角为什么要这么做",
		},
		4: {
			Level: 4, TotalLevels: totalLevels, Title: "献祭时刻", StageName: stageName,
			Atmosphere: "窒息",
			Description: fmt.Sprintf("《%s》的终章前奏。你手中握着改变一切的钥匙——但每一扇门背后都是未知的代价。时间不多了。", info.Title),
			Choices: []AdventureChoice{
				{Text: "牺牲自己拯救他人", Correct: false, Result: "英雄主义在这里行不通。", IsTrap: true},
				{Text: "保全自己继续前行", Correct: false, Result: "自私的选择带来了更大的灾难。"},
				{Text: "打破规则找到第三条路", Correct: true, Result: "你找到了主角真正的答案！"},
				{Text: "什么都不做，等待时机", Correct: false, Result: "等待让你错过了唯一的机会。"},
			},
			Hint: "主角从不做'正常人'会做的选择",
		},
		5: {
			Level: 5, TotalLevels: totalLevels, Title: "终局审判", StageName: stageName,
			Atmosphere: "史诗",
			Description: fmt.Sprintf("最后的时刻。《%s》的一切都汇聚于此。你的每一个选择都承载着之前的全部重量。这不是选择题——这是命运的审判。", info.Title),
			Choices: []AdventureChoice{
				{Text: "跟随内心的信念走到最后", Correct: false, Result: "信念有时候是最大的幻觉。", IsTrap: true},
				{Text: "接受命运的安排", Correct: false, Result: "命运从来不是被接受的。"},
				{Text: "做出所有人都认为不可能的选择", Correct: true, Result: "这就是主角的答案！"},
				{Text: "用爱和牺牲来结束一切", Correct: false, Result: "这个世界需要的不是牺牲。", IsWildcard: false},
			},
			Hint: "这部电影的核心主题是什么？",
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
		Stats:       fmt.Sprintf("倒在第%d关，距离终点还差%d关", level, totalLevels-level),
	}
}
