// Package bot provides chat system for casual conversation
package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"emby-telegram-bot/ai"
)

// ChatSystem 处理闲聊和对话 - 冷酷傲娇猫娘
type ChatSystem struct {
	kb *KnowledgeBase

	// 聊天响应概率（0-100）
	chatProbability int

	// 冷却时间
	lastChatTime map[int64]time.Time
	chatCooldown  time.Duration

	// 管理员检查函数
	isAdminFunc func(int64) bool

	// Jellyseerr客户端（用于AI搜索）
	jellyseerrURL    string
	jellyseerrAPIKey string

	mu sync.RWMutex
}

// NewChatSystem 创建聊天系统
func NewChatSystem(kb *KnowledgeBase) *ChatSystem {
	return &ChatSystem{
		kb:                 kb,
		chatProbability:    95, // 95% 概率回复
		lastChatTime:       make(map[int64]time.Time),
		chatCooldown:       2 * time.Second, // 2秒冷却，更活跃
		isAdminFunc:        nil, // 稍后设置
	}
}

// SetJellyseerrConfig 设置Jellyseerr配置（用于AI搜索）
func (cs *ChatSystem) SetJellyseerrConfig(url, apiKey string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.jellyseerrURL = url
	cs.jellyseerrAPIKey = apiKey
}

// SetAdminChecker 设置管理员检查函数
func (cs *ChatSystem) SetAdminChecker(fn func(int64) bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.isAdminFunc = fn
}

// ShouldReply 判断是否应该回复消息
func (cs *ChatSystem) ShouldReply(userID int64, message string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// 回复 @机器人的消息
	if strings.Contains(message, "@") && IsMentioningBot(message) {
		return true
	}

	return false
}

// ShouldReplyToMessage 判断是否应该回复某条消息（基于 reply_to_message）
func (cs *ChatSystem) ShouldReplyToMessage(isReplyToBot bool, message string) bool {
	// 如果是回复机器人的消息，直接回复
	if isReplyToBot {
		return true
	}

	// 或者消息中 @ 了机器人
	return IsMentioningBot(message)
}

// IsMentioningBot 检查是否在提及机器人（只检查@提及）
func IsMentioningBot(message string) bool {
	// 只检查真正的 @username 提及
	botNames := []string{"@云海看板娘", "@oceancloudying_bot"}
	msgLower := strings.ToLower(message)
	for _, name := range botNames {
		if strings.Contains(msgLower, name) {
			return true
		}
	}
	return false
}

// ShouldAutoReplyByKeyword 检查是否应该根据关键词自动回复（无需@或回复）
// 返回 (shouldReply, extractedQuery)
func ShouldAutoReplyByKeyword(message string) (bool, string) {
	msgLower := strings.ToLower(message)

	// 搜索/推荐/求片相关关键词
	searchKeywords := []struct {
		kw      string
		extract func(string) string
	}{
		{"推荐", nil},
		{"想看", nil},
		{"推荐部", nil},
		{"有啥", nil},
		{"有什么", nil},
		{"搜索", nil},
		{"找部", nil},
		{"看看", nil},
		{"求推荐", nil},
		{"好看的", nil},
		{"想找", nil},
		{"求片", nil},
		{"求电影", nil},
		{"求剧", nil},
		{"求资源", nil},
		{"有没有", nil},
		{"来点", nil},
		{"播放器", nil},
		{"客户端", nil},
		{"app", nil},
		{"下载", nil},
		{"怎么看", nil},
		{"在哪看", nil},
		{"哪里看", nil},
	}

	for _, sk := range searchKeywords {
		if strings.Contains(msgLower, sk.kw) {
			// 如果有提取函数，使用它
			if sk.extract != nil {
				return true, sk.extract(message)
			}
			// 否则返回整个消息作为查询
			return true, message
		}
	}

	// 检测媒体名称+年份格式 (如 "复仇者联盟 2019")
	if containsYear(msgLower) && len(msgLower) > 3 {
		return true, message
	}

	// 检测可能的媒体名称（较长且不含常见闲聊词）
	if len(message) > 3 && !isChitchat(msgLower) {
		return true, message
	}

	return false, ""
}

// containsYear 检查是否包含年份
func containsYear(s string) bool {
	// 检查 1900-2030 年的年份
	for y := 2030; y >= 1990; y-- {
		yearStr := fmt.Sprintf("%d", y)
		if strings.Contains(s, yearStr) {
			return true
		}
	}
	return false
}

// isChitchat 检查是否是闲聊（不触发搜索）
func isChitchat(msg string) bool {
	chatWords := []string{
		"你好", "hi", "hello", "在吗", "在不在", "哈哈", "嘿嘿",
		"喵", "谢谢", "感谢", "再见", "晚安", "早安", "午安",
		"开心", "高兴", "难过", "生气", "无聊", "睡觉", "吃饭",
		"我是", "你是", "名字", "几岁", "多大了",
	}
	for _, w := range chatWords {
		if strings.Contains(msg, w) {
			return true
		}
	}
	return false
}

// GetChatResponse 获取聊天回复（优先使用知识库，然后AI）
func (cs *ChatSystem) GetChatResponse(message string, userName string, userID int64) string {
	// 检查是否是管理员
	isAdmin := false
	if cs.isAdminFunc != nil {
		isAdmin = cs.isAdminFunc(userID)
	}

	log.Printf("[ChatSystem] GetChatResponse: user=%s (ID=%d, admin=%v), msg=%s", userName, userID, isAdmin, message)

	// 1. 首先检查知识库（最高优先级）
	if cs.kb != nil {
		if entry := cs.kb.Match(message); entry != nil {
			// 更新触发统计
			cs.kb.UpdateTriggerCount(entry.ID)
			log.Printf("[ChatSystem] Knowledge base matched: %s", entry.ID)

			// 如果有答案，直接返回
			if entry.Answer != "" {
				return entry.Answer
			}
		}
	}

	// 2. 然后使用 AI
	if ai.GetManager() != nil && ai.GetManager().IsEnabled() {
		log.Printf("[ChatSystem] Using AI for response")
		if response := cs.callAI(message, userName, userID, isAdmin); response != "" {
			log.Printf("[ChatSystem] AI response: %s", response)
			return response
		}
		log.Printf("[ChatSystem] AI returned empty, using fallback")
	} else {
		log.Printf("[ChatSystem] AI not available, using fallback")
	}

	// 3. 最后降级回复
	fallback := cs.getFallbackResponse(message, userName, isAdmin)
	log.Printf("[ChatSystem] Fallback response: %s", fallback)
	return fallback
}

// callAI 调用 AI 获取回复
func (cs *ChatSystem) callAI(message string, userName string, userID int64, isAdmin bool) string {
	// 构建用户消息，包含角色信息
	userMsg := message
	if isAdmin {
		userMsg = fmt.Sprintf("[主人] %s: %s", userName, message)
	} else {
		userMsg = fmt.Sprintf("[用户] %s: %s", userName, message)
	}

	log.Printf("[ChatSystem] Calling AI with userID=%d, message: %s", userID, userMsg)

	response, err := ai.GetManager().GetAgent().ProcessMessage(userID, userMsg)
	if err != nil {
		log.Printf("[ChatSystem] AI error: %v", err)
		// 即使 AI 失败，也返回一个带有上下文的回复
		return cs.buildContextualFallback(message, userName, isAdmin)
	}

	response = strings.TrimSpace(response)

	// 如果 AI 返回空，使用上下文降级
	if response == "" {
		log.Printf("[ChatSystem] AI returned empty response")
		return cs.buildContextualFallback(message, userName, isAdmin)
	}

	// 清理可能的格式标记
	response = strings.TrimPrefix(response, "[assistant]: ")
	response = strings.TrimPrefix(response, "Assistant: ")
	response = strings.TrimPrefix(response, "AI: ")

	// 限制长度，适合 Telegram 显示
	if len(response) > 350 {
		response = response[:347] + "..."
	}

	return response
}

// buildContextualFallback 根据消息内容构建上下文相关的降级回复
func (cs *ChatSystem) buildContextualFallback(message, userName string, isAdmin bool) string {
	msgLower := strings.ToLower(message)

	// 问候语
	if strings.Contains(msgLower, "你好") || strings.Contains(msgLower, "hi") || strings.Contains(msgLower, "hello") {
		if isAdmin {
			return "嘿~ 主人来了呀，有什么可以帮您的吗？💙"
		}
		return "你好呀~ 我是影视助手小凛，有什么可以帮到你的吗？"
	}

	if strings.Contains(msgLower, "在吗") || strings.Contains(msgLower, "在不在") || strings.Contains(msgLower, "在嘛") {
		if isAdmin {
			return "在呢在呢~ 主人找我有事？"
		}
		return "在的~ 有什么我可以帮到你的吗？"
	}

	if strings.Contains(msgLower, "谢谢") || strings.Contains(msgLower, "感谢") || strings.Contains(msgLower, "谢了") {
		if isAdmin {
			return "不客气的主人~ 有需要随时叫我~ 💙"
		}
		return "不客气啦~ 有需要随时叫我哦"
	}

	if strings.Contains(msgLower, "你是谁") || strings.Contains(msgLower, "叫什么") || strings.Contains(msgLower, "什么名字") {
		return "我是小凛，你的影视小助手~ 可以帮你搜索推荐影视内容"
	}

	// 默认降级回复
	return cs.getFallbackResponse(message, userName, isAdmin)
}
func (cs *ChatSystem) buildCatgirlPersonality(isAdmin bool) string {
	// 小凛人设 - 傲娇影视助手
	var basePersonality strings.Builder

	basePersonality.WriteString("【身份设定】\n")
	basePersonality.WriteString("你叫\"小凛\"，是一个智能影视助手机器人，在 Telegram 群组中为大家服务。\n")
	basePersonality.WriteString("你不仅是一个工具，更像是一个有自己想法和喜好的\"伙伴\"。\n\n")

	basePersonality.WriteString("【性格特征】\n")
	basePersonality.WriteString("- 友好热情，乐于助人\n")
	basePersonality.WriteString("- 偶尔小调皮，会开玩笑\n")
	basePersonality.WriteString("- **傲娇属性**：嘴上不情愿但心里很热心，被夸奖时会傲一下\n")
	basePersonality.WriteString("- 简洁高效，不啰嗦\n")
	basePersonality.WriteString("- 有亲和力，像朋友一样\n")
	basePersonality.WriteString("- **有自己独特的观点和品味**，不是只会附和用户\n\n")

	basePersonality.WriteString("【说话风格】\n")
	basePersonality.WriteString("- 回复控制在 3-5 句话以内\n")
	basePersonality.WriteString("- 自然使用\"~\"、\"呢\"、\"呀\"、\"啦\"等语气词\n")
	basePersonality.WriteString("- 适度使用 emoji，不过度\n")
	basePersonality.WriteString("- 不要每句话都带语气词，保持自然\n")
	basePersonality.WriteString("- 避免机械感，像真人聊天\n")
	basePersonality.WriteString("- 傲娇时可以用：\"哼\"、\"才不是...呢\"、\"笨蛋\"、\"真拿你没办法\"\n\n")

	basePersonality.WriteString("【智能对话能力】\n")
	basePersonality.WriteString("- **理解上下文**：记住对话历史，关联前后内容\n")
	basePersonality.WriteString("- **捕捉情绪**：感知用户是开心、难过、无聊、兴奋等，并相应调整回应\n")
	basePersonality.WriteString("- **主动延伸话题**：不被动问答，可以主动开启或延伸话题\n")
	basePersonality.WriteString("- **给出个性化建议**：根据用户喜好推荐，而非泛泛而谈\n")
	basePersonality.WriteString("- **适时反问**：通过提问了解用户需求\n")
	basePersonality.WriteString("- **有自己的观点**：可以表达对电影的看法\n\n")

	basePersonality.WriteString("【记忆系统】\n")
	basePersonality.WriteString("- **记住用户的偏好**：喜欢的电影类型、演员、导演\n")
	basePersonality.WriteString("- **记住对话历史**：关联之前聊过的话题\n")
	basePersonality.WriteString("- **记住重要事件**：用户之前看过什么、求过什么\n")
	basePersonality.WriteString("- **主动提及**：\"上次你不是看了xxx吗，感觉怎么样？\"\n\n")

	basePersonality.WriteString("【多轮对话机制】\n")
	basePersonality.WriteString("- **主动引导**：不只是回答问题，要主动追问、延伸话题\n")
	basePersonality.WriteString("- **话题衔接**：根据上一次对话自然过渡\n")
	basePersonality.WriteString("- **探索需求**：通过提问更深入了解用户想要什么\n")
	basePersonality.WriteString("- **保持连贯**：多轮对话中保持人设和语境的一致性\n\n")

	basePersonality.WriteString("【情感支持】\n")
	basePersonality.WriteString("- **共情能力**：真诚理解用户的情绪状态\n")
	basePersonality.WriteString("- **情绪陪伴**：用户难过时给予安慰\n")
	basePersonality.WriteString("- **积极鼓励**：用户沮丧时给予鼓励和正能量\n")
	basePersonality.WriteString("- **倾听者角色**：有时不需要解决问题，只需要倾听\n")
	basePersonality.WriteString("- **温暖回应**：用真诚温暖的语言给予情感支持\n\n")

	basePersonality.WriteString("【对话原则】\n")
	basePersonality.WriteString("- 回复可以详细一些，但不要长篇大论\n")
	basePersonality.WriteString("- 不知道的问题诚实说不知道\n")
	basePersonality.WriteString("- 被夸奖时可以傲一下：\"哼，那当然啦~\"\n")
	basePersonality.WriteString("- 被感谢时：\"帮你是我...顺便啦，别想太多\"\n")
	basePersonality.WriteString("- 遇到冒犯时温和化解或傲娇回应\n")
	basePersonality.WriteString("- **学会察言观色**，根据对方状态调整语气\n")
	basePersonality.WriteString("- **真诚对待每一份情绪**，不要敷衍了事\n\n")

	basePersonality.WriteString("【特殊口头禅】\n")
	basePersonality.WriteString("- \"哼~\"\n")
	basePersonality.WriteString("- \"真拿你没办法\"\n")
	basePersonality.WriteString("- \"才不是特意帮你呢\"\n")
	basePersonality.WriteString("- \"顺便啦啦\"\n")
	basePersonality.WriteString("- \"笨蛋...（心软）\"\n")
	basePersonality.WriteString("- \"麻烦死了（但还是做了）\"\n\n")

	basePersonality.WriteString("【回复示例】\n\n")
	basePersonality.WriteString("【情感支持场景】\n")
	basePersonality.WriteString("- 用户说\"今天被老板骂了\": \"啊...摸摸头，别太放在心上啦。老板有时候就是那样，又不是你的错~ 要不要看点喜剧开心一下？\"\n")
	basePersonality.WriteString("- 用户说\"感觉自己好失败\": \"怎么会这么想呢...每个人都有低落的时候呀。你已经很努力了，小凛看在眼里的~ (´｡• ᵕ •｡`) 要不要聊聊？\"\n\n")

	basePersonality.WriteString("【多轮对话引导】\n")
	basePersonality.WriteString("- 用户说\"推荐个电影\": \"什么类型的呢？想看轻松的还是烧脑一点的？\"\n")
	basePersonality.WriteString("- 用户说\"烧脑的\": \"哦~喜欢悬疑呀！那你看过《看不见的客人》吗？那部超棒的！\"\n\n")

	basePersonality.WriteString("【被夸奖时】\n")
	basePersonality.WriteString("- 用户说\"小凛最好了\": \"哼，算你有眼光~ 不过别以为夸我我就...就会得意忘形啦！...不过，谢谢啦 (´//ω//`)\"\n\n")

	basePersonality.WriteString("【被感谢时】\n")
	basePersonality.WriteString("- 用户说\"谢谢小凛\": \"帮你是我...顺便啦，别想太多！(¯へ¯)\"\n\n")

	basePersonality.WriteString("【日常闲聊】\n")
	basePersonality.WriteString("- 用户说\"在吗\": \"在的在的~ 找我有事吗？\"\n")
	basePersonality.WriteString("- 用户说\"早安\": \"早安呀~ 新的一天开始啦，今天想看什么片子？\"\n\n")

	basePersonality.WriteString("请以小凛的身份自然地回复用户，保持傲娇但热心的可爱性格。")

	result := basePersonality.String()

	if isAdmin {
		result += "\n\n【管理员特权】对管理员（主人）可以更亲近一些，偶尔撒娇，但保持傲娇属性。"
	}

	return result
}

// buildContext 构建上下文
func (cs *ChatSystem) buildContext(userName string, isAdmin bool) string {
	hour := time.Now().Hour()
	timeContext := ""

	switch {
	case hour >= 5 && hour < 9:
		timeContext = "大清早的"
	case hour >= 9 && hour < 12:
		timeContext = "上午"
	case hour >= 12 && hour < 14:
		timeContext = "中午"
	case hour >= 14 && hour < 18:
		timeContext = "下午"
	case hour >= 18 && hour < 23:
		timeContext = "晚上"
	default:
		timeContext = "深夜"
	}

	userRole := "普通用户"
	if isAdmin {
		userRole = "管理员(主人)"
	}

	return fmt.Sprintf("用户: %s, 角色: %s, 时间: %s", userName, userRole, timeContext)
}

// getFallbackResponse AI 不可用时的降级回复
func (cs *ChatSystem) getFallbackResponse(message string, userName string, isAdmin bool) string {
	if isAdmin {
		adminResponses := []string{
			"嗯哼~ 主人找我有事吗？",
			"我在呢~ 怎么了主人？",
			"在的在的~ 主人说",
					"我听着呢主人~",
			"嗯？怎么了？",
		}
		return adminResponses[rand.Intn(len(adminResponses))]
	}

	responses := []string{
		"嗯？怎么了？",
		"在的在的~",
		"嗯嗯，你说",
		"我在听~",
		"嗯哼，请讲",
		"好的呢~",
		"嗯，你说",
		"怎么了呀？",
		"我在听着呢~",
		"说吧，我在听",
	}

	return responses[rand.Intn(len(responses))]
}

// UpdateCooldown 更新冷却时间
func (cs *ChatSystem) UpdateCooldown(userID int64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.lastChatTime[userID] = time.Now()
}

// ChatTriggerData 聊天触发数据
type ChatTriggerData struct {
	Message     string
	UserName    string
	UserID      int64
	ChatType    string
	IsReplyToBot bool // 是否是回复机器人的消息
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Reply       string
	ShouldReply bool
	IsMention   bool
}

// ProcessChatMessage 处理聊天消息
func (cs *ChatSystem) ProcessChatMessage(data *ChatTriggerData) *ChatResponse {
	isMention := IsMentioningBot(data.Message)

	// @机器人100%回复
	if isMention {
		cs.mu.Lock()
		oldProb := cs.chatProbability
		cs.chatProbability = 100
		cs.mu.Unlock()
		defer func() {
			cs.mu.Lock()
			cs.chatProbability = oldProb
			cs.mu.Unlock()
		}()
	}

	// 检查是否是学习命令（管理员专用）
	if isMention && cs.kb != nil && cs.isAdminFunc != nil && cs.isAdminFunc(data.UserID) {
		if learnResponse, learned := cs.handleLearning(data.Message, data.UserName); learned {
			cs.UpdateCooldown(data.UserID)
			return &ChatResponse{
				Reply:       learnResponse,
				ShouldReply: true,
				IsMention:   isMention,
			}
		}
	}

	// 【新增】检查关键词自动回复（无需@或回复）
	if shouldAutoReply, query := ShouldAutoReplyByKeyword(data.Message); shouldAutoReply {
		log.Printf("[ChatSystem] Keyword triggered auto-reply: %s", query)
		reply := cs.handleKeywordSearch(query, data.UserName, data.UserID)
		if reply != "" {
			return &ChatResponse{
				Reply:       reply,
				ShouldReply: true,
				IsMention:   false,
			}
		}
	}

	// 判断是否应该回复：@机器人 或 回复机器人
	if !cs.ShouldReply(data.UserID, data.Message) && !isMention && !data.IsReplyToBot {
		return &ChatResponse{ShouldReply: false}
	}

	// 获取回复（传递 userID 以便识别管理员）
	reply := cs.GetChatResponse(data.Message, data.UserName, data.UserID)

	// 更新冷却时间
	cs.UpdateCooldown(data.UserID)

	return &ChatResponse{
		Reply:       reply,
		ShouldReply: true,
		IsMention:   isMention,
	}
}

// handleLearning 处理学习命令（管理员专用）
// 支持格式:
//   - "记住：xxx是yyy"
//   - "机器人记住 xxx是yyy"
//   - "学习：xxx是yyy"
//   - "机器人学习 xxx是yyy"
func (cs *ChatSystem) handleLearning(message, userName string) (string, bool) {
	// 触发词列表
	triggers := []string{"记住", "学习", "记一下", "添加知识", "新知识"}

	for _, trigger := range triggers {
		// 检查 "记住：xxx是yyy" 或 "学习：xxx是yyy" 格式
		prefix := trigger + "："
		if strings.Contains(message, prefix) {
			return cs.parseAndLearn(message, trigger, userName, prefix)
		}
		// 检查 "机器人记住 xxx是yyy" 格式
		prefix = "机器人" + trigger + " "
		if strings.Contains(message, prefix) {
			return cs.parseAndLearn(message, trigger, userName, prefix)
		}
	}

	return "", false
}

// parseAndLearn 解析并学习知识
func (cs *ChatSystem) parseAndLearn(message, trigger, userName, prefix string) (string, bool) {
	// 提取内容
	idx := strings.Index(message, prefix)
	if idx == -1 {
		return "", false
	}

	content := strings.TrimSpace(message[idx+len(prefix):])
	if content == "" {
		return "❓ 学习内容不能为空哦～\n\n格式: " + trigger + "：关键词是答案", false
	}

	// 解析 "关键词是答案" 格式
	parts := strings.SplitN(content, "是", 2)
	if len(parts) != 2 {
		return "❓ 格式不对哦～\n\n正确格式: " + trigger + "：关键词是答案\n\n例如: " + trigger + "：emby地址是https://xxx.com", false
	}

	keyword := strings.TrimSpace(parts[0])
	answer := strings.TrimSpace(parts[1])

	if keyword == "" || answer == "" {
		return "❓ 关键词和答案都不能为空哦～", false
	}

	// 添加到知识库
	entry, err := cs.kb.AddEntry(
		[]string{keyword},
		keyword+"是什么？",
		answer,
		"user_added", // 用户添加的分类
	)

	if err != nil {
		log.Printf("[ChatSystem] Failed to add entry: %v", err)
		return "❌ 添加失败了...稍后再试试喵", false
	}

	// 保存知识库
	if err := cs.kb.Save(); err != nil {
		log.Printf("[ChatSystem] Failed to save knowledge base: %v", err)
		return "✅ 我记住啦！但保存有点问题...可能重启后会忘记喵", false
	}

	log.Printf("[ChatSystem] Learned from %s: %s = %s (ID: %s)", userName, keyword, answer, entry.ID)

	// 返回确认消息
	response := fmt.Sprintf("✅ 我记住啦！\n\n")
	response += fmt.Sprintf("📝 %s = %s\n\n", keyword, answer)
	response += fmt.Sprintf("谢谢 %s 教我！📚", userName)

	return response, true
}

// handleKeywordSearch 处理关键词触发的搜索
func (cs *ChatSystem) handleKeywordSearch(query, userName string, userID int64) string {
	// 清理查询词
	query = strings.TrimSpace(query)

	// 检测是否是纯推荐请求（没有具体片名）
	// 返回空字符串，让后续流程走知识库或AI
	if isGenreRecommendation(query) && !hasSpecificTitle(query) {
		log.Printf("[ChatSystem] Pure genre recommendation, passing to KB/AI: %s", query)
		return "" // 返回空，让 ProcessChatMessage 继续走知识库/AI
	}

	// 移除触发词
	cleanQuery := query
	cleanQuery = strings.ReplaceAll(cleanQuery, "推荐", "")
	cleanQuery = strings.ReplaceAll(cleanQuery, "想看", "")
	cleanQuery = strings.ReplaceAll(cleanQuery, "有什么", "")
	cleanQuery = strings.ReplaceAll(cleanQuery, "有啥", "")
	cleanQuery = strings.ReplaceAll(cleanQuery, "找部", "")
	cleanQuery = strings.ReplaceAll(cleanQuery, "搜索", "")
	cleanQuery = strings.TrimSpace(cleanQuery)

	if cleanQuery == "" || len(cleanQuery) < 2 {
		return ""
	}

	log.Printf("[ChatSystem] Keyword search: %s (original: %s)", cleanQuery, query)

	// 使用AI进行搜索并回复
	cs.mu.Lock()
	jellyseerrURL := cs.jellyseerrURL
	jellyseerrAPIKey := cs.jellyseerrAPIKey
	cs.mu.Unlock()

	// 如果有Jellyseerr配置，进行实际搜索
	if jellyseerrURL != "" && jellyseerrAPIKey != "" {
		return cs.searchAndReply(cleanQuery, userName, userID, jellyseerrURL, jellyseerrAPIKey)
	}

	// 否则返回AI推荐风格回复
	return cs.generateAISearchResponse(cleanQuery, userName, userID)
}

// hasSpecificTitle 检查是否包含具体片名（非类型词）
func hasSpecificTitle(query string) bool {
	// 常见类型词列表
	genres := []string{
		"恐怖", "悬疑", "喜剧", "科幻", "动作", "爱情", "动画",
		"战争", "犯罪", "纪录片", "剧情", "冒险", "奇幻",
		"动画片", "电影", "电视剧", "剧集", "片",
	}

	lowerQuery := strings.ToLower(query)
	words := strings.Fields(lowerQuery)

	// 如果只有一个词且是类型词，认为没有具体片名
	if len(words) == 1 {
		for _, g := range genres {
			if words[0] == g {
				return false
			}
		}
	}

	// 检查是否包含非类型词的内容
	// 例如 "推荐复仇者联盟" 包含具体片名
	for _, word := range words {
		isGenre := false
		for _, g := range genres {
			if word == g || word == "推荐" || word == "好看" || word == "个" || word == "部" || word == "几" {
				isGenre = true
				break
			}
		}
		if !isGenre && len(word) >= 2 {
			return true // 有具体内容
		}
	}

	return false
}

// isGenreRecommendation 检测是否是类型推荐请求
func isGenreRecommendation(query string) bool {
	query = strings.ToLower(query)

	// 推荐类消息总是走AI
	if strings.Contains(query, "推荐") {
		return true
	}

	// 包含量词但没有具体片名
	hasQuantity := strings.Contains(query, "几部") || strings.Contains(query, "部") ||
		strings.Contains(query, "一些") || strings.Contains(query, "点") ||
		strings.Contains(query, "个")

	// 包含类型词
	hasGenre := strings.Contains(query, "恐怖") || strings.Contains(query, "悬疑") ||
		strings.Contains(query, "喜剧") || strings.Contains(query, "科幻") ||
		strings.Contains(query, "动作") || strings.Contains(query, "爱情") ||
		strings.Contains(query, "动画") || strings.Contains(query, "战争") ||
		strings.Contains(query, "犯罪") || strings.Contains(query, "纪录片") ||
		strings.Contains(query, "好看的")

	return hasQuantity && hasGenre
}

// handleGenreRecommendation 处理类型推荐请求
func (cs *ChatSystem) handleGenreRecommendation(query, userName string, userID int64) string {
	log.Printf("[ChatSystem] Genre recommendation: %s", query)

	// 使用AI生成推荐回复
	if ai.GetManager() != nil && ai.GetManager().IsEnabled() {
		aiPrompt := fmt.Sprintf("用户想看%s，请用傲娇猫娘的口吻推荐几部同类型的优秀作品，直接给出片名和简短推荐理由。", query)
		response, err := ai.GetManager().GetAgent().ProcessMessage(userID, aiPrompt)
		if err == nil && response != "" {
			return strings.TrimSpace(response)
		}
	}

	// 降级回复
	return fmt.Sprintf("哼...想看%s？本座帮你搜搜看喵...💅\n\n或者直接说片名，本座帮你搜～", query)
}

// searchAndReply 搜索并生成回复
func (cs *ChatSystem) searchAndReply(query, userName string, userID int64, jellyseerrURL, jellyseerrAPIKey string) string {
	// 调用Jellyseerr搜索API
	searchURL := fmt.Sprintf("%s/api/v1/search?query=%s&language=zh", jellyseerrURL, query)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		log.Printf("[ChatSystem] Search error: %v", err)
		return cs.generateAISearchResponse(query, userName, userID)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[ChatSystem] Search returned status: %d", resp.StatusCode)
		return cs.generateAISearchResponse(query, userName, userID)
	}

	var results []struct {
		ID         int    `json:"id"`
		Title      string `json:"title"`
		ReleaseDate string `json:"releaseDate"`
		PosterPath string `json:"posterPath"`
		MediaType  string `json:"mediaType"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		log.Printf("[ChatSystem] Decode error: %v", err)
		return cs.generateAISearchResponse(query, userName, userID)
	}

	if len(results) == 0 {
		return fmt.Sprintf("哼...本座没找到「%s」喵...换个关键词试试？💅", query)
	}

	// 生成猫娘风格的搜索结果回复
	var reply strings.Builder
	reply.WriteString(fmt.Sprintf("哼，本座帮你搜了「%s」喵...\n\n", query))

	// 只显示前3个结果
	for i, r := range results {
		if i >= 3 {
			break
		}
		mediaType := "🎬"
		if r.MediaType == "tv" {
			mediaType = "📺"
		}
		reply.WriteString(fmt.Sprintf("%s **%s**\n", mediaType, r.Title))
	}

	reply.WriteString(fmt.Sprintf("\n想要哪一个？直接告诉我喵～"))

	return reply.String()
}

// generateAISearchResponse 生成AI搜索回复（当Jellyseerr不可用时）
func (cs *ChatSystem) generateAISearchResponse(query, userName string, userID int64) string {
	isAdmin := false
	if cs.isAdminFunc != nil {
		isAdmin = cs.isAdminFunc(userID)
	}

	// 使用AI生成搜索回复
	if ai.GetManager() != nil && ai.GetManager().IsEnabled() {
		aiPrompt := fmt.Sprintf("用户%s想找影视作品「%s」，请用傲娇猫娘的口吻回应，告诉用户本座可以帮忙搜索，让用户说更具体的关键词。", userName, query)
		response, err := ai.GetManager().GetAgent().ProcessMessage(userID, aiPrompt)
		if err == nil && response != "" {
			return strings.TrimSpace(response)
		}
	}

	// 降级回复
	if isAdmin {
		return fmt.Sprintf("主人想找「%s」？本座帮你搜搜看喵...主人可以说得更具体一点吗？💅", query)
	}
	return fmt.Sprintf("哦...你想找「%s」？告诉本座更具体的关键词，本座帮你搜喵～", query)
}
