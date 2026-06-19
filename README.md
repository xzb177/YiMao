# YiMao · Telegram 影视求片助手

<div align="center">

**在 Telegram 内完成「搜片 → 求片 → 查进度 → 收通知」的完整闭环**

配合 MoviePilot + Emby/Jellyfin 使用，求片众筹、追剧进度一站搞定，无需切换 App

[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![Docker Pull](https://img.shields.io/badge/docker-xzb177%2Fyimao-blue?logo=docker)](https://hub.docker.com/r/xzb177/yimao)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Stars](https://img.shields.io/github/stars/xzb177/YiMao?style=social)](https://github.com/xzb177/YiMao/stargazers)

[核心功能](#-核心功能) · [命令速查](#-命令速查) · [快速开始](#-快速开始) · [配置说明](#%EF%B8%8F-配置说明) · [部署指南](#-部署指南)

</div>

---

## ⚠️ 更新提示

**更新前请务必阅读 [更新指南](docs/UPDATE.md)，避免数据丢失。**

---

## 简介

YiMao（云海影视助手）是一个 Telegram Bot，把"找片—求片—等片—看片"这条链路全部搬进聊天窗口：

- **搜片**：直接发片名，Bot 返回 TMDB 候选，点进去看详情。
- **求片**：详情页一键发起，走管理员审核 + 配额控制，通过后由 MoviePilot 自动下载。
- **等片**：候选列表有"预产期灯牌"给心理预期；搜不到的片可以丢进**许愿池**，找到源主动私信通知；多人想看的同一部片可以**拼车 +1**。
- **看片**：入库后自动推送通知；追剧能看到**集数进度条**；还有每日入库汇总、每周观影周报。

下载、刮削、入库由 **MoviePilot** 负责，媒体库与入库回调由 **Emby / Jellyfin** 提供，YiMao 是把这套后端能力包装成"人话"交互的前台。

> 隐私保护：搜片、求片、进度、配额等个人操作在私聊中进行；群组仅用于接收入库通知、拼车到货提醒和公告。

---

## ✨ 核心功能

### 🔍 智能搜片
私聊直接发送片名（中英文均可），返回 TMDB 候选列表；点击进入详情页查看简介、年份、海报，并决定是否求片。也支持 `🔥 本周热门 / 📺 热门剧集 / ⭐ 必看神作 / 🆕 最新上映 / 🎲 随机探索` 等精选入口。

### 🎬 求片（审核流 + 配额）
详情页点「求片」即可发起请求：
- **绑定校验**：首次使用自动绑定，无需注册。也可用 `/link 用户名` 手动绑定已有账号。
- **配额扣减**：电影扣 **1**，剧集扣 **3**（一整季对服务器资源消耗更大），每日 `00:00` 重置（按访问惰性刷新）。默认电影/剧集各 2 个/天。
- **去重提醒**：媒体库已存在或已有相同请求时会提示，可选择"仍要订阅"。
- **审核流**：请求进入管理员审核，管理员可 **✅ 批准 / ❌ 拒绝**。批准后调用 MoviePilot 下单；若下单失败会标记为 **stuck**（兜底保留，可重试，不会丢单）；拒绝时自动**返还配额**。

### 🚦 候选预产期灯牌
浏览某部片的候选资源列表时，根据已扫到的站点做种情况给出一个轻量"状态灯牌"（只给预期、不承诺精确时间）：

| 灯牌 | 含义 | 判定（去重后"有做种"的站点数） |
| --- | --- | --- |
| ⚡ | 资源充足，很快到货 | ≥ `ETA_THRESHOLD_HIGH`（默认 3） |
| 🔄 | 已有源，需等做种 | 1 ~ 阈值−1 |
| 🐢 | 暂无站点出源 | 0 |
| ❓ | 数据不足，仍在找源 | 候选为空 / 数据缺失 |

### 📈 剧集进度条
追剧时在入库通知里展示集数进度条，如 `📈 已更 S03E07/S03E16 ▓▓▓▓░░░░ 44%（距完结还差 9 集）`，完结显示 `100%（已完结）`。分母取自 TMDB 当季实际集数，支持本季 / 全剧双口径。

### 🙋 拼车 +1
详情页点「我也想看」，把自己加入这部片的拼车列表，并提示"已加入，共 N 人想看"。该片到货时会在群里 @ 拼过车的用户。

### 🌟 许愿池（`/wish`）
搜不到的片可以许愿，找到源第一时间通知你：
- `/wish 片名` 入池：TMDB 强校验匹配、与现有订阅去重、容量上限（每人 20 条 / 全局 500 条）。
- `/wish`（无参）列出"我的许愿"，可取消。
- **众筹计数**：同一部片多人许愿会合并，重复许愿时提示"目前 **N 人在等**，出源会自动通知你"。
- **定时重搜**：调度器约每天对每条许愿重搜一次（错峰），命中（做种数 ≥ `WISH_MIN_SEEDERS`）后**主动私信**许愿人"🎉 你许愿的影片出源啦！"并附「🎬 立即求片」按钮；其他许愿者也会收到提醒。出源后**仅通知不自动求片**，需用户点按钮确认后才走求片流程。
- **超期处理**：入池 `WISH_EXPIRE_DAYS`（默认 30）天仍无源会做一次最终重搜，仍无源则取消并私信发起人。

> 状态机：`PENDING → SEARCHING → FOUND → NOTIFIED → FULFILLED`，旁路 `EXPIRED`（超期/放弃）/ `ORPHANED`（用户退群/不可达）。

### 📋 我的请求（聚合视图）
`/requests`（或 `/watchlist`）把分散的状态聚合成一页，按状态分组展示并标出计数：

- **【进行中】**：审核中 / 下载中 / 搜索中等
- **【许愿中】**：许愿池里待出源的条目
- **【已完成】**：已入库
- **【异常/失败】**：失败 / 已取消

数据同时来自 MoviePilot 订阅、待处理的审核单和许愿池，避免来回切界面查状态。

### 🎬 AI 推荐 · 今晚看什么
点「🎬 今晚看什么」或发送 `/ai`，进入一次性 AI 对话模式，把口味/心情用一句话告诉 Bot（如"想看烧脑悬疑、别太长"），由 AI 帮你挑片。需配置 AI Key 或将 `AI_ENABLED=true`。

> 说明：AI 推荐是**按钮/命令触发**的交互，不存在"AI 电台主动定时推送"。系统自带的每日 9:00 推荐是基于 MoviePilot 热门榜（非 AI 生成），且受用户通知开关控制。

### 📊 观影周报
每周一 9:00 自动生成并按用户通知开关推送（也可在通知设置中开关）。内容包括本周搜索/求片/通过数、剩余配额、行为标签、热搜关键词、类型偏好与专属建议。

### 🔔 通知设置与每日汇总
「设置 → 通知设置」可单独开关：入库通知 / 每日推荐 / 周报 / 公告。每日 `DAILY_SUMMARY_HOUR`（默认 21:00）推送当天的入库汇总。

### 🤖 /status
查看 Bot 运行状态：版本、服务端时间、当前用户 ID、聊天类型、是否管理员。管理员会额外看到部署诊断：MoviePilot、Emby、AI、Rich Message、Webhook Secret、密码重置和用户映射存储配置状态。

---

## ⌨️ 命令速查

| 命令 | 作用 | 权限 |
| --- | --- | --- |
| `/start` | 打开主菜单 | 所有人 |
| `/search` | 提示进入搜片（直接发片名即可） | 所有人 |
| `/ai [描述]` | 今晚看什么 · AI 推荐（需启用 AI） | 所有人 |
| `/wish [片名]` | 许愿入池 / 查看我的许愿 | 所有人 |
| `/requests` | 我的请求聚合视图（私聊） | 所有人 |
| `/watchlist` | 我的片单（同 `/requests`，私聊） | 所有人 |
| `/quota` | 查看今日配额（私聊） | 所有人 |
| `/link 用户名` | 新用户自动创建并绑定 MoviePilot 账号 | 所有人 |
| `/link 用户名 密码` | 绑定已有 MoviePilot 账号（需密码验证） | 所有人 |
| `/resetpw` | 重置自己已绑定的 MoviePilot 密码 | 所有人 |
| `/resetpw 用户名` | 重置指定 MoviePilot 用户密码 | 管理员 |
| `/status` | 查看 Bot 运行状态 | 所有人 |
| `/id` | 查看当前聊天 / 用户 ID | 所有人 |
| `/help` | 功能与命令帮助 | 所有人 |

> 管理操作（审核、配额管理等）通过菜单内「🛠️ 管理」入口进入，仅管理员可见。

---

## 🚀 快速开始

### 方式一：Docker（推荐）

```bash
# 1. 准备配置
cp .env.example .env
vim .env        # 至少填写 TELEGRAM_BOT_TOKEN / MOVIEPILOT_URL / MOVIEPILOT_API_KEY

# 2. 拉起（host 网络，便于访问内网 MoviePilot / Emby）
docker compose up -d

# 3. 查看日志
docker compose logs -f
```

`docker-compose.yml` 默认使用 `network_mode: host`，数据持久化到 `./data`。出于安全考虑，默认不挂载 Docker socket，`/resetpw` 默认关闭；如需启用密码重置，请额外叠加 `docker-compose.resetpw.yml`：

```bash
docker compose -f docker-compose.yml -f docker-compose.resetpw.yml up -d
```

### 方式二：本地构建运行

```bash
# 需要 Go 1.24
go build -o yimao ./cmd/bot
export $(grep -v '^#' .env | xargs)
./yimao
```

服务默认监听 `:8080`，提供 `/health` 健康检查。首次部署后，用 `/link` 绑定的第一个用户会自动成为管理员，也可用 `ADMIN_USER_IDS` 预先指定。

---

## ⚙️ 配置说明

所有配置通过环境变量提供，完整示例见 [`.env.example`](.env.example)。

### 核心（必需）

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `TELEGRAM_BOT_TOKEN` | ✅ | — | 从 @BotFather 获取的 Bot Token |
| `ADMIN_USER_IDS` | 推荐 | 空 | 管理员 TG ID，逗号分隔；留空则首个 `/link` 用户成为管理员 |
| `TELEGRAM_CHAT_ID` | | 空 | 群组 Chat ID，用于群组通知；留空则不发群通知 |
| `PORT` | | `8080` | 服务监听端口 |
| `TZ` | | `Asia/Shanghai` | 时区 |

### MoviePilot（必需）

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `MOVIEPILOT_URL` | ✅ | `http://localhost:4500` | MoviePilot 地址 |
| `MOVIEPILOT_API_KEY` | ✅ | — | MoviePilot API Key（候选资源列表依赖此项） |
| `DOWNLOAD_SAVE_PATH` | | 空 | 自定义下载目录，多实例共用一个 MoviePilot 时区分用 |
| `MOVIEPILOT_CONTAINER` | 条件 | `moviepilot-v2` | `/resetpw` 使用，MoviePilot 容器名；需叠加 `docker-compose.resetpw.yml` |
| `MOVIEPILOT_DB_PATH` | 条件 | 空 | `/resetpw` 使用，MoviePilot 容器内 user.db 路径，如 `/config/user.db`；留空则关闭密码重置 |

### Emby / Jellyfin（可选但推荐）

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `EMBY_URL` | | 空 | Emby / Jellyfin 地址（用于媒体库查重与入库通知） |
| `EMBY_API_KEY` | 条件 | 空 | 设置了 `EMBY_URL` 时必填 |
| `EMBY_SKIP_TLS_VERIFY` | | `false` | 跳过 Emby TLS 校验（仅信任的自签名证书） |
| `NOTIFICATION_FORMAT` | | `detailed` | 通知格式：`simple` / `detailed` |

### 配额 / 会话

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `MAX_SESSIONS` | | `1000` | 最大并发会话数（1–10000） |
| `MAX_SESSION_AGE` | | `24` | 会话最大存活时长（小时，1–168） |

> 配额额度（电影/剧集每人每日上限）由服务在运行时管理（默认各 2，电影扣 1、剧集扣 3），并可与 MoviePilot 同步。

### 许愿池（`WISH_*`）

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `WISH_RESEARCH_INTERVAL_HOURS` | | `24` | 重搜调度间隔 / 锁定窗口（小时），旧锁超期自动自愈 |
| `WISH_EXPIRE_DAYS` | | `30` | 入池后无源自动过期天数 |
| `WISH_MIN_SEEDERS` | | `1` | 重搜"命中"判定的最低做种数 |
| `WISH_SEARCH_LOCK_TTL_MINUTES` | | `60` | `searching_at` 自愈锁 TTL（分钟） |

### 预产期灯牌（`ETA_*`）

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `ETA_THRESHOLD_HIGH` | | `3` | ⚡"资源充足"档位的站点做种数阈值 |

### AI（可选）

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `AI_ENABLED` | | `false` | 设为 `true` 显示 AI 推荐按钮；兼容旧变量 `ENABLE_AI` |
| `OPENAI_BASE_URL` | | 空 | OpenAI 兼容接口地址（Mimo / OneAPI / NewAPI 等） |
| `OPENAI_API_KEY` | | 空 | OpenAI 兼容接口 Key |
| `OPENAI_MODEL` | | 空 | OpenAI 兼容模型名 |
| `ZHIPU_API_KEY` | | 空 | 智谱 AI API Key |
| `CLAUDE_API_KEY` / `ANTHROPIC_API_KEY` | | 空 | Anthropic/Claude API Key（二选一） |
| `TMDB_API_KEY` | | 内置默认 | TMDB API Key |

### 通知 / 调度

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `DAILY_SUMMARY_HOUR` | | `21` | 每日入库汇总推送时间（0–23） |

### 其他

| 变量 | 必填 | 默认 | 说明 |
| --- | :---: | --- | --- |
| `WEBHOOK_SECRET` | | 空 | 设置后入站 Webhook 须携带有效签名（`X-Webhook-Signature: sha256=<hex>`） |
| `WEBHOOK_URL` | | 空 | 设置后 Bot 使用 Webhook 模式接收 Telegram 更新（否则轮询） |
| `UI_STYLE` | | `card` | 菜单风格：`neon` / `film` / `pop` / `card` / `cinema` |
| `LOG_LEVEL` | | `info` | 日志级别：`debug` / `info` / `warn` / `error` |
| `LOG_PREFIX` | | `YiMao` | 日志前缀 |
| `LOG_COLOR` | | `false` | 彩色日志输出 |
| `ENABLE_RICH_MESSAGE` | | `true` | Telegram Rich Message 开关；设为 `false` 强制使用普通消息 fallback |
| `PUID` / `PGID` | | `0` / `0` | 容器内运行用户 UID/GID（与 `data` 目录权限匹配） |

---

## 📦 部署指南

### 服务端点
- `GET /health`、`GET /api/health`：健康检查
- `POST /webhook`、`/telegram-webhook`：Telegram 更新（Webhook 模式）
- `POST /webhook/emby`、`/webhook/moviepilot`、`/webhook/mp`、`/webhook/jellyseerr`、`/api/summary`：媒体后端入库回调

在 Emby/Jellyfin 与 MoviePilot 中配置对应 Webhook 指向上述地址，YiMao 即可在入库时推送通知、更新剧集进度、触发拼车与许愿出源逻辑。若设置了 `WEBHOOK_SECRET`，回调请求需携带签名。

### 数据持久化
运行数据（配额、管理员、偏好、反馈、许愿池 SQLite 等）保存在 `DATA_DIR`（容器内默认 `/app/data`，已挂载到 `./data`）。升级前请备份该目录。

用户绑定迁移会强制保证一个 MoviePilot 账号只绑定一个 Telegram 用户；如果检测到历史重复绑定，YiMao 会停止启动并在 `DATA_DIR` 生成 `user_mapping_conflicts_*.json`，请管理员处理冲突后再重启，避免静默丢失映射。

### 更新
更新前务必阅读 **[docs/UPDATE.md](docs/UPDATE.md)**，按指引迁移数据后再拉取新镜像 / 重新构建。

---

## 🧱 技术栈与架构

- **语言/运行时**：Go 1.24，单二进制，Docker 多阶段构建（`alpine` 运行）
- **存储**：本地 JSON（配额/管理员/偏好等）+ SQLite（`modernc.org/sqlite`，纯 Go，免 CGO；许愿池、搜索历史等）
- **交互**：Telegram Bot API（轮询 / Webhook 双模式）
- **后端集成**：MoviePilot（搜索、订阅下载、热门榜）、Emby/Jellyfin（媒体库查重、入库回调）、TMDB（元数据）
- **后台任务**：每日推荐（9:00，热门榜）、每日入库汇总（`DAILY_SUMMARY_HOUR`）、每周周报（周一 9:00）、许愿池每分钟错峰重搜与过期清理

更多设计细节见 [`docs/`](docs/)（`ARCHITECTURE.md`、`FEATURES.md`、`COMMANDS.md`、`DEPLOY.md` 等）。

---

## License

[MIT](LICENSE)
