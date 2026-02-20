# API 错误处理最佳实践

## 概述

本文档说明了企业级 API 错误处理的实现方式，以及如何在未来避免类似问题。

## 修改的问题

### 1. /ai 命令显示"未知命令"

**原因**: `/ai` 命令没有在 `main.go` 的 switch 语句中处理。

**解决方案**: 在 `main.go` 的命令处理 switch 中添加了 `/ai` 命令的处理分支，调用 `ai.HandleAICommand()`。

**位置**: `main.go:2289-2303`

### 2. /recommend 命令未连接 AI 模块

**原因**: `/recommend` 命令只显示"功能开发中"消息，没有实际调用 AI 模块。

**解决方案**:
- 连接到 `ai.GetAIRecommendations()` 函数
- 添加心情参数解析
- 提供友好的错误消息

**位置**: `main.go:2294-2332`

### 3. API 400 错误信息不友好

**原因**: Jellyseerr API 返回的错误直接显示给用户，缺少上下文和可操作的指导。

**解决方案**:
- 创建 `formatAPIError()` 函数，将错误转换为用户友好的消息
- 在 `jellyseerr.go` 中添加企业级错误处理
- 根据错误类型提供具体的诊断信息

**位置**: `main.go:4647-4722`, `jellyseerr.go:147-252`

## 企业级错误处理模式

### 1. 错误分类

```go
// 认证错误 (401)
case strings.Contains(errMsg, "401"):
    // 提示 API Key 无效

// 权限错误 (403)
case strings.Contains(errMsg, "403"):
    // 提示权限不足

// 资源未找到 (404)
case strings.Contains(errMsg, "404"):
    // 提示资源不存在

// 请求格式错误 (400)
case strings.Contains(errMsg, "400"):
    // 尝试解析并显示实际错误详情
```

### 2. 结构化错误响应

```go
type APIErrorResponse struct {
    ErrorMessage string `json:"errorMessage"`
    Error        string `json:"error"`
    StatusCode   int    `json:"statusCode"`
    Message      string `json:"message"`
}
```

### 3. 错误详情提取

当 API 返回 400 错误时，尝试：
1. 解析 JSON 错误响应
2. 提取实际错误消息
3. 提供可操作的指导

### 4. 日志记录

所有 API 错误都应记录到日志，包含：
- 请求方法
- 请求路径
- 错误响应

## 防止未来错误的指导原则

### 1. 新命令注册

添加新命令时，确保：
1. 在 `command_center.go` 中注册命令
2. 在 `main.go` 的 switch 中添加处理分支
3. 提供友好的错误消息

### 2. API 集成

集成新 API 时，确保：
1. 使用 `apperrors` 包创建结构化错误
2. 在 `makeRequest` 级别处理错误
3. 提供诊断信息
4. 记录详细日志

### 3. 用户反馈

向用户显示错误时，确保：
1. 使用友好的语言
2. 提供可操作的指导
3. 区分不同类型的错误
4. 避免暴露内部实现细节

### 4. 错误消息格式

```
❌ *操作失败*

*错误类型*

可能原因：
• 原因 1
• 原因 2

📋 解决方案/指导
```

## 示例

### 好的错误消息

```
❌ *API 认证失败*

可能原因：
• API Key 无效或已过期
• API Key 配置错误

📋 请联系管理员检查 JELLYSEERR_API_KEY 配置
```

### 不好的错误消息

```
❌ 获取失败: API error: 401 - {"error":"Unauthorized"}
```

## 测试建议

1. 测试各种 HTTP 状态码（400, 401, 403, 404, 429, 500）
2. 验证错误消息的友好性
3. 确保日志包含足够的调试信息
4. 测试网络故障场景
