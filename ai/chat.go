// Package ai provides intelligent recommendation chat interface
package ai

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ============================================
// 智能推荐对话接口
// ============================================
// 允许用户用自然语言与推荐系统交互
// 例如："我心情不好推荐点喜剧"、"最近有什么好看的悬疑片"
// ============================================

// RecommendationChat 推荐对话接口
type RecommendationChat struct {
	engine         *RecommendationEngine
	learning       *LearningSystem
	conversations  map[int64]*ChatSession
	sessionMutex   sync.RWMutex
	enabled        bool
}

// ChatSession 对话会话
type ChatSession struct {
	UserID       int64
	Messages     []*ChatMessage
	StartTime    time.Time
	LastActive   time.Time
	Context      *ChatContext
	State        ChatState
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role      string    `json:"role"`       // user/assistant
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Metadata  *MessageMetadata `json:"metadata,omitempty"`
}

// MessageMetadata 消息元数据
type MessageMetadata struct {
	Intent      string                 `json:"intent"`
	Entities    map[string]string      `json:"entities"`
	Confidence  float64                `json:"confidence"`
		Recommendations []*RecommendationResultV2 `json:"recommendations,omitempty"`
}

// ChatContext 对话上下文
type ChatContext struct {
	CurrentMood      string                 `json:"current_mood"`
	PreviousQueries  []string               `json:"previous_queries"`
	Preferences      map[string]interface{} `json:"preferences"`
	FeedbackHistory  []string               `json:"feedback_history"`
}

// ChatState 对话状态
type ChatState string

const (
	StateIdle         ChatState = "idle"          // 空闲
	StateUnderstanding ChatState = "understanding" // 理解需求
	StateRecommending ChatState = "recommending"  // 推荐中
	StateRefining     ChatState = "refining"      // 精细化
	StateFeedback     ChatState = "feedback"      // 收集反馈
)

// NewRecommendationChat 创建推荐对话接口
func NewRecommendationChat(engine *RecommendationEngine, learning *LearningSystem) *RecommendationChat {
	return &RecommendationChat{
		engine:        engine,
		learning:      learning,
		conversations: make(map[int64]*ChatSession),
		enabled:       engine != nil,
	}
}

// Chat 处理用户聊天消息
func (c *RecommendationChat) Chat(userID int64, message string) (string, []*RecommendationResultV2, error) {
	if !c.enabled {
		return "抱歉，智能推荐功能暂时不可用", nil, nil
	}

	// 获取或创建会话
	session := c.getOrCreateSession(userID)
	session.LastActive = time.Now()

	// 添加用户消息
	session.Messages = append(session.Messages, &ChatMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	})

	// 分析意图
	intent, entities := c.parseIntent(message)
	log.Printf("[RecChat] User %d: intent=%s, entities=%v", userID, intent, entities)

	// 根据意图处理
	var response string
	var recommendations []*RecommendationResultV2
	var err error

	switch intent {
	case "recommend", "recommend_mood", "recommend_genre", "recommend_similar":
		response, recommendations, err = c.handleRecommendIntent(session, intent, entities)

	case "search", "info":
		response, err = c.handleInfoIntent(session, entities)

	case "feedback":
		response, err = c.handleFeedbackIntent(session, entities)

	case "greeting", "smalltalk":
		response, err = c.handleCasualIntent(session)

	case "help":
		response = c.getHelpMessage()

	default:
		response, recommendations, err = c.handleRecommendIntent(session, "recommend", entities)
	}

	// 添加助手消息
	session.Messages = append(session.Messages, &ChatMessage{
		Role:      "assistant",
		Content:   response,
		Timestamp: time.Now(),
		Metadata: &MessageMetadata{
			Intent:          intent,
			Entities:        entities,
			Recommendations: recommendations,
		},
	})

	// 限制历史长度
	if len(session.Messages) > 20 {
		session.Messages = session.Messages[len(session.Messages)-20:]
	}

	return response, recommendations, err
}

// parseIntent 解析用户意图
func (c *RecommendationChat) parseIntent(message string) (string, map[string]string) {
	msg := strings.ToLower(message)
	entities := make(map[string]string)

	// 检测意图
	intentPatterns := map[string]*regexp.Regexp{
		"recommend_mood":   regexp.MustCompile(`(心情|难过|开心|无聊|压力大|放松|失眠|沮丧)`),
		"recommend_genre":  regexp.MustCompile(`(推荐|想看|要看).*?(喜剧|悬疑|恐怖|动作|科幻|爱情|动画|战争|犯罪|纪录片)`),
		"recommend_similar": regexp.MustCompile(`(类似|相似|像).*?(推荐|看看)`),
		"search":           regexp.MustCompile(`^(搜索|找|查|看看有没有)`),
		"info":             regexp.MustCompile(`(介绍|讲什么|剧情|怎么样)`),
		"feedback":         regexp.MustCompile(`(不喜欢|爱看|好看|难看|推荐得好)`),
		"greeting":         regexp.MustCompile(`^(你好|嗨|嗨嗨|在吗|嘿)`),
		"help":             regexp.MustCompile(`^(帮助|help|怎么用|怎么玩)`),
	}

	// 优先级检测
	if match := intentPatterns["recommend_genre"]; match.MatchString(msg) {
		intent := "recommend_genre"
		// 提取类型
		for _, genre := range []string{"喜剧", "悬疑", "恐怖", "动作", "科幻", "爱情", "动画", "战争", "犯罪", "纪录片"} {
			if strings.Contains(msg, genre) {
				entities["genre"] = genre
				break
			}
		}
		return intent, entities
	}

	if match := intentPatterns["recommend_mood"]; match.MatchString(msg) {
		// 提取心情关键词
		moods := map[string]string{
			"难过":   "难过", "不开心": "难过", "郁闷": "难过",
			"开心":   "开心", "高兴": "开心", "兴奋": "开心",
			"无聊":   "无聊", "没劲": "无聊",
			"压力大": "紧张", "压力": "紧张",
			"放松":   "放松", "轻松": "放松",
			"失眠":   "放松", "睡不着": "放松",
			"沮丧":   "治愈", "失落": "治愈",
		}
		for keyword, mood := range moods {
			if strings.Contains(msg, keyword) {
				entities["mood"] = mood
				break
			}
		}
		return "recommend_mood", entities
	}

	if match := intentPatterns["recommend_similar"]; match.MatchString(msg) {
		return "recommend_similar", entities
	}

	if match := intentPatterns["search"]; match.MatchString(msg) {
		return "search", entities
	}

	if match := intentPatterns["info"]; match.MatchString(msg) {
		return "info", entities
	}

	if match := intentPatterns["feedback"]; match.MatchString(msg) {
		return "feedback", entities
	}

	if match := intentPatterns["greeting"]; match.MatchString(msg) {
		return "greeting", entities
	}

	if match := intentPatterns["help"]; match.MatchString(msg) {
		return "help", entities
	}

	// 默认当作推荐请求
	return "recommend", entities
}

// handleRecommendIntent 处理推荐意图
func (c *RecommendationChat) handleRecommendIntent(session *ChatSession, intent string, entities map[string]string) (string, []*RecommendationResultV2, error) {
	count := 5
	req := &RecommendationRequestV2{
		UserID:      session.UserID,
		Count:       count,
		MediaType:   "both",
		ExcludeSeen: true,
		MinRating:   6.5,
	}

	// 根据意图和实体设置请求参数
	switch intent {
	case "recommend_mood":
		if mood, ok := entities["mood"]; ok {
			req.Strategy = StrategyMood
			req.Context = mood
			// 记录心情到上下文
			if session.Context == nil {
				session.Context = &ChatContext{}
			}
			session.Context.CurrentMood = mood
			c.engine.RecordInteraction(session.UserID, "mood", map[string]interface{}{"mood": mood})
		}

	case "recommend_genre":
		if genre, ok := entities["genre"]; ok {
			req.Context = fmt.Sprintf("想看%s", genre)
		}

	case "recommend_similar":
		req.Strategy = StrategyPersonalized
		req.Context = "相似推荐"

	default:
		// 综合考虑上下文
		if session.Context != nil && session.Context.CurrentMood != "" {
			req.Strategy = StrategyMood
			req.Context = session.Context.CurrentMood
		} else {
			req.Strategy = StrategyHybrid
		}
	}

	// 获取推荐
	results, err := c.engine.Recommend(req)
	if err != nil {
		return fmt.Sprintf("抱歉，推荐系统出错了：%s", err.Error()), nil, err
	}

	// 格式化响应
	var response strings.Builder
	response.WriteString(fmt.Sprintf("为你找到 %d 部作品：\n\n", len(results)))

	for i, r := range results {
		emoji := "🎬"
		if r.MediaType == "tv" {
			emoji = "📺"
		}

		response.WriteString(fmt.Sprintf("%d. %s **%s** (%d)\n", i+1, emoji, r.Title, r.Year))
		if r.Rating > 0 {
			response.WriteString(fmt.Sprintf("   ⭐ %.1f", r.Rating))
		}
		response.WriteString("\n")

		// 使用 AI 生成的推荐理由
		if r.Reason != "" {
			response.WriteString(fmt.Sprintf("   💡 %s\n", r.Reason))
		}

		response.WriteString("\n")
	}

	response.WriteString("\n回复序号查看详情，或说\"换一批\"获取更多推荐")

	return response.String(), results, nil
}

// handleInfoIntent 处理信息查询意图
func (c *RecommendationChat) handleInfoIntent(session *ChatSession, entities map[string]string) (string, error) {
	return "请告诉我你想了解哪部电影或剧集？我可以帮你介绍剧情、评分等信息。", nil
}

// handleFeedbackIntent 处理反馈意图
func (c *RecommendationChat) handleFeedbackIntent(session *ChatSession, entities map[string]string) (string, error) {
	return "感谢你的反馈！这会帮助我更好地了解你的喜好~", nil
}

// handleCasualIntent 处理闲聊意图
func (c *RecommendationChat) handleCasualIntent(session *ChatSession) (string, error) {
	greetings := []string{
		"嗨~ 想看点什么？我可以帮你推荐！",
		"在的在的~ 需要推荐吗？",
		"你好呀~ 今天想看什么类型的片子？",
		"嗨！想找电影还是剧集？",
	}

	// 根据时间段选择问候语
	hour := time.Now().Hour()
	var timeGreeting string
	switch {
	case hour >= 5 && hour < 12:
		timeGreeting = "早安~"
	case hour >= 12 && hour < 18:
		timeGreeting = "下午好~"
	case hour >= 18 && hour < 23:
		timeGreeting = "晚上好~"
	default:
		timeGreeting = "夜深了~"
	}

	return fmt.Sprintf("%s %s", timeGreeting, greetings[time.Now().Second()%len(greetings)]), nil
}

// getHelpMessage 获取帮助消息
func (c *RecommendationChat) getHelpMessage() string {
	return `🤖 **智能推荐帮助**

你可以这样问我：

📌 **推荐类**
• "推荐几部喜剧电影"
• "我心情不好，推荐点治愈的"
• "有什么好看的悬疑片吗"
• "给我推荐点最近的热门剧集"

📌 **搜索类**
• "搜索星际穿越"
• "找一下泰坦尼克号"

📌 **信息类**
• "肖申克的救赎讲什么"
• "这部剧怎么样"

💡 直接像聊天一样提问就行！`
}

// getOrCreateSession 获取或创建会话
func (c *RecommendationChat) getOrCreateSession(userID int64) *ChatSession {
	c.sessionMutex.Lock()
	defer c.sessionMutex.Unlock()

	session, exists := c.conversations[userID]
	if !exists {
		session = &ChatSession{
			UserID:     userID,
			Messages:   []*ChatMessage{},
			StartTime:  time.Now(),
			LastActive: time.Now(),
			Context:    &ChatContext{
				PreviousQueries: []string{},
				Preferences:     make(map[string]interface{}),
				FeedbackHistory: []string{},
			},
			State: StateIdle,
		}
		c.conversations[userID] = session
	}

	return session
}

// ClearSession 清除会话
func (c *RecommendationChat) ClearSession(userID int64) {
	c.sessionMutex.Lock()
	defer c.sessionMutex.Unlock()
	delete(c.conversations, userID)
}

// GetSessionHistory 获取会话历史
func (c *RecommendationChat) GetSessionHistory(userID int64) []*ChatMessage {
	c.sessionMutex.RLock()
	defer c.sessionMutex.RUnlock()

	if session, exists := c.conversations[userID]; exists {
		return session.Messages
	}
	return nil
}

// ============================================
// 自然语言推荐解析器
// ============================================

// NLRecommendationParser 自然语言推荐解析器
type NLRecommendationParser struct {
	engine  *RecommendationEngine
	enabled bool
}

// NewNLRecommendationParser 创建自然语言推荐解析器
func NewNLRecommendationParser(engine *RecommendationEngine) *NLRecommendationParser {
	return &NLRecommendationParser{
		engine:  engine,
		enabled: engine != nil,
	}
}

// ParseAndRecommend 解析自然语言并推荐
func (p *NLRecommendationParser) ParseAndRecommend(userID int64, input string) (*RecommendationResponse, error) {
	if !p.enabled {
		return nil, fmt.Errorf("parser not enabled")
	}

	// 解析输入
	req := p.parseInput(input)
	req.UserID = userID

	// 获取推荐
	results, err := p.engine.Recommend(req)
	if err != nil {
		return nil, err
	}

	return &RecommendationResponse{
		Query:         input,
		Strategy:      string(req.Strategy),
		Count:         len(results),
		Results:       results,
		ParsedIntent:  string(req.Strategy),
		ResponseTime:  time.Now(),
	}, nil
}

// RecommendationResponse 推荐响应
type RecommendationResponse struct {
	Query         string                   `json:"query"`
	Strategy      string                   `json:"strategy"`
	Count         int                      `json:"count"`
		Results       []*RecommendationResultV2 `json:"results"`
	ParsedIntent  string                   `json:"parsed_intent"`
	ResponseTime  time.Time                `json:"response_time"`
}

// parseInput 解析输入
func (p *NLRecommendationParser) parseInput(input string) *RecommendationRequestV2 {
	req := &RecommendationRequestV2{
		Count:       5,
		Strategy:    StrategyHybrid,
		MediaType:   "both",
		ExcludeSeen: true,
		MinRating:   6.0,
	}

	inputLower := strings.ToLower(input)

	// 解析媒体类型
	if strings.Contains(inputLower, "电影") && !strings.Contains(inputLower, "剧集") && !strings.Contains(inputLower, "电视剧") {
		req.MediaType = "movie"
	} else if strings.Contains(inputLower, "剧集") || strings.Contains(inputLower, "电视剧") || strings.Contains(inputLower, "剧") {
		req.MediaType = "tv"
	}

	// 解析数量
	countMatch := regexp.MustCompile(`(\d+)部|(\d+)个`).FindStringSubmatch(input)
	if len(countMatch) > 1 {
		if count, err := parseInt(countMatch[1]); err == nil && count > 0 && count <= 20 {
			req.Count = count
		}
	}

	// 解析类型
	genres := map[string]string{
		"喜剧": "喜剧", "悬疑": "悬疑", "恐怖": "恐怖",
		"动作": "动作", "科幻": "科幻", "爱情": "爱情",
		"动画": "动画", "战争": "战争", "犯罪": "犯罪",
		"纪录片": "纪录片", "惊悚": "惊悚", "冒险": "冒险",
	}
	for keyword, genre := range genres {
		if strings.Contains(input, keyword) {
			req.Context = genre
			break
		}
	}

	// 解析心情
	moods := map[string]string{
		"心情不好": "难过", "难过": "难过", "不开心": "难过",
		"无聊": "无聊", "放松": "放松", "轻松": "放松",
		"兴奋": "兴奋", "刺激": "紧张", "紧张": "紧张",
		"治愈": "治愈", "温暖": "治愈",
	}
	for keyword, mood := range moods {
		if strings.Contains(input, keyword) {
			req.Context = mood
			req.Strategy = StrategyMood
			break
		}
	}

	// 解析特殊需求
	if strings.Contains(input, "最新") || strings.Contains(input, "新片") || strings.Contains(input, "最近上映") {
		req.Strategy = StrategyTrending
	}

	if strings.Contains(input, "经典") || strings.Contains(input, "老片") {
		// 设置年份偏好
		req.Context = "经典怀旧"
	}

	if strings.Contains(input, "高评分") || strings.Contains(input, "好评") || strings.Contains(input, "经典") {
		req.MinRating = 7.5
	}

	return req
}

func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}
