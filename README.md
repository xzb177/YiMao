# YiMao

<div align="center">

**Telegram 影视求片机器人 —— 搜片 → 求片 → 跟踪进度 → 收通知，完整闭环**

集成 MoviePilot + Emby/Jellyfin，全流程自动化管理

[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![Docker Pull](https://img.shields.io/badge/docker-xzb177%2Fyimao-blue?logo=docker)](https://hub.docker.com/r/xzb177/yimao)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![CI](https://github.com/xzb177/YiMao/actions/workflows/ci.yml/badge.svg)](https://github.com/xzb177/YiMao/actions/workflows/ci.yml)

</div>

---

## 概述

YiMao 是一个 Telegram Bot，将媒体资源请求的完整流程——搜索、提交、审核、下载、通知——整合到单一的聊天界面中。它与 [MoviePilot](https://github.com/jxxghp/MoviePilot) 深度集成，利用 [TMDB](https://www.themoviedb.org) 提供元数据，并可选接入 [Emby](https://emby.media) / [Jellyfin](https://jellyfin.org) 实现媒体库存在性检查与入库回调通知。

---

## 核心功能

### 智能搜索与详情展示
- 发送影片名称（中英文均可），Bot 返回 TMDB 搜索结果
- 详情页展示简介、年份、评分、类型、海报等元数据
- 剧集额外展示季数信息，支持按季发起订阅

### 请求流程
- **一键求片**：详情页发起请求，自动校验绑定状态与配额
- **审核机制**：管理员审核队列，支持批准 / 拒绝操作，审核结果通过 Rich Message 推送
- **配额管理**：每日配额（默认电影 2 部 / 剧集 2 部），次日 00:00 自动重置
- **去重保护**：自动检测媒体库已存在资源、重复请求，支持强制订阅
- **失败兜底**：审核通过后 MoviePilot 提交失败时标记为 stuck，保留可重试

### 状态跟踪
- **请求聚合视图**：`/requests` 展示用户所有订阅请求，按状态分组（进行中 / 已完成 / 异常）
- **订阅状态**：从 MoviePilot 同步，覆盖等待搜索 → 下载中 → 已完成全链路
- **剧集进度条**：入库通知中显示集数进度（如 S03E07/S03E16），支持本季 / 全剧口径
- **候选资源灯牌**：基于站点做种情况给出预期（⚡资源充足 / 🔄等待做种 / 🐢暂无出源 / ❓数据不足）

### 许愿池
- 搜不到的影片可通过 `/wish` 加入许愿池
- 定时重搜：调度器每天错峰扫描，命中后主动私信通知
- 众筹计数：同一影片多人许愿自动合并，出源后通知所有许愿用户
- 超期自动清理：超过 `WISH_EXPIRE_DAYS`（默认 30 天）自动取消

### 通知系统
- **入库通知**：下载完成入库时推送，支持图文消息
- **每日汇总**：当日入库影片汇总，推送时间可配置（`DAILY_SUMMARY_HOUR`）
- **观影周报**：每周推送个人搜索/求片统计、配额使用、热搜关键词与类型偏好
- **通知开关**：用户可单独控制入库通知 / 每日推荐 / 周报 / 公告的推送

### 辅助功能
- **拼车 +1**：详情页标记「我也想看」，到货时群内 @ 通知
- **密码重置**：通过 `/resetpw` 重置 MoviePilot 密码（需额外配置 Docker socket）
- **账户绑定**：自动创建或手动绑定 MoviePilot 账号，支持密码验证

---

## 命令列表

| 命令 | 说明 | 权限 |
|------|------|------|
| `/start` | 打开主菜单 | 所有人 |
| `/search` | 进入搜索模式 | 所有人 |
| `/wish [影片名称]` | 许愿入池 / 查看我的许愿 | 所有人 |
| `/requests` | 我的请求聚合视图 | 所有人 |
| `/watchlist` | 同 `/requests` | 所有人 |
| `/quota` | 查看今日配额 | 所有人 |
| `/link <用户名>` | 创建并绑定 MoviePilot 账号 | 所有人 |
| `/link <用户名> <密码>` | 绑定已有 MoviePilot 账号（需密码验证） | 所有人 |
| `/resetpw` | 重置自己已绑定的 MoviePilot 密码 | 所有人 |
| `/resetpw <用户名>` | 重置指定用户的 MoviePilot 密码 | 管理员 |
| `/status` | 查看运行状态与部署诊断 | 所有人 |
| `/id` | 查看当前聊天 / 用户 ID | 所有人 |
| `/help` | 帮助信息 | 所有人 |

> 管理操作（审核、配额管理、管理员管理等）通过主菜单「🛠️ 管理」入口操作，仅管理员可见。

---

## 快速开始

### Docker 部署（推荐）

```bash
# 准备配置
cp .env.example .env
vim .env

# 启动
docker compose up -d

# 查看日志
docker compose logs -f
```

> `docker-compose.yml` 使用 `network_mode: host`，数据持久化到 `./data`。默认不挂载 Docker socket；如需密码重置功能，请叠加 `docker-compose.resetpw.yml`：
>
> ```bash
> docker compose -f docker-compose.yml -f docker-compose.resetpw.yml up -d
> ```

### 本地构建

```bash
# 需要 Go 1.24
go build -o yimao ./cmd/bot
export $(grep -v '^#' .env | xargs)
./yimao
```

服务默认监听 `:8080`，提供 `/health` 健康检查端点。第一个绑定的用户自动成为管理员，也可通过 `ADMIN_USER_IDS` 预先指定。

---

## 配置参考

### 必需配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TELEGRAM_BOT_TOKEN` | — | 从 @BotFather 获取的 Bot Token |
| `MOVIEPILOT_URL` | `http://localhost:4500` | MoviePilot 地址 |
| `MOVIEPILOT_API_KEY` | — | MoviePilot API Key |
| `ADMIN_USER_IDS` | 空 | 管理员 Telegram ID（逗号分隔）；留空则首个 `/link` 用户成为管理员 |

### 媒体库集成

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `EMBY_URL` | 空 | Emby / Jellyfin 地址 |
| `EMBY_API_KEY` | 空 | Emby API Key（设置了 `EMBY_URL` 时必填） |
| `EMBY_SKIP_TLS_VERIFY` | `false` | 跳过 Emby TLS 证书校验 |
| `TMDB_API_KEY` | 内置默认值 | TMDB API Key |

### MoviePilot 集成

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DOWNLOAD_SAVE_PATH` | 空 | 自定义下载目录，多实例共用时区分 |
| `MOVIEPILOT_CONTAINER` | `moviepilot-v2` | 密码重置用的容器名（需 `docker-compose.resetpw.yml`） |
| `MOVIEPILOT_DB_PATH` | 空 | 密码重置用的容器内 user.db 路径 |

### 通知与调度

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DAILY_SUMMARY_HOUR` | `21` | 每日入库汇总推送时间（0–23） |
| `WEBHOOK_SECRET` | 空 | 入站 Webhook 签名密钥（HMAC-SHA256） |
| `WEBHOOK_URL` | 空 | 设置后使用 Webhook 模式（否则轮询） |
| `NOTIFICATION_FORMAT` | `detailed` | 通知格式：`simple` / `detailed` |
| `ENABLE_RICH_MESSAGE` | `true` | Rich Message 开关；关闭后降级为普通消息 |

### 服务配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8080` | 服务监听端口 |
| `TZ` | `Asia/Shanghai` | 时区 |
| `ENABLE_API_AUTH` | `true` | HTTP API 鉴权开关 |
| `UI_STYLE` | `card` | 菜单风格：`neon` / `film` / `pop` / `card` / `cinema` |
| `LOG_LEVEL` | `info` | 日志级别：`debug` / `info` / `warn` / `error` |
| `PUID` / `PGID` | `0` / `0` | 容器运行用户 UID/GID |

### 许愿池

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `WISH_RESEARCH_INTERVAL_HOURS` | `24` | 重搜调度间隔（小时） |
| `WISH_EXPIRE_DAYS` | `30` | 许愿无源自动过期天数 |
| `WISH_MIN_SEEDERS` | `1` | 命中判定最低做种数 |
| `WISH_SEARCH_LOCK_TTL_MINUTES` | `60` | 搜索锁 TTL（分钟） |

### 配额

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `MAX_SESSIONS` | `1000` | 最大并发会话数 |
| `MAX_SESSION_AGE` | `24` | 会话最大存活时长（小时） |

完整配置示例见 [`.env.example`](.env.example)。

---

## 部署指南

### 服务端点

| 路径 | 用途 |
|------|------|
| `GET /health`、`GET /api/health` | 健康检查 |
| `POST /webhook`、`/telegram-webhook` | Telegram 更新（Webhook 模式） |
| `POST /webhook/emby` | Emby 入库回调 |
| `POST /webhook/jellyseerr` | Jellyseerr 回调 |
| `POST /webhook/moviepilot`、`/webhook/mp` | MoviePilot 回调 |
| `POST /api/summary` | 手动触发入库汇总 |

Emby/Jellyfin 与 MoviePilot 配置 Webhook 指向上述地址后，YiMao 自动推送入库通知、更新剧集进度、触发拼车与许愿出源逻辑。若设置了 `WEBHOOK_SECRET`，回调请求需携带 `X-Webhook-Signature: sha256=<hex>` 签名头。

### 数据持久化

运行数据（配额、管理员、偏好、审核请求、许愿池 SQLite 等）保存在 `DATA_DIR`（容器内默认 `/app/data`，映射到 `./data`）。升级前请备份该目录。

> 用户绑定迁移确保一个 MoviePilot 账号只绑定一个 Telegram 用户。检测到历史重复绑定时，Bot 会停止启动并生成冲突报告 `user_mapping_conflicts_*.json`，供管理员处理。

### 更新

更新前务必阅读 **[docs/UPDATE.md](docs/UPDATE.md)**，按指引迁移数据后拉取新镜像或重新构建。

---

## 技术架构

| 组件 | 选型 |
|------|------|
| **语言** | Go 1.24 |
| **分发** | 单二进制，Docker 多阶段构建（基于 Alpine） |
| **存储** | JSON 文件（配额/管理员/偏好） + SQLite（许愿池/搜索历史/用户映射） |
| **Telegram** | Bot API（轮询 / Webhook 双模式） |
| **后端集成** | MoviePilot（搜索、订阅、下载）、Emby/Jellyfin（查重、入库通知）、TMDB（元数据） |
| **后台任务** | 每日入库汇总、每周周报、许愿池定时重搜 |

详细设计文档见 [`docs/`](docs/) 目录。

---

## License

[MIT](LICENSE)
