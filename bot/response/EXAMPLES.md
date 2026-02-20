# 企业级消息响应系统 - 使用指南

## 概述

`bot/response` 包提供了企业级的消息响应系统，用于统一管理 Telegram Bot 的所有用户交互消息。

## 核心组件

### 1. ResponseType - 响应类型

```go
const (
    ResponseTypeSuccess ResponseType = iota  // ✅ 成功
    ResponseTypeError                       // ❌ 错误
    ResponseTypeInfo                        // ℹ️ 信息
    ResponseTypeWarning                     // ⚠️ 警告
    ResponseTypeLoading                     // ⏳ 加载中
    ResponseTypeProgress                    // 📊 进度
)
```

### 2. Severity - 严重程度

```go
const (
    SeverityLow    Severity = iota  // 低
    SeverityMedium                  // 中
    SeverityHigh                    // 高
    SeverityCritical                // 严重
)
```

### 3. Builder 模式

```go
import "emby-telegram-bot/bot/response"

// 创建成功响应
resp := response.NewBuilder().
    WithType(response.ResponseTypeSuccess).
    WithTitle("操作成功").
    WithMessage("您的请求已成功处理").
    WithDetails("可以在「我的请求」中查看进度").
    Build()
```

## 预定义模板

### 搜索相关

```go
// 搜索中
data := response.TemplateData{MediaTitle: "复仇者联盟"}
resp := response.RenderTemplate(response.TemplateSearchInProgress, data)

// 无结果
resp := response.RenderTemplate(response.TemplateSearchNoResults, data)

// 搜索错误
data.Error = err.Error()
resp := response.RenderTemplate(response.TemplateSearchError, data)
```

### 请求相关

```go
// 请求成功
data := response.TemplateData{
    MediaTitle:     "复仇者联盟",
    MediaType:      "movie",
    QuotaUsed:      1,
    QuotaLimit:     2,
    QuotaRemaining: 1,
    QuotaType:      "电影",
}
resp := response.RenderTemplate(response.TemplateRequestSuccess, data)

// 配额用完
data := response.TemplateData{
    QuotaType:  "电影",
    QuotaUsed:  2,
    QuotaLimit: 2,
}
resp := response.RenderTemplate(response.TemplateRequestQuotaExhausted, data)

// 账号未绑定
resp := response.RenderTemplate(response.TemplateAccountNotLinked, response.TemplateData{})
```

### 系统相关

```go
// 网络错误
resp := response.RenderTemplate(response.TemplateNetworkError, response.TemplateData{
    Error: err.Error(),
})

// 频率限制
resp := response.RenderTemplate(response.TemplateRateLimited, response.TemplateData{
    RetryAfter: 30 * time.Second,
})

// 操作超时
resp := response.RenderTemplate(response.TemplateOperationTimeout, response.TemplateData{
    MediaTitle: "搜索媒体",
})
```

## Handler 使用

### 创建 Handler

```go
handler := response.NewHandler()
defer handler.Shutdown()
```

### 进度跟踪

```go
// 开始进度跟踪
state := handler.StartProgress("req-123", "search", 3)

// 更新进度
handler.UpdateProgress("req-123", "正在搜索...", 1)
handler.UpdateProgress("req-123", "处理结果...", 2)

// 完成进度
handler.CompleteProgress("req-123", "搜索完成")
```

### 订阅进度更新

```go
ch := handler.SubscribeToProgress("req-123")
for update := range ch {
    fmt.Printf("Progress: %.0f%% - %s\n", update.Percentage, update.Message)
}
handler.UnsubscribeFromProgress("req-123", ch)
```

## Tracker 使用

### 创建操作

```go
tracker := response.NewTracker()
defer tracker.Shutdown()

op := tracker.Create("search_media", userID, chatID)
```

### 使用 Context

```go
ctx := response.NewContext(tracker, op)

// 更新状态
ctx.Update(response.StatusRunning, 25, "第一步完成")
ctx.Update(response.StatusRunning, 50, "第二步完成")

// 设置元数据
ctx.SetMetadata("query", "复仇者联盟")
ctx.SetMetadata("page", 1)

// 获取元数据
if query, ok := ctx.GetMetadata("query"); ok {
    fmt.Printf("Query: %v\n", query)
}

// 完成操作
ctx.Complete("操作完成")

// 或失败
ctx.Fail("操作失败", err)
```

## Integration 使用

```go
integration := response.NewIntegration()

// 搜索中
callbackResp := integration.SearchInProgress("查询内容")

// 无结果
msgResp := integration.SearchNoResults("查询内容")

// 请求成功
callbackResp := integration.RequestSuccess("标题", "movie", 1, 2, 1, false)

// 配额用完
callbackResp := integration.QuotaExhausted("movie", 2, 2)

// 账号未绑定
callbackResp := integration.AccountNotLinked()

// 频率限制
callbackResp := integration.RateLimited(30)

// 网络错误
msgResp := integration.NetworkError(err)

// 操作超时
msgResp := integration.OperationTimeout("搜索媒体")

// 简单响应
msgResp := integration.Success("操作成功")
msgResp = integration.Error("操作失败")
msgResp = integration.Info("提示信息")
msgResp = integration.Warning("警告信息")
```

## 在 Bot Handler 中集成

```go
func (h *Handler) handleSearch(query string) *bot.MessageResponse {
    // 发送搜索中响应
    loadingResp := h.integration.SearchInProgress(query)
    h.messageEditor.SendMessage(chatID, loadingResp.Text, loadingResp.Keyboard)

    // 执行搜索
    results, err := h.searchChain.Search(query)
    if err != nil {
        return h.integration.SearchError(query, err)
    }

    if len(results) == 0 {
        return h.integration.SearchNoResults(query)
    }

    // 构建结果响应...
}
```

## 本地化

```go
provider := response.NewLocalizationProvider(response.LocaleZH)
provider.SetLocale(response.LocaleEN)

// 获取模板
template := provider.GetTemplate(response.TemplateSearchInProgress)

// 格式化消息
data := map[string]interface{}{
    "query": "复仇者联盟",
}
message := provider.FormatMessage(response.TemplateSearchInProgress, data)
```

## 响应示例

### 成功响应

```
✅ 操作成功

您的请求已成功处理

可以在「我的请求」中查看进度
```

### 错误响应

```
❌ 请求失败

无法连接到服务器

请检查网络连接或稍后再试

💡 检查网络设置
💡 联系管理员
```

### 配额用完

```
⚠️ 配额已达上限

🚫 今日电影配额已用完

今日已请求 2 部电影，达到每日限额 2 部

💡 明天配额会自动重置，请明天再试
```

### 进度更新

```
⏳ 处理中

[1/3] 正在搜索...
```

## 最佳实践

1. **始终使用模板**: 使用预定义模板确保消息格式一致
2. **跟踪长时间操作**: 使用 Handler 跟踪进度，向用户提供反馈
3. **使用 Tracker**: 对于复杂操作，使用 Tracker 管理生命周期
4. **提供有用建议**: 在错误消息中包含解决建议
5. **设置正确的严重程度**: 帮助用户理解问题的重要性
6. **使用 Builder 模式**: 创建自定义响应时使用 Builder 模式

## 性能考虑

- 所有组件都是并发安全的
- Handler 和 Tracker 会自动清理过期数据
- 使用 sync.RWMutex 保护共享状态
- 进度更新使用通道进行异步通知
