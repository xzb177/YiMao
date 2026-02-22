# AI 推荐系统 2.0 - 设计文档

## 概述

全新的 AI 推荐系统采用了现代化的推荐算法，结合了：
- **多维度用户画像** - 行为、偏好、上下文、交互历史
- **混合推荐策略** - 个性化、热门、心情、发现、混合
- **实时学习** - 从用户反馈中持续优化
- **上下文感知** - 考虑时间、情绪、社交场景
- **可解释性** - AI 能解释为什么推荐这个

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                    用户交互层                                 │
├─────────────────────────────────────────────────────────────┤
│  • 自然语言对话 (chat.go)                                     │
│  • 反馈学习系统 (learning.go)                               │
│  • 智能解析器 (NLRecommendationParser)                       │
├─────────────────────────────────────────────────────────────┤
│                    推荐引擎层                                 │
├─────────────────────────────────────────────────────────────┤
│  • RecommendationEngine (recommendation_v2.go)               │
│    - 多策略路由                                              │
│    - 用户画像管理                                            │
│    - AI 推荐生成                                            │
│    - 匹配度计算                                              │
├─────────────────────────────────────────────────────────────┤
│                    AI 模型层                                   │
├─────────────────────────────────────────────────────────────┤
│  • ZhipuClient (zhipu.go) - 智谱 AI 接口                   │
│  • TrendingAIManager (trending.go) - 热门推荐              │
│  • MemorySystem (memory.go) - 记忆系统                      │
└─────────────────────────────────────────────────────────────┘
```

## 使用示例

### 1. 基本推荐

```go
import "emby-telegram-bot/ai"

// 初始化引擎
zhipu := ai.NewZhipuClient(os.Getenv("ZHIPU_API_KEY"))
trendingMgr := ai.NewTrendingAIManager(zhipu)
engine := ai.NewRecommendationEngine(zhipu, trendingMgr)

// 创建推荐请求
req := &ai.RecommendationRequestV2{
    UserID:      123456,
    Count:       5,
    Strategy:    ai.StrategyHybrid,
    MediaType:   "both",
    MinRating:   6.5,
}

// 获取推荐
results, err := engine.Recommend(req)
if err != nil {
    log.Printf("Recommendation error: %v", err)
    return
}

// 格式化输出
for _, r := range results {
    fmt.Printf("🎬 %s (%d) ⭐%.1f\n", r.Title, r.Year, r.Rating)
    fmt.Printf("   💡 %s\n", r.Reason)
    fmt.Printf("   🎯 %v\n\n", r.Tags)
}
```

### 2. 记录用户行为（学习）

```go
// 记录搜索
engine.RecordInteraction(userID, "search", map[string]interface{}{
    "query": "复仇者联盟",
})

// 记录求片请求
engine.RecordInteraction(userID, "request", map[string]interface{}{
    "media_type": "movie",
    "genre":       "动作",
    "year":        2012,
})

// 记录心情
engine.RecordInteraction(userID, "mood", map[string]interface{}{
    "mood": "放松",
})

// 记录反馈
learning.RecordFeedback(&ai.Feedback{
    UserID:    123456,
    ItemID:    "12345",
    ItemTitle: "复仇者联盟",
    Reaction:  "like",
    Strategy:  "personalized",
})
```

### 3. 自然语言对话

```go
chat := ai.NewRecommendationChat(engine, learning)

// 用户可以自然语言提问
response, results, err := chat.Chat(123456, "我心情不好，推荐点喜剧")
response, results, err = chat.Chat(123456, "最近有什么好看的悬疑片吗")
response, results, err = chat.Chat(123456, "推荐几部类似盗梦空间的电影")
```

### 4. 获取推荐解释

```go
reasons, err := learning.ExplainRecommendation(userID, result)
// 返回: ["你很喜欢诺兰的作品", "烧脑悬疑片符合你口味", "评分很高值得看"]

// 获取个性化推荐理由
shortReason := learning.GetPersonalizedReason(userID, "盗梦空间", "movie")
// 返回: "因为你喜欢《星际穿越》，推荐这部诺兰的经典作品"
```

## 推荐策略

### StrategyPersonalized（个性化推荐）
- 基于用户历史行为
- 考虑用户偏好（类型、年份、演员）
- 使用 AI 生成个性化推荐理由

### StrategyTrending（热门推荐）
- 当前热门的电影和剧集
- 新上映作品
- 高评分内容

### StrategyMood（心情推荐）
- 根据用户当前心情推荐
- 心情→类型映射：
  - 开心 → 喜剧、动画
  - 难过 → 喜剧、温情、治愈
  - 紧张 → 悬疑、惊悚、动作
  - 放松 → 喜剧、爱情、动画
  - 思考 → 科幻、悬疑、剧情
  - 刺激 → 恐怖、惊悚、动作

### StrategyDiscovery（发现推荐）
- 探索用户未接触过的内容
- 不同国家、不同时代、不同类型
- 平衡熟悉与新鲜

### StrategyHybrid（混合推荐）
- 40% 个性化
- 30% 热门
- 20% 心情
- 10% 发现

## 用户画像数据结构

```go
type UserProfileV2 struct {
    UserID          int64
    CreatedAt       time.Time
    UpdatedAt       time.Time

    // 行为维度
    Behavior        *UserBehavior      // 搜索、请求历史
    Preferences     *UserPreferencesV2 // 喜好、厌恶
    Context         *UserContext       // 心情、时间模式
    Interaction     *InteractionHistory // 正负面反馈

    // AI 推理结果
    AITags          []string           // AI 标签
    AIPersona       string             // 观影人格
    LastAIAnalysis  time.Time
}
```

## 观影人格类型

AI 会为用户分配观影人格，例如：
- **悬疑迷** - 喜欢烧脑悬疑片
- **浪漫主义者** - 爱看爱情片
- **动作片狂热者** - 追求刺激动作场面
- **科幻迷** - 热爱科幻作品
- **喜剧达人** - 轻松喜剧爱好者
- **纪录片爱好者** - 偏好真实内容
- **全能探索者** - 什么都看

## API 接口

### 启动 API 服务器

```go
engine := ai.NewRecommendationEngine(zhipu, trendingMgr)
learning := ai.NewLearningSystem(engine)

// 启动 API 服务器（可选）
go engine.StartRecommendationAPI("8080")
```

### HTTP 端点

```
GET  /recommend?user_id=123&count=5&strategy=personalized
     返回推荐结果

POST /feedback
     提交用户反馈
     Body: {"user_id": 123, "item_id": "456", "sentiment": "positive"}
```

## 集成到 Telegram Bot

在 `main.go` 中添加命令处理：

```go
// /ai 推荐命令
func handleAIRecommendCommand(userID int64, args string) {
    chat := ai.NewRecommendationChat(recEngine, recLearning)

    response, results, err := chat.Chat(userID, args)
    if err != nil {
        sendPrivateMessage(userID, "❌ "+err.Error(), nil)
        return
    }

    // 发送推荐结果
    sendPrivateMessage(userID, response, buildResultsKeyboard(results))
}

// 构建结果键盘
func buildResultsKeyboard(results []*ai.RecommendationResultV2) *TelegramInlineKeyboard {
    keyboard := &TelegramInlineKeyboard{InlineKeyboard: [][]map[string]string{}}

    for _, r := range results {
        row := []map[string]string{
            {"text": fmt.Sprintf("📋 %s", r.Title),
             "callback_data": fmt.Sprintf("rec_%d", r.TmdbID)},
            {"text": "❌", "callback_data": fmt.Sprintf("dislike_%d", r.TmdbID)},
        }
        keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, row)
    }

    return keyboard
}
```

## 环境变量

```bash
# 智谱 AI API Key（必需）
ZHIPU_API_KEY=your_api_key_here

# 可选：Claude API Key
CLAUDE_API_KEY=your_claude_key
```

## 配置建议

1. **开发环境**：使用 `glm-4-flash`（免费且快速）
2. **生产环境**：可升级到 `glm-4` 或 `glm-4-air` 获得更好效果
3. **缓存时间**：
   - 热门推荐：1小时
   - 新片推荐：30分钟
   - 个性化推荐：24小时

## 性能优化

1. **缓存机制**：热门推荐缓存 1 小时
2. **批量请求**：多个用户共享 AI 调用
3. **异步处理**：学习过程在后台进行
4. **增量更新**：只更新变化的数据

## 未来扩展

- [ ] 协同过滤 - 基于"相似用户"推荐
- [ ] 深度学习 - 神经网络模型
- [ ] 多模态 - 考虑海报、预告片等
- [ ] 社交推荐 - 基于朋友推荐
- [ ] A/B 测试 - 测试不同推荐策略
