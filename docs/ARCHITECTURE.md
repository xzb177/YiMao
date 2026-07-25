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
| `menu.go` | 主菜单、求片进度、帮助 |
| `review.go` | 审核系统 |
| `admin.go` | 管理员功能 |
| `feedback.go` | 问题工单、用户反馈与追问 |
| `wash.go` | 洗版工单、媒体库目标核验与季度选择 |
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
| `review.go` | 求片审核与洗版工单状态 |
| `request_submission.go` | 求片提交编排与幂等控制 |
| `quota.go` | 配额管理 |
| `notification.go` | 通知发送 |
| `user_mapping*.go` | 用户映射（SQLite 为主，兼容旧 JSON） |
| `admin.go` | 管理员功能 |
| `issue.go` | 问题工单与反馈处理 |
| `preferences.go` | 用户偏好 |
| `adventure*.go` | 可选电影冒险与视觉场景 |

### Session (`internal/session/`)

管理用户会话状态，支持搜索结果分页、上下文保持。

### Bot (`internal/bot/`)

| 文件 | 职责 |
|------|------|
| `webhook.go` | Webhook 模式接收 Telegram 更新 |
| `poll.go` | Polling 模式接收 Telegram 更新 |
| `command.go` | 命令处理 |
| `callback_response.go` | 统一渲染回调响应 |
| `tmdb_link.go` | 私聊 TMDB 链接直达详情 |

### UI 与富消息 (`internal/ui/`、`internal/richmessage/`)

- `internal/ui/`：文本降级样式、搜索结果和通用键盘构建。
- `internal/richmessage/`：Telegram Rich Message 卡片、欢迎页、详情、审核与游戏内容。
- `internal/services/search_card.go`、`adventure_visual.go`：海报与冒险视觉图片渲染。

## 5. 目录结构

```
YiMao/
├── cmd/
│   ├── bot/                      # Bot 程序入口
│   └── smoke/                    # Staging smoke 检查
├── internal/
│   ├── api/                      # HTTP API 路由
│   ├── bot/                      # Telegram 更新接收与响应渲染
│   ├── callback/                 # 回调解析、白名单与分发
│   ├── config/                   # 环境变量与安全配置
│   ├── handlers/                 # 搜索、求片、洗版、反馈与管理交互
│   ├── richmessage/              # Telegram Rich Message 卡片
│   ├── server/                   # HTTP 服务器
│   ├── services/                 # MoviePilot、TMDB、Emby 与业务服务
│   ├── session/                  # 会话管理
│   └── ui/                       # 文本降级样式与通用键盘
├── pkg/                          # 公共类型、日志、错误与输入验证
├── scripts/                      # 验收、运维与安全脚本
└── docs/                         # 架构、部署、运维与 Staging 文档
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
- 电影冒险与 AI 场景：`handlers/adventure.go` + `services/adventure*.go`
- 游戏中心与观影画像：`handlers/game.go` + `services/game.go` + `services/portrait.go`
- 洗版工单：`handlers/wash.go` + `services/review.go` + `services/webhook_emby_api.go`
- 搜索历史：`handlers/search_history.go` + `services/search_history*.go`

### 管理运维模块
- 管理员面板：`handlers/admin.go`
- 求片与洗版审核：`handlers/review.go` + `services/review.go`
- 配额管理：`services/quota.go`
- 问题工单：`handlers/feedback.go` + `services/issue.go`

## 8. Callback 格式规范

所有回调遵循统一格式：`action:key1:value1:key2:value2`

| Action | 格式示例 | 说明 |
|--------|----------|------|
| `start` | `start` | 打开开始菜单 |
| `search` | `search` 或 `search:type:trending` | 搜索/推荐 |
| `detail` | `detail:id:123:type:movie` | 显示媒体详情 |
| `request` | `request:id:123:type:movie` | 创建普通求片请求 |
| `wash` | `wash:id:123:type:movie` | 创建洗版工单 |
| `issue` | `issue` | 打开问题工单入口 |
| `review_complete_wash` | `review_complete_wash:token:<short>` | 管理员完成洗版工单 |
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
| 用户映射 | SQLite（自动迁移旧 JSON） | `data/user_mappings.db` |
| 许愿池 | SQLite | `data/wishpool.db` |
| 游戏与社交数据 | SQLite | `data/social.db` |
| 用户配额 | JSON | `data/user_quotas.json` |
| 用户偏好 | JSON | `data/preferences.json` |
| 求片/洗版审核工单 | JSON | `data/review_requests.json` |
| 问题工单 | JSON | `data/feedback.json` |
| 绑定请求 | JSON | `data/binding_requests.json` |
