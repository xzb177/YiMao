# Emby Telegram Bot - 模块化架构重构

## 概述

参考 MoviePilot 的架构设计，对 Emby Telegram Bot 进行了全面的模块化重构。

## 新架构目录结构

```
/root/emby-telegram-bot/
├── bot/                    # Bot 处理模块
│   ├── handler.go         # 消息处理入口
│   ├── editor.go          # 消息编辑和发送
│   └── module.go          # 模块集成
├── session/               # 会话管理
│   └── manager.go         # 用户会话管理器
├── callback/              # 回调处理
│   └── parser.go          # 回调数据解析
├── chain/                 # 业务链处理
│   ├── base.go            # 链基类
│   ├── search.go          # 搜索链
│   ├── subscribe.go       # 订阅链
│   └── download.go        # 下载链
└── main.go                # 主程序入口
```

## 核心模块说明

### 1. Bot Handler (`bot/handler.go`)

**功能**: 统一的消息处理入口

**核心方法**:
- `HandleMessage()` - 处理普通消息
- `HandleCallback()` - 处理按钮回调
- `handleSearch()` - 处理搜索请求
- `handlePagination()` - 处理翻页
- `handleNumericInput()` - 处理数字选择

**特点**:
- 支持命令、搜索、订阅、下载等多种操作
- 统一的消息响应格式
- 集成会话管理和回调解析

### 2. Session Manager (`session/manager.go`)

**功能**: 用户会话状态管理

**UserSession 结构**:
```go
type UserSession struct {
    UserID        int64
    ChatID        int64
    LastActive    time.Time
    CurrentPage   int
    SearchQuery   string
    SearchResults []SearchItem
    TotalResults  int
    SelectedItem  *SearchItem
    PendingAction string
    Context       map[string]interface{}
}
```

**特点**:
- 30分钟会话超时
- 自动清理过期会话
- 支持分页状态保持
- 支持上下文存储

### 3. Message Editor (`bot/editor.go`)

**功能**: 消息发送和编辑

**核心方法**:
- `SendMessage()` - 发送新消息
- `EditMessage()` - 编辑已有消息
- `DeleteMessage()` - 删除消息
- `SendMediaMessage()` - 发送图片消息
- `AnswerCallback()` - 回应按钮回调

**特点**:
- 优先编辑原消息，减少聊天刷屏
- 自动拆分长消息 (>4000字符)
- 支持 Markdown 格式
- 消息 ID 跟踪

### 4. Callback Parser (`callback/parser.go`)

**功能**: 解析和格式化回调数据

**Callback 格式**: `action:key1:value1:key2:value2`

**示例**:
```
search:id:123:type:movie
subscribe:id:456:type:tv:season:1
page:2
cancel
```

**特点**:
- 统一的回调数据格式
- 支持复杂数据传递
- 类型安全的数据访问

### 5. Chain 模块 (`chain/`)

**ChainBase** - 链基类，提供通用功能
**SearchChain** - 搜索处理链
**SubscribeChain** - 订阅处理链
**DownloadChain** - 下载处理链

## 交互流程

### 搜索流程

```
用户输入 "复仇者联盟"
    ↓
Handler.HandleMessage()
    ↓
获取/创建 UserSession
    ↓
调用 SearchChain.SearchByTitle()
    ↓
返回 SearchResult
    ↓
buildSearchResultsMessage() 构建消息
    ↓
MessageEditor.SendMessage() 发送消息
    ↓
保存结果到 UserSession
```

### 选择流程

```
用户输入 "1"
    ↓
Handler.handleNumericInput()
    ↓
从 UserSession 获取 SearchResults
    ↓
计算索引: index = num + currentPage*8 - 1
    ↓
获取选中的 SearchItem
    ↓
buildItemDetailsMessage() 构建详情
    ↓
MessageEditor.EditMessage() 编辑消息
```

### 回调流程

```
用户点击按钮 "订阅"
    ↓
Handler.HandleCallback()
    ↓
CallbackParser.Parse() 解析回调
    ↓
根据 Action 调用对应的处理函数
    ↓
执行操作 (订阅/下载/翻页)
    ↓
MessageEditor.EditMessage() 更新消息
```

## 与 MoviePilot 的对比

| 特性 | MoviePilot | Emby Bot (新架构) |
|------|-----------|-------------------|
| 语言 | Python | Go |
| 会话管理 | ✅ 30分钟超时 | ✅ 30分钟超时 |
| 消息编辑 | ✅ 优先编辑 | ✅ 优先编辑 |
| 分页支持 | ✅ 每页8条 | ✅ 每页8条 |
| 回调系统 | ✅ 灵活格式 | ✅ 统一格式 |
| Chain 模式 | ✅ 独立链 | ✅ 独立链 |
| 长消息处理 | ✅ 自动拆分 | ✅ 自动拆分 |

## 使用示例

### 初始化

```go
botModule := NewBotModule()
botModule.Init(
    os.Getenv("TELEGRAM_BOT_TOKEN"),
    os.Getenv("TELEGRAM_CHAT_ID"),
    os.Getenv("JELLYSEERR_URL"),
    os.Getenv("JELLYSEERR_API_KEY"),
)
```

### 注册路由

```go
r.POST("/webhook", botModule.GinRoute())
```

### 获取会话信息

```go
session := botModule.GetSession(userID)
query, results, page, total := session.GetSearchResults()
```

## 配置迁移

所有现有功能保持不变，新架构内部使用:

1. 消息处理 → `bot.Handler`
2. 会话管理 → `session.SessionManager`
3. 回调处理 → `callback.CallbackParser`
4. 业务逻辑 → `chain.*`

## 后续优化方向

1. **AI 集成** - 添加 AI 智能体支持 (`/ai` 命令)
2. **插件系统** - 支持动态插件加载
3. **更多 Chain** - 添加更多业务链
4. **Web UI** - 添加可视化管理界面
5. **数据持久化** - 会话数据持久化到数据库

---

**文档更新时间**: 2026-02-19
