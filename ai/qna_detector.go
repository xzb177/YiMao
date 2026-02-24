package ai

import (
	"regexp"
	"strings"
	"unicode"
)

// QNADetector handles detection of questions and answers in chat messages
type QNADetector struct {
	embyKeywords []string
	adminIDs     map[int64]bool
}

// Emby-related keywords for filtering
var defaultEmbyKeywords = []string{
	"emby", "jellyfin", "plex", "jellyseerr",
	"播放", "求片", "绑定", "账号", "搜索", "下载",
	"电影", "电视剧", "剧集", "番剧", "动画", "动漫",
	"字幕", "音轨", "画质", "4k", "1080p",
	"转码", "硬解", "软解", "直连",
	"字幕组", "资源", "磁力", "种子",
	"moviepilot", "mp",
}

// Question patterns with weights
var questionPatterns = []struct {
	pattern string
	weight  float64
}{
	{"吗[？?]?$", 1.0},           // ...吗？
	{"怎么[办说弄做]", 0.9},       // 怎么办/怎么说/怎么弄/怎么做
	{"如何", 0.9},                 // 如何...
	{"[什啥]么", 0.8},            // 什么/啥么
	{"哪里", 0.8},                 // 哪里...
	{"[几多]时", 0.7},            // 几时/多时
	{"[?？]+$", 0.6},             // Ends with ?
	{"在[吗嘛]", 0.5},            // 在吗
	{"有没有", 0.5},               // 有没有
	{"是不是", 0.5},               // 是不是
	{"可以.*吗", 0.5},            // 可以...吗
	{"需要.*吗", 0.5},            // 需要...吗
}

// Stop words for keyword extraction
var stopWords = map[string]bool{
	"的": true, "了": true, "在": true, "是": true,
	"我": true, "有": true, "和": true, "就": true,
	"不": true, "人": true, "都": true, "一": true,
	"一个": true, "上": true, "也": true, "很": true,
	"到": true, "说": true, "要": true, "去": true,
	"你": true, "会": true, "着": true, "没有": true,
	"吗": true, "呢": true, "吧": true, "啊": true,
	"呀": true, "哦": true, "嗯": true, "哈": true,
	"想": true, "能": true, "看": true, "来": true,
	"还是": true, "或者": true, "如果": true, "虽然": true,
}

// NewQNADetector creates a new Q&A detector
func NewQNADetector(adminIDs []int64) *QNADetector {
	adminMap := make(map[int64]bool)
	for _, id := range adminIDs {
		adminMap[id] = true
	}

	return &QNADetector{
		embyKeywords: defaultEmbyKeywords,
		adminIDs:     adminMap,
	}
}

// IsEmbyQuestion checks if a message is an Emby-related question
// Returns (isQuestion, confidence score)
func (d *QNADetector) IsEmbyQuestion(text string) (bool, float64) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 500 {
		return false, 0
	}

	// Check if it looks like a question
	questionScore := 0.0
	for _, p := range questionPatterns {
		if matched, _ := regexp.MatchString(p.pattern, text); matched {
			questionScore = p.weight
			break
		}
	}

	if questionScore < 0.3 {
		return false, 0
	}

	// Check if it's Emby-related
	textLower := strings.ToLower(text)
	embyScore := 0.0
	for _, keyword := range d.embyKeywords {
		if strings.Contains(textLower, keyword) {
			embyScore += 0.25
		}
	}

	// Combined confidence
	confidence := (questionScore * 0.6) + (embyScore * 0.4)

	return confidence >= 0.4, confidence
}

// NormalizeQuestion normalizes a question for matching
func (d *QNADetector) NormalizeQuestion(text string) string {
	// Remove common punctuation
	text = strings.Map(func(r rune) rune {
		if r == '？' || r == '?' || r == '!' || r == '！' {
			return -1
		}
		return r
	}, text)

	// Remove common prefixes
	prefixes := []string{"请问", "想问", "有人知道", "谁知道", "那", "那个", "求助"}
	for _, prefix := range prefixes {
		text = strings.TrimPrefix(text, prefix)
	}

	// Normalize whitespace
	text = strings.Join(strings.Fields(text), " ")
	text = strings.ToLower(strings.TrimSpace(text))

	return text
}

// IsPotentialAnswer checks if a message could be an answer to a question
func (d *QNADetector) IsPotentialAnswer(text string) bool {
	text = strings.TrimSpace(text)

	// Too short or too long
	if len(text) < 5 || len(text) > 2000 {
		return false
	}

	// Check for answer-like patterns
	answerIndicators := []string{
		"可以", "需要", "方法是", "步骤", "操作",
		"在 ", "://", "http", "https",
		"设置", "配置", "选项", "菜单",
		"点击", "选择", "输入", "打开",
		"直接", "然后", "之后", "首先",
		"建议", "推荐", "试试",
	}

	textLower := strings.ToLower(text)
	for _, indicator := range answerIndicators {
		if strings.Contains(textLower, indicator) {
			return true
		}
	}

	// Contains a URL
	if strings.Contains(text, "http") {
		return true
	}

	// Contains numbers/steps (1. 2. 3. or 第一步 第二步)
	if regexp.MustCompile(`^\d+\.\s`).MatchString(text) {
		return true
	}
	if regexp.MustCompile(`第[一二三四五六七八九十]步`).MatchString(text) {
		return true
	}

	return false
}

// ExtractKeywords extracts keywords from a question for indexing
func (d *QNADetector) ExtractKeywords(question string) []string {
	question = d.NormalizeQuestion(question)
	runes := []rune(question)
	keywords := make(map[string]bool)

	// Extract 2-character bigrams (common for Chinese)
	for i := 0; i < len(runes)-1; i++ {
		bigram := string(runes[i : i+2])
		if !stopWords[bigram] && len(bigram) == 2 {
			// Check if it contains meaningful characters
			hasMeaningful := false
			for _, r := range bigram {
				if unicode.Is(unicode.Han, r) {
					hasMeaningful = true
					break
				}
			}
			if hasMeaningful {
				keywords[bigram] = true
			}
		}
	}

	// Extract single meaningful words
	words := strings.Fields(question)
	for _, word := range words {
		word = strings.Trim(word, "。，、；：？！\"'（）【】")
		if len(word) >= 2 && !stopWords[word] {
			keywords[word] = true
		}
	}

	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}

	return result
}

// IsAdmin checks if a user ID is an admin
func (d *QNADetector) IsAdmin(userID int64) bool {
	return d.adminIDs[userID]
}

// SetAdmins updates the admin IDs
func (d *QNADetector) SetAdmins(adminIDs []int64) {
	d.adminIDs = make(map[int64]bool)
	for _, id := range adminIDs {
		d.adminIDs[id] = true
	}
}
