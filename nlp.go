package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// 意图类型 - 表示用户的意图
type Intent string

const (
	IntentSearch  Intent = "search"  // 搜索、找、我想看
	IntentRequest Intent = "request" // 请求、求片
	IntentStatus  Intent = "status"  // 我的请求、状态
	IntentHelp    Intent = "help"    // 帮助、怎么用
	IntentMovie   Intent = "movie"   // 电影相关
	IntentTV      Intent = "tv"      // 电视剧相关
	IntentAdmin   Intent = "admin"   // 管理员操作
	IntentStats   Intent = "stats"   // 统计查询
	IntentQuota   Intent = "quota"   // 配额、限额
	IntentLink    Intent = "link"    // 链接账号
	IntentVerify  Intent = "verify"  // 验证码
	IntentUnlink  Intent = "unlink"  // 解绑账号
	IntentUsers   Intent = "users"   // 用户列表
	IntentUnknown Intent = "unknown"
)

// 搜索参数 - 解析出的搜索参数
type SearchParams struct {
	Query      string // 搜索关键词
	MediaType  string // "movie"或"tv"或空
	Year       string // 年份
	MinRating  float64 // 最低评分
	Genre      string // 类型
	IsChinese  bool // 是否中文
	Original   string // 原始输入文本
}

// 自然语言解析器 - 处理中文自然语言输入
type NLPParser struct {
	// 预编译的正则表达式，提高性能
	searchPatterns     []*regexp.Regexp
	requestPatterns    []*regexp.Regexp
	statusPatterns     []*regexp.Regexp
	helpPatterns       []*regexp.Regexp
	quotaPatterns      []*regexp.Regexp
	moviePatterns      []*regexp.Regexp
	tvPatterns         []*regexp.Regexp
	adminPatterns      []*regexp.Regexp
	statsPatterns      []*regexp.Regexp
	yearPattern        *regexp.Regexp
	ratingPattern      *regexp.Regexp
}

// 新建自然语言解析器
func NewNLPParser() *NLPParser {
	parser := &NLPParser{
		// 搜索模式
		searchPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(搜索|找|查找|搜一下|我想看|我想找|帮我看|查询|求|search)`),
			regexp.MustCompile(`(搜索|找|查找)\s+.+`),
			regexp.MustCompile(`有没有\s+(.+)`),
			regexp.MustCompile(`有\s+(.+)\s+吗`),
		},
		// 请求模式
		requestPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(请求|求片|求资源|我要看|想看|下载|add|request)`),
			regexp.MustCompile(`帮我\s+(下载|请求|求)\s+(.+)`),
		},
		// 状态查询模式
		statusPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(我的请求|我的状态|状态|status|我\s+的|my\s*request|my\s*status)`),
			regexp.MustCompile(`怎么样了|如何了|进度|进展`),
		},
		// 帮助模式
		helpPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(帮助|help|怎么用|怎么玩|使用说明|指南|commands|菜单|menu)`),
			regexp.MustCompile(`如何\s+.+`),
		},
		// 配额查询模式
		quotaPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(配额|限额|限制|额度|quota|limit|剩余)`),
			regexp.MustCompile(`(配额|限额|quota|limit)\?`),
		},
		// 电影关键词
		moviePatterns: []*regexp.Regexp{
			regexp.MustCompile(`电影|影片|movie|film`),
		},
		// 电视剧关键词
		tvPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(电视剧|剧集|综艺|动漫|动画|番|美剧|韩剧|日剧|tv|show|series|anime)`),
		},
		// 管理员关键词
		adminPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(待处理|pending|批准|approve|拒绝|decline|管理员|admin)`),
		},
		// 统计关键词
		statsPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^(统计|数据|排行|热门|trends|top|activity|stats)`),
		},
		// 年份提取模式
		yearPattern: regexp.MustCompile(`(19|20)\d{2}年?|(19|20)\d{2}`),
		// 评分提取模式
		ratingPattern: regexp.MustCompile(`(\d+\.?\d*)\s*分以上?|评分\s*(\d+\.?\d*)|(\d+)\+分`),
	}

	return parser
}

// 解析 - 分析自然语言输入并提取意图和参数
func (p *NLPParser) Parse(text string) (Intent, *SearchParams, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return IntentUnknown, nil, fmt.Errorf("输入为空")
	}

	// 保留原始文本
	params := &SearchParams{
		Original: text,
	}

	// 检查是否是命令格式（如 /search）
	if strings.HasPrefix(text, "/") {
		return p.parseCommand(text)
	}

	// 检测意图
	intent := p.detectIntent(text)

	// 根据意图提取搜索参数
	switch intent {
	case IntentSearch, IntentRequest:
		p.extractSearchParams(text, params)
	case IntentStatus:
		// 状态查询不需要额外参数
	case IntentMovie, IntentTV:
		// 媒体特定意图
		params.MediaType = string(intent)
		p.extractSearchParams(text, params)
	}

	return intent, params, nil
}

// 解析命令 - 处理斜杠命令
func (p *NLPParser) parseCommand(text string) (Intent, *SearchParams, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return IntentUnknown, nil, fmt.Errorf("空命令")
	}

	command := strings.ToLower(strings.TrimPrefix(parts[0], "/"))

	params := &SearchParams{
		Original: text,
	}

	switch command {
	case "start", "help":
		return IntentHelp, params, nil
	case "search", "s":
		if len(parts) > 1 {
			params.Query = strings.Join(parts[1:], " ")
			p.extractSearchParams(params.Query, params)
		}
		return IntentSearch, params, nil
	case "request", "req":
		if len(parts) > 1 {
			params.Query = strings.Join(parts[1:], " ")
			p.extractSearchParams(params.Query, params)
		}
		return IntentRequest, params, nil
	case "quota", "limit", "配额":
		return IntentQuota, params, nil
	case "link", "绑定":
		if len(parts) > 1 {
			params.Query = strings.Join(parts[1:], " ")
		}
		return IntentLink, params, nil
	case "verify", "验证", "验证码":
		return IntentVerify, params, nil
	case "unlink", "解绑", "解除绑定":
		return IntentUnlink, params, nil
	case "users", "用户":
		return IntentUsers, params, nil
	case "my", "me", "myrequests", "status":
		return IntentStatus, params, nil
	case "stats", "top", "activity", "trends":
		return IntentStats, params, nil
	case "pending", "approve", "decline":
		return IntentAdmin, params, nil
	case "prefs", "preferences":
		return IntentHelp, params, nil
	default:
		return IntentUnknown, nil, fmt.Errorf("未知命令: %s", command)
	}
}

// 检测意图 - 从文本中判断用户意图
func (p *NLPParser) detectIntent(text string) Intent {
	textLower := strings.ToLower(text)

	// 优先检查配额模式（最具体）
	for _, pattern := range p.quotaPatterns {
		if pattern.MatchString(textLower) {
			return IntentQuota
		}
	}

	// 优先检查管理员模式（更具体）
	for _, pattern := range p.adminPatterns {
		if pattern.MatchString(textLower) {
			return IntentAdmin
		}
	}

	// 检查统计模式
	for _, pattern := range p.statsPatterns {
		if pattern.MatchString(textLower) {
			return IntentStats
		}
	}

	// 检查请求模式
	for _, pattern := range p.requestPatterns {
		if pattern.MatchString(textLower) {
			return IntentRequest
		}
	}

	// 检查搜索模式
	for _, pattern := range p.searchPatterns {
		if pattern.MatchString(textLower) {
			return IntentSearch
		}
	}

	// 检查状态模式
	for _, pattern := range p.statusPatterns {
		if pattern.MatchString(textLower) {
			return IntentStatus
		}
	}

	// 检查帮助模式
	for _, pattern := range p.helpPatterns {
		if pattern.MatchString(textLower) {
			return IntentHelp
		}
	}

	// 检查媒体特定关键词
	hasMovieKeyword := false
	hasTVKeyword := false

	for _, pattern := range p.moviePatterns {
		if pattern.MatchString(textLower) {
			hasMovieKeyword = true
			break
		}
	}

	for _, pattern := range p.tvPatterns {
		if pattern.MatchString(textLower) {
			hasTVKeyword = true
			break
		}
	}

	// 如果包含媒体关键词，视为搜索
	if hasMovieKeyword || hasTVKeyword {
		if hasMovieKeyword && !hasTVKeyword {
			return IntentMovie
		}
		if hasTVKeyword && !hasMovieKeyword {
			return IntentTV
		}
		return IntentSearch
	}

	// 检查是否是中文标题（可能是搜索）
	if p.isLikelyChineseTitle(text) {
		return IntentSearch
	}

	// 检查是否是英文标题（可能是搜索）
	if p.isLikelyEnglishTitle(text) {
		return IntentSearch
	}

	// 默认返回帮助
	return IntentHelp
}

// 提取搜索参数 - 从文本中提取搜索参数
func (p *NLPParser) extractSearchParams(text string, params *SearchParams) {
	textLower := strings.ToLower(text)

	// 移除常见前缀以获得干净的查询
	query := text

	// 移除搜索/请求前缀
	prefixes := []string{
		"搜索", "找", "查找", "搜一下", "我想看", "我想找", "帮我看", "查询",
		"请求", "求片", "求资源", "我要看", "想看", "下载",
		"search", "find", "look for", "show me", "i want",
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(textLower, prefix) {
			query = strings.TrimSpace(text[len(prefix):])
			break
		}
	}

	// 提取年份
	if matches := p.yearPattern.FindAllString(query, -1); len(matches) > 0 {
		year := strings.TrimSuffix(matches[0], "年")
		if len(year) == 4 {
			params.Year = year
			// 从查询中移除年份
			query = p.yearPattern.ReplaceAllString(query, "")
		}
	}

	// 提取评分
	if matches := p.ratingPattern.FindStringSubmatch(query); len(matches) > 0 {
		for _, match := range matches[1:] {
			if match != "" {
				if rating, err := strconv.ParseFloat(match, 64); err == nil {
					params.MinRating = rating
					break
				}
			}
		}
		// 从查询中移除评分
		query = p.ratingPattern.ReplaceAllString(query, "")
	}

	// 从剩余文本中检测媒体类型
	queryLower := strings.ToLower(query)
	for _, pattern := range p.moviePatterns {
		if pattern.MatchString(queryLower) {
			params.MediaType = "movie"
			// 从查询中移除类型关键词
			for _, kw := range []string{"电影", "影片", "movie", "film"} {
				query = strings.ReplaceAll(query, kw, "")
				queryLower = strings.ReplaceAll(queryLower, kw, "")
			}
			break
		}
	}

	if params.MediaType == "" {
		for _, pattern := range p.tvPatterns {
			if pattern.MatchString(queryLower) {
				params.MediaType = "tv"
				// 从查询中移除类型关键词
				for _, kw := range []string{"电视剧", "剧集", "综艺", "动漫", "动画", "番", "美剧", "韩剧", "日剧", "tv", "show", "series", "anime"} {
					query = strings.ReplaceAll(query, kw, "")
					queryLower = strings.ReplaceAll(queryLower, kw, "")
				}
				break
			}
		}
	}

	// 清理查询
	query = strings.TrimSpace(query)
	query = regexp.MustCompile(`\s+`).ReplaceAllString(query, " ")

	params.Query = query

	// 检测查询是否是中文
	params.IsChinese = p.isChinese(query)
}

// 检测中文 - 检查文本是否包含中文字符
func (p *NLPParser) isChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// 检测是否为中文标题 - 检查文本是否可能是中文媒体标题
func (p *NLPParser) isLikelyChineseTitle(text string) bool {
	// 移除常见词汇
	wordsToSkip := []string{
		"的", "了", "是", "在", "有", "和", "与", "或", "但", "而", "等",
		"怎么", "如何", "什么", "哪些", "怎样", "帮忙", "帮", "请", "你好",
		"hello", "hi", "hey", "please", "help", "how",
	}

	textLower := strings.ToLower(text)
	for _, word := range wordsToSkip {
		if strings.Contains(textLower, word) {
			return false
		}
	}

	// 检查中文字符数量
	chineseCount := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			chineseCount++
		}
	}

	// 至少30%是中文且长度在2-30字符之间
	ratio := float64(chineseCount) / float64(len([]rune(text)))
	return chineseCount >= 2 && ratio > 0.3 && len([]rune(text)) <= 30
}

// 检测是否为英文标题 - 检查文本是否可能是英文媒体标题
func (p *NLPParser) isLikelyEnglishTitle(text string) bool {
	// 检查主要是ASCII字母
	letterCount := 0
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letterCount++
		}
	}

	totalLen := len([]rune(text))
	ratio := float64(letterCount) / float64(totalLen)

	// 至少50%是字母且长度在2-30字符之间
	return letterCount >= 2 && ratio > 0.5 && totalLen <= 30
}

// 全局自然语言解析器实例
var nlpParser *NLPParser

// 初始化自然语言处理
func InitNLP() {
	nlpParser = NewNLPParser()
	log.Println("自然语言解析器初始化完成")
}

// 解析自然语言 - 使用全局解析器解析文本
func ParseNLP(text string) (Intent, *SearchParams, error) {
	if nlpParser == nil {
		return IntentUnknown, nil, fmt.Errorf("自然语言解析器未初始化")
	}
	return nlpParser.Parse(text)
}

// 格式化意图 - 返回可读的意图名称
func FormatIntent(intent Intent) string {
	switch intent {
	case IntentSearch:
		return "搜索"
	case IntentRequest:
		return "请求"
	case IntentStatus:
		return "状态"
	case IntentHelp:
		return "帮助"
	case IntentMovie:
		return "电影"
	case IntentTV:
		return "剧集"
	case IntentAdmin:
		return "管理"
	case IntentStats:
		return "统计"
	default:
		return "未知"
	}
}

// 建议操作 - 根据意图返回建议的操作
func SuggestActions(intent Intent) []string {
	switch intent {
	case IntentSearch:
		return []string{"点击下方搜索结果", "输入关键词搜索", "使用筛选条件"}
	case IntentRequest:
		return []string{"点击搜索结果", "选择要请求的内容", "等待管理员批准"}
	case IntentStatus:
		return []string{"查看请求进度", "联系管理员"}
	case IntentHelp:
		return []string{"查看命令列表", "发送 /start 开始使用"}
	case IntentMovie:
		return []string{"输入电影名称", "如: 搜索复仇者联盟"}
	case IntentTV:
		return []string{"输入剧集名称", "如: 搜索权力的游戏"}
	default:
		return []string{"发送 /help 查看帮助"}
	}
}
