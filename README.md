# YiMao · 云海求片助手

<div align="center">

**Telegram 影视求片助手 — 以搜索、订阅、追踪与入库通知为核心，并提供可选的电影互动玩法。深度集成 MoviePilot + Emby/Jellyfin。**

[English](README_EN.md) · [![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org) [![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com) [![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

</div>

---

## 概述

云海求片助手 (YiMao) 是一款 Telegram Bot，将影视求片全流程——搜索 → 订阅 → 追踪 → 通知——整合到单一聊天界面。深度集成 [MoviePilot](https://github.com/jxxghp/MoviePilot)，基于 [TMDB](https://www.themoviedb.org) 元数据，可选集成 [Emby](https://emby.media) / [Jellyfin](https://jellyfin.org) 实现媒体库检查和入库通知。

**清晰的主次结构**

```text
默认路径：
  🔍 搜索求片 — 搜索 → 一键订阅 → 进度追踪 → 入库通知

可选玩法：
  ⚔️ 电影冒险 — 五关电影互动 → 通关后自动提交求片
```

搜索求片始终可以直接使用，无需参与互动玩法。

---

## 核心功能

### 🔍 搜索求片
- 发送影片名称（中英文均可），Bot 返回 TMDB 搜索结果
- 详情页展示简介、年份、评分、类型、海报
- 一键订阅，含配额检查和重复检测
- 剧集支持季选择

### ⚔️ 电影冒险（可选）
- 五关电影互动：发现 → 转折 → 冲突 → 抉择 → 终章
- AI 生成场景，融入 TMDB 深度数据（关键词、演员、导演、标语）
- 每关 4 个相近选项，通过剧情与人物线索作出判断
- 保留生命值、连击、评分与进度记录
- 通关后自动提交求片；未通关也可返回主菜单直接使用搜索求片
- 排行榜、每日挑战与奖励盲盒

### 🎮 游戏中心
- 冒险排行（TOP10 排名）
- 每日挑战（每天一部共同片单）
- 奖励盲盒（通关后免费抽取）
- 情报站（AI 影视解说）
- 个人统计、连击追踪、成就系统

### 📊 求片追踪
- `/requests` — 按状态分组的统一求片视图
- 订阅状态同步（MoviePilot：搜索中 → 下载中 → 已完成）
- 入库通知含剧集进度条
- 候选资源指示（⚡ 资源充足 / 🔄 等待中 / 🐢 暂无源）

### 🌟 许愿池
- `/wish` — 将找不到的资源加入许愿池
- 定时重搜，交错间隔
- 众筹计数：多人许愿同一内容自动合并
- 超期自动过期

### 🔔 通知系统
- **入库通知** — 下载完成入库 Emby 时推送
- **每日汇总** — 可配置推送时间
- **周报** — 个人搜片/求片统计、配额使用、热词
- **按用户通知开关** — 入库、每日、每周、公告

### 🛡️ 安全
- HMAC-SHA256 Webhook 签名验证
- 防作弊：已试选项追踪、选项锁定、输入净化
- 会话管理，可配置上限
- API 鉴权，可配置 API Key
- 日志脱敏（API Key、Token、密码）
- 频率限制和 IP 封禁

---

## 命令

| 命令 | 说明 | 权限 |
|---------|-------------|--------|
| `/start` | 主菜单 | 所有人 |
| `/search` | 搜索求片 | 所有人 |
| `/adventure` | 电影冒险（五关互动） | 所有人 |
| `/game` | 游戏中心 | 所有人 |
| `/wish` | 许愿池 | 所有人 |
| `/requests` | 求片进度 | 所有人 |
| `/quota` | 查看配额 | 所有人 |
| `/link` | 绑定 MoviePilot 账号 | 所有人 |
| `/portrait` | 观影画像 | 所有人 |
| `/help` | 帮助 | 所有人 |

---

## 快速开始

### Docker（推荐）

```bash
cp .env.example .env
vim .env
./scripts/preflight.sh --env  # 只检查，不启动、不重启服务
docker compose up -d
curl -fsS http://localhost:8080/health
```

服务监听 `:8080` 端口，`/health` 为健康检查端点。首次部署必须填写 `API_KEYS`（JSON 对象，每个 Key 至少 16 个字符）；首个执行 `/link` 的用户会自动成为管理员。

### 环境变量

| 变量 | 默认值 | 说明 |
|----------|---------|-------------|
| `TELEGRAM_BOT_TOKEN` | — | 从 @BotFather 获取的 Bot Token |
| `MOVIEPILOT_URL` | `http://localhost:4500` | MoviePilot 地址 |
| `MOVIEPILOT_API_KEY` | — | MoviePilot API Key |
| `EMBY_URL` | — | Emby/Jellyfin 地址 |
| `EMBY_API_KEY` | — | Emby API Key |
| `TMDB_API_KEY` | — | TMDB API Key（v3 认证） |
| `OPENAI_API_KEY` | — | AI 提供商 Key（电影冒险场景生成） |
| `OPENAI_BASE_URL` | — | AI 提供商 Base URL |
| `OPENAI_MODEL` | — | AI 模型名称 |
| `WEBHOOK_SECRET` | — | 入站 Webhook 的 HMAC-SHA256 密钥 |
| `ENABLE_API_AUTH` | `true` | 是否启用 HTTP API Key 鉴权 |
| `API_KEYS` | — | HTTP API Key JSON 对象；鉴权开启时必填 |
| `ADMIN_USER_IDS` | — | 可选管理员 Telegram ID，逗号分隔 |
| `TZ` | `Asia/Shanghai` | 时区 |

完整配置：[`.env.example`](.env.example)

---

## Webhook 端点

| 路径 | 用途 |
|------|---------|
| `POST /webhook/emby` | Emby 入库回调 |
| `POST /webhook/moviepilot` | MoviePilot 回调 |
| `POST /api/summary` | 手动触发每日汇总 |
| `GET /health` | 健康检查 |

设置 `WEBHOOK_SECRET` 后，请求须携带 `?token=<secret>` 或 `X-Webhook-Signature: sha256=<hex>`。

---

## 技术栈

| 层级 | 选择 |
|-------|--------|
| 语言 | Go 1.24 |
| 分发 | 单二进制 + Docker 多阶段构建（Alpine） |
| 存储 | JSON 文件 + SQLite（许愿池、搜索历史、用户映射、社交数据） |
| Telegram | Bot API（polling/webhook 双模式） |
| 后端 | MoviePilot、Emby/Jellyfin、TMDB、OpenAI 兼容 AI |

---

## License

[MIT](LICENSE)
