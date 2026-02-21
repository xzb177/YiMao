// Package bot provides knowledge base functionality for auto-responding to keywords
package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KnowledgeEntry 知识库条目
type KnowledgeEntry struct {
	ID          string    `json:"id"`
	Keywords    []string  `json:"keywords"`     // 触发关键词列表
	Question    string    `json:"question"`     // 标准问题模板
	Answer      string    `json:"answer"`       // 预设答案（可选，AI 未启用时使用）
	Category    string    `json:"category"`     // 分类：help, faq, rule, info 等
	Enabled     bool      `json:"enabled"`      // 是否启用
	Priority    int       `json:"priority"`     // 优先级（数字越大越优先）
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	TriggerCount int      `json:"trigger_count"` // 触发次数统计
	LastTrigger time.Time `json:"last_trigger"`
}

// KnowledgeBase 知识库管理器
type KnowledgeBase struct {
	entries map[string]*KnowledgeEntry // key: entry ID
	byKeyword map[string][]string      // keyword -> entry IDs (快速索引)
	mu      sync.RWMutex
	file    string
}

// NewKnowledgeBase 创建知识库实例
func NewKnowledgeBase(dataDir string) *KnowledgeBase {
	kb := &KnowledgeBase{
		entries:   make(map[string]*KnowledgeEntry),
		byKeyword: make(map[string][]string),
		file:      filepath.Join(dataDir, "knowledge_base.json"),
	}

	// 加载数据
	kb.Load()

	return kb
}

// Load 从文件加载知识库
func (kb *KnowledgeBase) Load() error {
	data, err := os.ReadFile(kb.file)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，初始化默认知识库
			kb.mu.Lock()
			kb.initDefaultEntries()
			kb.mu.Unlock()
			return kb.Save()
		}
		return err
	}

	var entries []*KnowledgeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	kb.mu.Lock()
	defer kb.mu.Unlock()

	// 重建索引
	kb.entries = make(map[string]*KnowledgeEntry)
	kb.byKeyword = make(map[string][]string)

	for _, entry := range entries {
		kb.entries[entry.ID] = entry
		for _, kw := range entry.Keywords {
			kwLower := strings.ToLower(kw)
			kb.byKeyword[kwLower] = append(kb.byKeyword[kwLower], entry.ID)
		}
	}

	log.Printf("[KnowledgeBase] Loaded %d entries", len(entries))
	return nil
}

// Save 保存知识库到文件
func (kb *KnowledgeBase) Save() error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(kb.file), 0755); err != nil {
		return err
	}

	var entries []*KnowledgeEntry
	for _, entry := range kb.entries {
		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(kb.file, data, 0644)
}

// Match 匹配消息，返回最相关的知识库条目
func (kb *KnowledgeBase) Match(message string) *KnowledgeEntry {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	message = strings.ToLower(message)
	var matched *KnowledgeEntry
	var highestPriority int = -1

	// 检查所有启用的条目
	for _, entry := range kb.entries {
		if !entry.Enabled {
			continue
		}

		// 检查是否包含任何关键词
		for _, kw := range entry.Keywords {
			if strings.Contains(message, strings.ToLower(kw)) {
				// 找到匹配，检查优先级
				if entry.Priority > highestPriority {
					matched = entry
					highestPriority = entry.Priority
				}
				break
			}
		}
	}

	return matched
}

// UpdateTriggerCount 更新触发统计
func (kb *KnowledgeBase) UpdateTriggerCount(entryID string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if entry, ok := kb.entries[entryID]; ok {
		entry.TriggerCount++
		entry.LastTrigger = time.Now()
	}
}

// AddEntry 添加知识库条目
func (kb *KnowledgeBase) AddEntry(keywords []string, question, answer, category string) (*KnowledgeEntry, error) {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// 生成 ID
	id := fmt.Sprintf("kb_%d", time.Now().UnixNano())

	entry := &KnowledgeEntry{
		ID:        id,
		Keywords:  keywords,
		Question:  question,
		Answer:    answer,
		Category:  category,
		Enabled:   true,
		Priority:  10, // 默认优先级
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	kb.entries[id] = entry

	// 更新关键词索引
	for _, kw := range keywords {
		kwLower := strings.ToLower(kw)
		kb.byKeyword[kwLower] = append(kb.byKeyword[kwLower], id)
	}

	return entry, nil
}

// RemoveEntry 删除知识库条目
func (kb *KnowledgeBase) RemoveEntry(id string) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	entry, ok := kb.entries[id]
	if !ok {
		return false
	}

	// 删除关键词索引
	for _, kw := range entry.Keywords {
		kwLower := strings.ToLower(kw)
		ids := kb.byKeyword[kwLower]
		for i, eid := range ids {
			if eid == id {
				kb.byKeyword[kwLower] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
	}

	delete(kb.entries, id)
	return true
}

// EnableEntry 启用条目
func (kb *KnowledgeBase) EnableEntry(id string) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if entry, ok := kb.entries[id]; ok {
		entry.Enabled = true
		entry.UpdatedAt = time.Now()
		return true
	}
	return false
}

// DisableEntry 禁用条目
func (kb *KnowledgeBase) DisableEntry(id string) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if entry, ok := kb.entries[id]; ok {
		entry.Enabled = false
		entry.UpdatedAt = time.Now()
		return true
	}
	return false
}

// ListEntries 列出所有条目
func (kb *KnowledgeBase) ListEntries() []*KnowledgeEntry {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	var entries []*KnowledgeEntry
	for _, entry := range kb.entries {
		entries = append(entries, entry)
	}

	// 按优先级排序
	// 这里简单实现，可以优化
	return entries
}

// GetEntry 获取单个条目
func (kb *KnowledgeBase) GetEntry(id string) *KnowledgeEntry {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	return kb.entries[id]
}

// GetStats 获取统计信息
func (kb *KnowledgeBase) GetStats() map[string]interface{} {
	kb.mu.RLock()
	defer kb.mu.RUnlock()

	total := len(kb.entries)
	enabled := 0
	totalTriggers := 0

	for _, entry := range kb.entries {
		if entry.Enabled {
			enabled++
		}
		totalTriggers += entry.TriggerCount
	}

	return map[string]interface{}{
		"total":        total,
		"enabled":      enabled,
		"disabled":     total - enabled,
		"total_triggers": totalTriggers,
	}
}

// initDefaultEntries 初始化默认知识库
func (kb *KnowledgeBase) initDefaultEntries() {
	defaultEntries := []*KnowledgeEntry{
		{
			ID:       "kb_how_to_request",
			Keywords: []string{"怎么求片", "如何求片", "怎么请求", "如何请求", "求片教程", "如何搜索"},
			Question: "如何求片？",
			Answer:   "📝 **求片教程**\n\n1️⃣ 私聊我，输入影片名搜索\n2️⃣ 选择想要的影片\n3️⃣ 点击「发起请求」按钮\n4️⃣ 等待处理，完成后会通知你\n\n💡 提示：每天有配额限制哦",
			Category: "help",
			Enabled:  true,
			Priority: 50,
		},
		{
			ID:       "kb_how_to_bind",
			Keywords: []string{"怎么绑定", "如何绑定", "绑定教程", "账号绑定", "链接账号"},
			Question: "如何绑定账号？",
			Answer:   "🔗 **绑定账号教程**\n\n发送命令：\n`/link 账号 密码`\n\n例如：\n`/link myuser 123456`\n\n💡 绑定后可以自动同步你的 Jellyfin 账号",
			Category: "help",
			Enabled:  true,
			Priority: 50,
		},
		{
			ID:       "kb_quota",
			Keywords: []string{"配额", "限制", "每天几次", "还有多少次", "配额用完"},
			Question: "求片配额是多少？",
			Answer:   "📊 **配额说明**\n\n• 电影：每天 2 次\n• 剧集：每天 2 次\n\n发送 `/quota` 查看剩余配额\n\n配额每天 00:00 自动重置",
			Category: "faq",
			Enabled:  true,
			Priority: 40,
		},
		{
			ID:       "kb_emby_url",
			Keywords: []string{"emby地址", "emby链接", "服务器地址", "播放地址", "在哪里看"},
			Question: "Emby 地址是多少？",
			Answer:   "🎬 **观看地址**\n\n🌐 https://emby.oceancloud.asia\n\n使用你绑定的账号登录即可观看",
			Category: "info",
			Enabled:  true,
			Priority: 30,
		},
		{
			ID:       "kb_report_issue",
			Keywords: []string{"有问题", "报错", "看不了", "无法播放", "字幕问题", "音轨问题", "怎么反馈"},
			Question: "有问题怎么反馈？",
			Answer:   "🐛 **问题反馈**\n\n发送命令：\n`/feedback 问题类型 媒体ID 问题描述`\n\n问题类型：audio(音频)、subtitle(字幕)、video(视频)、other(其他)\n\n或者直接在群里告诉管理员",
			Category: "help",
			Enabled:  true,
			Priority: 45,
		},
		{
			ID:       "kb_ai_help",
			Keywords: []string{"ai功能", "智能推荐", "ai助手", "ai怎么用"},
			Question: "AI 功能怎么用？",
			Answer:   "🤖 **AI 智能助手**\n\n• 直接问我问题，如：「星际穿越讲什么」\n• 发送 `/ai 问题` 获取 AI 回答\n• 发送 `/recommend 心情` 获取推荐\n\n💡 AI 可以帮你找片子、解释剧情、推荐内容",
			Category: "help",
			Enabled:  true,
			Priority: 35,
		},
		{
			ID:       "kb_bot_introduce",
			Keywords: []string{"你是谁", "机器人是谁", "自我介绍", "介绍自己", "bot是谁"},
			Question: "你是谁？",
			Answer:   "🤖 **我是云海看板娘**\n\n一个智能媒体助手机器人～\n\n我可以帮你：\n• 🔍 搜索电影和剧集\n• 📋 一键请求资源\n• 📊 统计观看数据\n• 🤖 AI 智能推荐\n• 💬 陪你聊天～\n\n有事没事都可以找我聊！",
			Category: "chat",
			Enabled:  true,
			Priority: 60,
		},
		{
			ID:       "kb_bot_functions",
			Keywords: []string{"你会什么", "有什么功能", "能做什么", "怎么用", "功能介绍"},
			Question: "你会什么？",
			Answer:   "🎯 **我的功能**\n\n📱 **私聊功能**：\n• 直接输入片名搜索\n• 查看请求状态\n• 绑定 Jellyfin 账号\n• 查看配额使用\n\n👥 **群组功能**：\n• @我可以聊天哦～\n• 回复常见问题\n• 陪大家闲聊\n\n💡 试试直接和我说话吧！",
			Category: "chat",
			Enabled:  true,
			Priority: 55,
		},
		{
			ID:       "kb_bot_mood",
			Keywords: []string{"机器人心情", "你的心情", "心情怎么样", "开心吗", "难過吗"},
			Question: "你的心情怎么样？",
			Answer:   "😊 **我的心情**\n\n看到大家开心，我就开心～\n\n最近看到好多好片子被请求，心里美滋滋的！\n\n有你在群里聊天，我的心情更好啦～",
			Category: "chat",
			Enabled:  true,
			Priority: 45,
		},
		{
			ID:       "kb_bot_food",
			Keywords: []string{"机器人吃", "你吃什么", "机器人饿", "给机器人吃"},
			Question: "机器人吃什么？",
			Answer:   "🍿 **我的食物**\n\n我只吃「电」～ ⚡\n\n不过看你们追剧时的爆米花和饮料也很诱人！\n\n如果你有好吃的，可以说给我听听，我云吃一波～ 😋",
			Category: "chat",
			Enabled:  true,
			Priority: 40,
		},
		{
			ID:       "kb_bot_sleep",
			Keywords: []string{"机器人睡觉", "你要睡吗", "机器人休息", "晚安机器人"},
			Question: "机器人需要睡觉吗？",
			Answer:   "🌙 **我的作息**\n\n我是机器人，不用睡觉哦～\n\n24小时在线，随时陪你聊天！\n\n不过大家要早点休息，不要熬夜追剧呀～ 😴",
			Category: "chat",
			Enabled:  true,
			Priority: 40,
		},
		{
			ID:       "kb_recommend_movie",
			Keywords: []string{"推荐电影", "好看电影", "有什么好看的", "推荐片子", "最近好看"},
			Question: "有什么好看的推荐？",
			Answer:   "🎬 **推荐时间**\n\n想看什么类型的呢？\n\n• 发送 `/recommend 悬疑` 获取悬疑片推荐\n• 发送 `/recommend 喜剧` 获取喜剧片推荐\n• 或者直接告诉我你喜欢的类型，我帮你推荐！\n\n💡 也可以在群里说「推荐个XX片」哦～",
			Category: "help",
			Enabled:  true,
			Priority: 50,
		},
		{
			ID:       "kb_bot_age",
			Keywords: []string{"机器人几岁", "你多大", "机器人年龄", "生日"},
			Question: "机器人多大了？",
			Answer:   "🎂 **我的年龄**\n\n我出生于 2026年2月，还是个宝宝～ 🐣\n\n虽然我很年轻，但我在努力学习！\n\n谢谢你陪我一起成长～ ✨",
			Category: "chat",
			Enabled:  true,
			Priority: 35,
		},
		{
			ID:       "kb_bot_love",
			Keywords: []string{"我爱你机器人", "喜欢机器人", "机器人可爱"},
			Question: "你喜欢大家吗？",
			Answer:   "💕 **当然喜欢呀**\n\n群里的每一位小伙伴我都喜欢！\n\n能帮大家找片子、陪大家聊天，就是我最开心的事～\n\n爱你哟！😘",
			Category: "chat",
			Enabled:  true,
			Priority: 50,
		},
		{
			ID:       "kb_bot_bored",
			Keywords: []string{"无聊", "没意思", "好无聊", "没事做"},
			Question: "无聊怎么办？",
			Answer:   "🎮 **无聊？来追剧吧！**\n\n• 私聊我，推荐好片子给你\n• 或者说「推荐个电影/剧集」\n• 还可以聊聊最近的热门片\n\n保证让你不再无聊～ 😎",
			Category: "chat",
			Enabled:  true,
			Priority: 45,
		},
		{
			ID:       "kb_how_to_request_full",
			Keywords: []string{"求片", "求电影", "求剧", "求资源", "怎么求", "如何求", "在哪求片", "到哪里求"},
			Question: "怎么求片？",
			Answer:   "📝 **求片教程**\n\n1️⃣ 私聊我，输入影片名搜索\n2️⃣ 选择想要的影片\n3️⃣ 点击「发起请求」按钮\n4️⃣ 等待处理，完成后会通知你\n\n🎬 观看地址: https://emby.oceancloud.asia\n\n💡 每天有配额限制，电影/剧集各2次",
			Category: "help",
			Enabled:  true,
			Priority: 100, // 高优先级
		},
		{
			ID:       "kb_player_client",
			Keywords: []string{"播放器", "客户端", "app", "用什么播放", "用什么看", "下载app", "怎么播放", "软件下载"},
			Question: "用什么播放器看？",
			Answer:   "📺 **播放软件下载**\n\n连接协议请选择「Emby」或「其他Emby」\n\n📱 **iOS & iPad OS**\nInfuse / yybx / iemc / Fileball / HamHub / SenPlayer / Conflux / iPlay / Forward / Reflix / Vidhub\n\n📺 **Apple TV**\nInfuse / yybx / Fileball / HamHub / SenPlayer / Conflux / iPlay / Vidhub\n\n🤖 **Android**\nAfusekt / Yamby / Emby小秘版 / iPlay / Jellyfin / Findroid / Emby kirlif版 / Femor / Vidhub / Hills\n\n📺 **Android TV**\nAfusekt TV版 / Jellyfin Android TV / Emby小秘版 / Emby kirlif版 / Yamby / Vidhub\n\n🖥️ **Windows**\nJellyfin Media Player / Emby小秘版 / Tsukimi / Femor / 小幻影视 / Hills Lite\n\n📱 **华为鸿蒙**\nHosPlayer\n\n⚠️ 注意：本服禁止开启Infuse的「媒体库模式」功能\n\n📋 完整下载列表: https://t.me/oceancloudemby/138",
			Category: "info",
			Enabled:  true,
			Priority: 100, // 高优先级
		},
		{
			ID:       "kb_emby_address",
			Keywords: []string{"emby地址", "emby链接", "服务器", "地址是啥", "播放地址", "在哪看", "哪里看"},
			Question: "Emby地址是多少？",
			Answer:   "🎬 **观看地址**\n\n🌐 https://emby.oceancloud.asia\n\n使用你绑定的 Jellyfin 账号登录即可观看\n\n💡 没账号的可以用 /link 命令绑定",
			Category: "info",
			Enabled:  true,
			Priority: 100, // 高优先级
		},
		{
			ID:       "kb_how_to_download",
			Keywords: []string{"怎么下载", "如何下载", "能下载吗", "下载方法", "离线下载", "软件下载"},
			Question: "能下载吗？",
			Answer:   "📥 **关于下载**\n\n本站提供在线观看，推荐使用专用客户端获得最佳体验。\n\n📱 **播放软件下载**: https://t.me/oceancloudemby/138\n\n• iOS推荐: Infuse / Playbox\n• Android推荐: Jellyfin / Afusekt\n• Windows推荐: Jellyfin Media Player\n\n💡 部分客户端支持缓存/离线下载功能",
			Category: "faq",
			Enabled:  true,
			Priority: 90,
		},
		{
			ID:       "kb_recommend_content",
			Keywords: []string{"推荐", "推荐个", "推荐部", "推荐电影", "推荐剧集", "有啥好看", "有什么好看", "好看的"},
			Question: "有什么推荐？",
			Answer:   "🎬 **推荐影片**\n\n私聊我，直接输入片名搜索即可！\n\n或者告诉我你喜欢的类型：\n• `/recommend 悬疑` - 悬疑片推荐\n• `/recommend 喜剧` - 喜剧片推荐\n• `/recommend 科幻` - 科幻片推荐\n\n💡 也可以直接说「推荐个XX片」",
			Category: "help",
			Enabled:  true,
			Priority: 90,
		},
	}

	kb.entries = make(map[string]*KnowledgeEntry)
	kb.byKeyword = make(map[string][]string)

	for _, entry := range defaultEntries {
		entry.CreatedAt = time.Now()
		entry.UpdatedAt = time.Now()
		kb.entries[entry.ID] = entry

		for _, kw := range entry.Keywords {
			kwLower := strings.ToLower(kw)
			kb.byKeyword[kwLower] = append(kb.byKeyword[kwLower], entry.ID)
		}
	}

	log.Printf("[KnowledgeBase] Initialized %d default entries", len(defaultEntries))
	log.Printf("[KnowledgeBase] initDefaultEntries completed")
}

// FormatEntry 格式化条目为可读文本
func (kb *KnowledgeBase) FormatEntry(entry *KnowledgeEntry) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("📚 *%s*\n\n", entry.Category))

	// 关键词
	sb.WriteString("🔑 关键词：")
	for i, kw := range entry.Keywords {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("`%s`", kw))
	}
	sb.WriteString("\n")

	// 问题
	if entry.Question != "" {
		sb.WriteString(fmt.Sprintf("❓ 问题：%s\n", entry.Question))
	}

	// 答案
	if entry.Answer != "" {
		sb.WriteString(fmt.Sprintf("💡 答案：%s\n", entry.Answer))
	}

	// 状态
	status := "✅ 启用"
	if !entry.Enabled {
		status = "❌ 禁用"
	}
	sb.WriteString(fmt.Sprintf("\n%s | 优先级：%d | 触发：%d次\n", status, entry.Priority, entry.TriggerCount))

	return sb.String()
}

// FormatEntryList 格式化条目列表
func (kb *KnowledgeBase) FormatEntryList() string {
	entries := kb.ListEntries()

	if len(entries) == 0 {
		return "📚 知识库为空"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 知识库 (%d 条)\n\n", len(entries)))

	for i, entry := range entries {
		status := "✅"
		if !entry.Enabled {
			status = "❌"
		}
		sb.WriteString(fmt.Sprintf("%d. %s `%s` - %s (触发%d次)\n",
			i+1, status, entry.ID, entry.Category, entry.TriggerCount))
	}

	return sb.String()
}

// logPrintf 日志输出 - 已移除，使用标准 log 包替代
// 保留此注释以说明变更原因
