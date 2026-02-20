# 命令处理最佳实践

## 概述

本文档说明了企业级的命令处理架构，以及如何确保每个命令都有独立、明确的功能。

## 问题分析

### 原始问题

`/start` 和 `/help` 命令显示相同的内容，原因：

1. **命令合并处理**: `case "/start", "/help":` 将两个命令合并为同一个处理逻辑
2. **缺少职责分离**: 没有明确的命令响应管理模块
3. **硬编码消息**: 响应内容直接写在 switch 语句中

### 企业级解决方案

## 架构设计

### 1. 命令响应管理模块

```
bot/response/
├── commands.go       # 命令响应构建器
├── templates.go      # 通用消息模板
├── types.go          # 响应类型定义
└── locales.go        # 多语言支持
```

### 2. 命令处理原则

#### 原则 1: 每个命令独立处理

```go
// ❌ 错误 - 命令合并处理
case "/start", "/help":
    sendHelpMessage()

// ✅ 正确 - 每个命令独立处理
case "/start":
    sendWelcomeMessage()
case "/help":
    sendHelpMessage()
```

#### 原则 2: 命令有明确的语义

| 命令 | 用途 | 用户场景 |
|------|------|----------|
| `/start` | 欢迎新用户，问候老用户 | 首次使用或返回使用 |
| `/help` | 显示完整的使用指南 | 需要了解功能 |

#### 原则 3: 响应内容由专门模块管理

```go
// 使用 CommandResponseBuilder 构建响应
builder := NewCommandBuilder(ctx)
resp := builder.BuildStartCommand()
```

### 3. 命令上下文

每个命令响应都基于上下文构建：

```go
type CommandContext struct {
    UserID      int64   // 用户ID
    Username    string  // 用户名
    FirstName   string  // 名字
    IsAdmin     bool    // 是否管理员
    IsNewUser   bool    // 是否新用户
    IsReturning bool    // 是否回头用户
}
```

## 实现细节

### /start 命令

**新用户体验**:
```
🎉 欢迎来到云海看板娘！

我是你的智能影视助手，帮你：

🔍 搜索内容 - 直接输入电影/剧集名称
📋 发起请求 - 自动下载你想看的内容
🔔 自动通知 - 完成后第一时间通知你

💡 快速开始
试试输入：「复仇者联盟」
```

**回头用户体验**:
```
👋 欢迎回来，用户名！

我可以帮你搜索和请求影视内容

🔍 快速搜索
直接输入电影或剧集名称

📋 其他功能
/help - 查看完整帮助
/recommend - 智能推荐
/profile - 我的资料
```

### /help 命令

**普通用户**:
```
📖 使用指南

🔍 搜索内容
直接输入电影或剧集名称即可搜索

📋 发起请求
搜索后点击「📋 请求」按钮

🎯 高级功能
/recommend - 智能推荐
/trending - 热门搜索
/profile - 我的资料
/link - 绑定账号
```

**管理员**:
```
🔧 管理员功能

📋 请求管理
/pending - 查看待处理请求
/approve <ID> - 批准请求
/decline <ID> - 拒绝请求

👥 用户管理
/users - 查看用户列表
/bindrequests - 绑定请求

📊 系统管理
/stats - 系统统计
/addadmin - 添加管理员
```

## 防止未来错误的指导原则

### 1. 命令注册清单

添加新命令时，必须完成以下检查：

- [ ] 在 `command_center.go` 中注册命令
- [ ] 在 `main.go` 的 switch 中添加**独立**的 case
- [ ] 确保命令与现有命令不会产生冲突
- [ ] 定义命令的响应内容
- [ ] 更新帮助文档

### 2. 命令语义检查

| 问题 | 检查方法 |
|------|----------|
| 命令功能重叠 | 比较命令的响应内容 |
| 命令合并处理 | 检查 switch case 中是否有逗号分隔的命令 |
| 缺少命令处理 | 在 default case 中记录未知命令 |

### 3. 代码审查清单

- [ ] 每个命令是否有独立的 case？
- [ ] 命令响应是否使用专门模块构建？
- [ ] 新用户和老用户的响应是否不同（如适用）？
- [ ] 管理员和普通用户的响应是否不同（如适用）？

### 4. 测试指南

```bash
# 测试所有命令是否独立工作
/start   # 应该显示欢迎消息
/help    # 应该显示帮助指南

# 测试用户状态
/profile # 需要绑定账号
/daily   # 需要返回用户数据
```

## 命令分类

### 基础命令 (Basic)
- `/start` - 开始使用
- `/help` - 帮助指南

### 搜索命令 (Search)
- `/search` - 搜索内容
- `/recommend` - 智能推荐
- `/trending` - 热门搜索
- `/history` - 搜索历史

### 个人命令 (Personal)
- `/profile` - 我的资料
- `/daily` - 每日签到
- `/my` - 我的请求
- `/quota` - 配额查询
- `/prefs` - 通知设置

### 社交命令 (Social)
- `/leaderboard` - 排行榜
- `/challenges` - 每日挑战
- `/badges` - 我的成就
- `/top` - 热门内容
- `/activity` - 活跃用户

### 账号命令 (Account)
- `/link` - 绑定账号
- `/quicklink` - 快速绑定
- `/unlink` - 解绑账号

### 管理员命令 (Admin)
- `/pending` - 待处理请求
- `/approve` - 批准请求
- `/decline` - 拒绝请求
- `/users` - 用户列表
- `/stats` - 系统统计

## 相关文件

- `main.go` - 命令处理入口
- `command_center.go` - 命令注册中心
- `bot/response/commands.go` - 命令响应构建器
- `bot/response/templates.go` - 通用消息模板
