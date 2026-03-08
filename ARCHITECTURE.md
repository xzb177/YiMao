# YiMao 架构文档

## 1. 程序入口

程序从 `cmd/bot/main.go` 启动。启动流程：

1. 加载环境变量配置（`internal/config/config.go`）
2. 初始化服务层（Telegram、MoviePilot、Session 等）
3. 初始化回调注册器并注册 handlers（`internal/callback/types.go`）
4. 设置 Webhook 或启动 Polling（`internal/bot/webhook.go` 或 `poll.go`）
5. 启动 HTTP 服务器（`internal/server/`）

## 2. 请求如何进入系统

### Webhook 模式

```
Telegram → POST /webhook → internal/server/server.go
    → internal/bot/webhook.go → 解析 Update
    → callback.Registry.Dispatch() → Handler.Handle()
```

### Polling 模式

```
Telegram (long polling) → internal/bot/poll.go
    → 解析 Update → callback.Registry.Dispatch() → Handler.Handle()
```

## 3. Callback 分发机制

所有回调数据由 `internal/callback/types.go` 统一解析：

- **格式**：`action:param1:value1:param2:value2`
- **解析器**：`callback.NewParser().Parse(data)`
- **白名单验证**：`validActions` 映射表防止非法回调
- **分发**：`callback.Registry` 根据 `Action` 字段路由到对应 Handler

已注册的 Actions 在 `internal/callback/types.go` 第 15-150 行定义。

## 4. 模块职责划分

### Handlers (`internal/handlers/`)

处理 Telegram 回调和命令，返回响应。

| 文件 | 职责 |
|------|------|
| `callback.go` | 通用回调处理（开始、返回、取消） |
| `search.go` | 搜索、推荐、搜索历史 |
| `request.go` | 创建求片请求 |
| `menu.go` | 主菜单、我的请求、帮助 |
| `review.go` | 审核系统 |
| `admin.go` | 管理员功能 |
| `feedback.go` | 用户反馈 |
| `link.go` | 账号绑定 |
| `search_history.go` | 搜索历史展示 |

### Services (`internal/services/`)

封装外部系统能力和业务逻辑。

| 文件 | 职责 |
|------|------|
| `telegram.go` | Telegram Bot API 封装、消息发送 |
| `moviepilot.go` | MoviePilot API 封装 |
| `tmdb.go` | TMDB API 封装 |
| `webhook.go` | Webhook 发送（Emby 通知） |
| `search.go` | 搜索业务逻辑 |
| `search_fallback.go` | 兜底搜索策略 |
| `search_history*.go` | 搜索历史管理（内存/缓存/数据库） |
| `review.go` | 审核业务逻辑 |
| `quota.go` | 配额管理 |
| `notification.go` | 通知发送 |
| `user_mapping.go` | 用户映射 |
| `admin.go` | 管理员功能 |
| `issue.go` | Issue/反馈处理 |
| `preferences.go` | 用户偏好 |

### Session (`internal/session/`)

管理用户会话状态，支持搜索结果分页、上下文保持。

### Bot (`internal/bot/`)

| 文件 | 职责 |
|------|------|
| `webhook.go` | Webhook 模式接收 Telegram 更新 |
| `poll.go` | Polling 模式接收 Telegram 更新 |
| `command.go` | 命令处理 |

### UI (`internal/ui/`)

消息构建器和键盘构建器。

## 5. 目录结构

```
YiMao/
├── cmd/
│   └── bot/
│       └── main.go              # 程序入口
├── internal/
│   ├── bot/                      # Telegram 消息接收
│   │   ├── webhook.go
│   │   ├── poll.go
│   │   └── command.go
│   ├── callback/                 # 回调解析与分发
│   │   └── types.go
│   ├── config/                   # 配置管理
│   │   └── config.go
│   ├── handlers/                 # 回调处理器
│   │   ├── callback.go
│   │   ├── search.go
│   │   ├── request.go
│   │   ├── menu.go
│   │   ├── review.go
│   │   ├── admin.go
│   │   ├── feedback.go
│   │   ├── link.go
│   │   └── search_history.go
│   ├── middleware/               # HTTP 中间件
│   ├── server/                   # HTTP 服务器
│   │   └── server.go
│   ├── services/                 # 业务服务层
│   │   ├── telegram.go
│   │   ├── moviepilot.go
│   │   ├── tmdb.go
│   │   ├── webhook.go
│   │   ├── search.go
│   │   ├── search_fallback.go
│   │   ├── search_history.go
│   │   ├── search_history_cache.go
│   │   ├── search_history_db.go
│   │   ├── review.go
│   │   ├── quota.go
│   │   ├── notification.go
│   │   ├── user_mapping.go
│   │   ├── admin.go
│   │   ├── issue.go
│   │   └── preferences.go
│   ├── session/                  # 会话管理
│   │   └── manager.go
│   └── ui/                       # UI 构建
│       ├── message_builder.go
│       └── keyboard_builder.go
├── ai/                           # AI 推荐模块
│   └── search.go
├── pkg/                          # 公共包
│   └── types/
└── data/                         # 运行时数据
```

## 6. 代码分层

```
┌─────────────────────────────────────────────────────────────┐
│                        Telegram Bot API                      │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │
┌─────────────────────────────────────────────────────────────┐
│  internal/bot/ (webhook.go, poll.go) - Update 接收           │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │
┌─────────────────────────────────────────────────────────────┐
│  internal/callback/ (types.go) - 解析与分发                   │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │
┌─────────────────────────────────────────────────────────────┐
│  internal/handlers/ - 回调处理                               │
│  ┌──────────┬──────────┬──────────┬──────────┬────────────┐  │
│  │ callback │  search  │ request  │  review  │   admin    │  │
│  └──────────┴──────────┴──────────┴──────────┴────────────┘  │
└─────────────────────────────────────────────────────────────┘
         ▲                              │
         │                              ▼
┌─────────────────────────┐  ┌─────────────────────────────────┐
│  internal/services/     │  │  internal/session/              │
│  - telegram             │  │  - 会话状态管理                   │
│  - moviepilot           │  └─────────────────────────────────┘
│  - tmdb                 │
│  - search               │
│  - review               │
│  - notification         │
└─────────────────────────┘
         ▲
         │
┌─────────────────────────────────────────────────────────────┐
│  外部系统：MoviePilot / Emby / TMDB                          │
└─────────────────────────────────────────────────────────────┘
```

## 7. 核心链路 vs 增强模块

### 核心链路（不可少）
- 搜索：`handlers/search.go` + `services/search.go` + `services/moviepilot.go`
- 请求：`handlers/request.go` + `services/review.go`
- 通知：`services/notification.go` + `services/webhook.go`

### 增强模块（可选）
- AI 推荐：`ai/` 目录
- 审核系统：`handlers/review.go` + `services/review.go`
- 搜索历史：`handlers/search_history.go` + `services/search_history*.go`

### 管理运维模块
- 管理员面板：`handlers/admin.go`
- 配额管理：`services/quota.go`
- 用户反馈：`handlers/feedback.go` + `services/issue.go`

## 8. Callback 格式规范

所有回调遵循统一格式：`action:key1:value1:key2:value2`

| Action | 格式示例 | 说明 |
|--------|----------|------|
| `start` | `start` | 打开开始菜单 |
| `search` | `search` 或 `search:type:trending` | 搜索/推荐 |
| `detail` | `detail:id:123:type:movie` | 显示媒体详情 |
| `request` | `request:id:123:type:movie` | 创建媒体请求 |
| `back` | `back` | 返回上一页 |
| `cancel` | `cancel` | 取消当前操作 |
| `ai` | `ai:type:trending` | AI 推荐 |
| `mood` | `mood` | 心情选片 |

完整 Action 列表见 `internal/callback/types.go`。

## 9. 会话管理

会话由 `internal/session/manager.go` 管理：

- **超时时间**：30 分钟无活动自动清理
- **存储内容**：搜索结果、当前页码、选中项、上下文数据
- **用途**：支持分页、返回上一页、状态保持

## 10. 数据存储

| 类型 | 存储方式 | 位置 |
|------|----------|------|
| 会话数据 | 内存 | `session.Manager` |
| 搜索历史 | SQLite | `data/search_history.db` |
| 用户配额 | JSON | `user_quotas.json` |
| 用户偏好 | JSON | `preferences.json` |
| 用户映射 | JSON | `user_mappings.json` |
| 绑定请求 | JSON | `binding_requests.json` |
