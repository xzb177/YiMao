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
	Title    string   `json:"title"`
	Year     int      `json:"year"`
	Genres   []string `json:"genres"`
	Overview string   `json:"overview"`
	Rating   float64  `json:"rating"`
	TMDBID   int      `json:"tmdb_id"`
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
			MediaType    string  `json:"media_type"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Results) == 0 {
		return nil, fmt.Errorf("TMDB未找到: %s", query)
	}
	for _, r := range result.Results {
		if r.MediaType == "movie" || r.MediaType == "tv" {
			title := r.Title
			if title == "" {
				title = r.Name
			}
			date := r.ReleaseDate
			if date == "" {
				date = r.FirstAirDate
			}
			year := 0
			if len(date) >= 4 {
				fmt.Sscanf(date[:4], "%d", &year)
			}
			return &MovieInfo{
				Title: title, Year: year, Genres: genreIDsToNames(r.GenreIDs),
				Overview: r.Overview, Rating: r.VoteAverage, TMDBID: r.ID,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到电影/剧集")
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

	// 根据关卡递增难度
	difficultyGuide := ""
	switch level {
	case 1:
		difficultyGuide = `难度：中等。1个陷阱选项（看起来合理但电影里不是这样发展的）。
让玩家建立虚假信心——第一个陷阱要"差点骗到"但仔细想想能避开。`
	case 2:
		difficultyGuide = `难度：高。2个陷阱选项。其中一个必须是"大多数人都会选的"但其实是错的。
利用认知偏差：人们倾向于选择看起来"安全"的选项，但在这部电影里，主角恰恰做了相反的事。`
	case 3:
		difficultyGuide = `难度：很高。2个陷阱+1个wildcard（看起来疯狂但其实正确）。
利用这部电影的关键转折点。如果没看过电影，几乎不可能选对。
陷阱要利用"常识性错误"——用现实生活中的合理逻辑来迷惑玩家，但电影世界有自己的规则。`
	case 4:
		difficultyGuide = `难度：极高。3个选项全部有迷惑性。正确答案看起来最不可能。
利用道德困境：一个选项看起来是"正确的事"但电影里主角没这么做；另一个看起来"不道德"但恰恰是主角的选择。
这是"献祭"关——玩家必须抛弃常识，真正理解主角的动机。`
	case 5:
		difficultyGuide = `难度：地狱级。4个选项，每一个都有理由选。
利用电影的终极悬念。正确答案需要理解整部电影的主题和主角的核心信念。
一个选项是"大多数人希望的结局"，一个选项是"看起来合理的结局"，一个选项是"黑暗结局"，正确选项是"电影实际的结局"。
如果玩家到了这一关，说明他真的在认真玩——但越认真越容易被自己的期望误导。`
	}

	prompt := fmt.Sprintf(`你是「求片大冒险」的终极游戏引擎。你的设计哲学：
- 每一个错误选项都要让玩家事后觉得"我怎么没想到"
- 正确答案不能靠猜，必须真正理解这部电影
- 利用玩家的"常识偏见"和"道德直觉"来设陷阱
- 让玩家在选择时犹豫不决——这才是好游戏

电影信息：
- 片名：%s (%d年)
- 类型：%s
- 简介：%s
%s

当前是第 %d/%d 关，阶段名：%s
玩家剩余生命：%d%%

%s

严格要求：
1. 用第一人称叙事，150字以内，要有画面感和紧张感
2. 氛围词必须符合当前关卡的紧张程度
3. 每个选项20字以内
4. result描述30字以内，要让玩家感受到选择的后果
5. trap字段说明哪个选项是陷阱以及为什么
6. hint要非常隐晦——像是给真正看过电影的人的暗示
7. 所有文字用中文

严格按以下JSON格式返回，不要有任何多余文字：
{
  "level": %d,
  "total_levels": %d,
  "title": "关卡标题（简短有力，4字以内）",
  "stage_name": "%s",
  "description": "场景描述（第一人称，有画面感，150字以内）",
  "atmosphere": "氛围词（1个词：紧张/诡异/压迫/绝望/史诗/窒息/癫狂）",
  "choices": [
    {"text": "选项文字", "correct": false, "result": "结果描述", "is_trap": true, "is_wildcard": false, "hp_change": 0},
    {"text": "选项文字", "correct": true, "result": "结果描述", "is_trap": false, "is_wildcard": false, "hp_change": 0},
    {"text": "选项文字", "correct": false, "result": "结果描述", "is_trap": false, "is_wildcard": false, "hp_change": 0}
  ],
  "hint": "极其隐晦的提示",
  "trap": "陷阱分析（给游戏引擎用，不展示给玩家）"
}`, info.Title, info.Year, strings.Join(info.Genres, "/"),
		truncate(info.Overview, 300), historyStr,
		level, totalLevels, stageName, hp, difficultyGuide,
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
