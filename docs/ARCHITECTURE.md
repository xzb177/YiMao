# YiMao 项目流程

## 1. 系统边界

YiMao 是 Telegram 入口和媒体任务编排层，不替代 MoviePilot 的下载/整理能力，也不替代 Emby 的媒体库和播放能力。

```text
Telegram Bot / Mini App
        │
        ▼
YiMao HTTP + callback registry + services
        │
        ├── MoviePilot：搜索、订阅、下载状态、入库状态
        ├── Emby：媒体可见性与 Playback-ready 通知
        ├── TMDB：标题、年份、类型、海报和详情
        ├── AI provider：可选的 Mini App assistant / 冒险文案
        └── SQLite + JSON：绑定、历史、许愿、社交、审核和偏好
```

运行时数据全部位于 `/app/data`，宿主机对应 named volume `yimao-data`。会话在内存中，重启后不保留临时搜索分页上下文。

## 2. 启动流程

`cmd/bot/main.go` 是唯一程序入口：

1. 读取环境变量并执行配置校验。
2. 创建 Telegram、MoviePilot、TMDB、Emby webhook、权限、配额和各业务 service。
3. 初始化 SQLite 数据库和 JSON store，必要时执行表迁移。
4. 注册 Telegram commands、callback actions 与 handlers。
5. 根据 `WEBHOOK_URL` 选择 Telegram webhook 或 long polling。
6. 启动 HTTP server。
7. Docker healthcheck 请求 `/health`；该接口同时检查配置的 MoviePilot/Emby 依赖。

## 3. Telegram 请求链路

### Bot polling

```text
Telegram getUpdates
  -> internal/bot/poll.go
  -> Update 解析
  -> callback.Registry / command dispatcher
  -> internal/handlers 或 internal/services
  -> Telegram response + inline keyboard
```

### Bot webhook

```text
Telegram POST /webhook
  -> internal/server/server.go
  -> internal/bot/webhook.go
  -> callback.Registry / command dispatcher
  -> handler/service
```

命令处理在 `internal/bot/command.go`，搜索和求片等业务 handler 位于 `internal/handlers/`。按钮 callback 使用白名单 action parser，避免把任意 callback 字符串当作业务命令执行。

## 4. Mini App 链路

```text
Telegram chat menu
  -> GET /miniapp
  -> Telegram WebApp initData
  -> /api/miniapp/v1/*
  -> HMAC initData 校验 + auth age 校验
  -> Mini App Server handler
  -> shared services / MoviePilot / TMDB / SQLite
```

当前 Mini App API 路由包括：

- 发现与查询：`search`、`assistant`、`detail`、`discover`
- 首页状态：`dynamic`、`me`、`progress`
- 用户动作：`watchlist`、`request`、`wash`、`request/cancel`
- 阻塞项和玩法：`issues`、`wishes`、`adventure`

HTML shell 可以公开加载；业务 API 依赖 Telegram `initData`，不能用静态页面访问绕过身份校验。Mini App 发布后通过 `./manage.sh telegram` 更新菜单中的 `?v=<OCI Git revision>`，避免 Telegram WebView 长期缓存旧 bundle。

## 5. 普通求片主链路

```text
用户输入片名
  -> YiMao 调 MoviePilot 搜索
  -> 展示 TMDB/源信息与详情
  -> 用户选择电影或剧集季
  -> 检查 Telegram 身份、账号绑定、配额和重复请求
  -> RequestSubmissionService 写入幂等请求
  -> ReviewService 按策略自动通过或进入管理员审核
  -> MoviePilot 创建 subscription
  -> YiMao 保存 subscription/request 状态
  -> MoviePilot webhook 更新下载/整理进度
  -> Emby webhook 确认媒体入库
  -> Telegram / Mini App 展示“可以看”通知
```

“下载完成”和“可以看”是两个状态：前者由 MoviePilot 事件推动，后者需要媒体库已发现或入库事件确认。外部系统失败时保留阻塞状态和重试信息，不伪装成空列表或成功。

## 6. 洗版、许愿与反馈

### 洗版

管理员或用户从媒体详情创建 wash 工单。系统记录目标版本/季信息，由管理员审核后调用 MoviePilot 执行，完成后由 Emby 事件确认目标媒体可见。旧版本是否删除属于独立清理动作，不能由识别失败路径直接删除。

### 许愿池

找不到可订阅资源的项目进入 `wishpool.db`，canonical key 用媒体身份合并重复许愿。后台按间隔重搜，状态通常经过 `PENDING/WISHED -> SEARCHING -> FOUND -> NOTIFIED -> FULFILLED`，也可能进入 `EXPIRED` 或 `ORPHANED`。

### 问题反馈

用户从 Bot 或 Mini App 创建 issue。Issue 与当前任务关联，管理员回复后保留公开状态和内部处理记录；外部 MoviePilot/Emby 不可用时，问题项进入“卡住”状态，等待补偿或人工处理。

## 7. 数据存储

| 数据 | 实现 | 典型文件 |
| --- | --- | --- |
| 用户映射 | SQLite，兼容旧 JSON 迁移 | `user_mappings.db` |
| 搜索历史 | SQLite | `search_history.db` |
| 许愿池 | SQLite WAL | `wishpool.db` |
| 游戏/社交 | SQLite | `social.db` |
| 配额、偏好 | JSON | `user_quotas.json`、`preferences.json` |
| 求片/洗版审核 | JSON | `review_requests.json` |
| 反馈、绑定请求、通知设置 | JSON | 对应 `feedback.json`、`binding_requests.json` 等 |

更新前必须通过 `./manage.sh backup` 停止服务并归档整个 volume，不能只复制某个 SQLite 主文件而遗漏 `-wal` 和 `-shm`。

## 8. 安全边界

- Bot Token、API Key、Webhook secret 只进受控 `.env`，文件权限 `0600`。
- `ADMIN_USER_IDS` 显式决定管理员；`/link` 不会自动提权。
- 管理 API 在启用 auth 时要求 `API_KEYS`；关闭 auth 时仅允许 localhost。
- 入站 webhook 支持 secret/signature 校验，公网代理只暴露必要路径。
- Mini App API 校验 Telegram `initData`；不接受浏览器自造的 user ID。
- Docker socket 仅用于 MoviePilot 密码重置等明确功能，应限制主机暴露面。
- 日志和最终报告不输出 Token、密码、完整内网地址或 API Key。

## 9. 代码变更与发布链路

```text
修改代码
  -> gofmt / 单测
  -> docker build --target verify
  -> Docker production image + OCI revision
  -> staging smoke / Mini App 真机验收
  -> backup named volume
  -> 健康门控的事务部署
  -> Telegram Mini App revision 菜单更新
  -> health、依赖、重启次数和关键用户流程验收
```

部署入口：

```text
install.sh       首次拉取仓库并转到 manage.sh install
manage.sh        唯一运维入口
update.sh        兼容转发到 manage.sh update
deploy.sh        兼容转发到 manage.sh install
deploy-docker.sh 兼容转发到 manage.sh install
scripts/preflight.sh 只做代码、配置和 Docker verify 门禁
```
