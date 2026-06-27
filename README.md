# YiMao · 云海求片助手

<div align="center">

**Telegram Media Request Bot — Dual-Core System: Search & Subscribe / Challenge & Conquer**

**求片也能闯关⚔️ 通关才给下载 | Hell-Mode Movie Adventure**

[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![Docker Pull](https://img.shields.io/badge/docker-xzb177%2Fyimao-blue?logo=docker)](https://hub.docker.com/r/xzb177/yimao)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

</div>

---

## Overview | 概述

YiMao (云海求片助手) is a Telegram Bot that integrates the complete media request lifecycle — search → request → track → notify — into a single chat interface. Deeply integrated with [MoviePilot](https://github.com/jxxghp/MoviePilot), powered by [TMDB](https://www.themoviedb.org) metadata, with optional [Emby](https://emby.media) / [Jellyfin](https://jellyfin.org) integration for media library checking and import notifications.

**Dual-Core Architecture | 双核心架构**

```
Request a Movie → Two Paths:
  🔍 Normal Request — Search → One-click Subscribe
  ⚔️ Adventure Mode — 5-Level Hell Challenge → Survive → Auto-Submit
```

---

## Core Features | 核心功能

### 🔍 Normal Request | 普通求片
- Send a movie/show name (Chinese or English), bot returns TMDB search results
- Detail view with overview, year, rating, genres, poster
- One-click subscribe with quota checking and duplicate detection
- Series support with season selection

### ⚔️ Adventure Mode | 趣味求片
- **5-Level Hell Challenge**: Trial → Crossroads → Judgment → Sacrifice → Finale
- AI-generated scenes with TMDB deep data (keywords, cast, director, taglines)
- 4 options per level, all look correct — only 1 is right
- Two wrong choices = death. Completion rate <10%
- **Psychology-driven**: Social proof, loss aversion, sunk cost, scarcity, streak system
- Clear it → auto-submit request with **priority processing**
- Leaderboard, daily challenges, reward blind boxes

### 🎮 Game Center | 游戏中心
- Leaderboard (TOP10 rankings)
- Daily Challenge (social comparison mode)
- Reward Blind Box (free after adventure completion)
- Intelligence Station (AI movie narration)
- Personal stats, streak tracking, achievement system

### 📊 Request Tracking | 求片追踪
- `/requests` — unified request view grouped by status
- Subscription status sync from MoviePilot (searching → downloading → completed)
- Episode progress bar in import notifications
- Candidate resource indicators (⚡ abundant / 🔄 waiting / 🐢 no source)

### 🌟 Wish Pool | 许愿池
- `/wish` — add unfindable content to wish pool
- Scheduled re-scan with staggered intervals
- Crowd-counting: same wish from multiple users auto-merged
- Auto-expiry after configurable days

### 🔔 Notification System | 通知
- **Import notifications** — push when download completes and imports to Emby
- **Daily summary** — configurable push time
- **Weekly report** — personal search/request stats, quota usage, hot keywords
- **Per-user notification toggles** — import, daily, weekly, announcements

### 🛡️ Security | 安全
- HMAC-SHA256 webhook signature verification
- Anti-cheat: tried-choice tracking, choice locking, input sanitization
- Session management with configurable limits
- API auth with configurable API keys
- Logger credential sanitization (API keys, tokens, passwords)
- Rate limiting and IP blocking

---

## Commands | 命令

| Command | Description | Access |
|---------|-------------|--------|
| `/start` | Main menu | All |
| `/search` | Search mode | All |
| `/adventure` | Adventure mode (5-level challenge) | All |
| `/game` | Game center | All |
| `/wish` | Wish pool | All |
| `/requests` | My requests | All |
| `/quota` | Check quota | All |
| `/link` | Bind MoviePilot account | All |
| `/portrait` | Soul portrait | All |
| `/help` | Help | All |

---

## Quick Start | 快速开始

### Docker (Recommended)

```bash
cp .env.example .env
vim .env
docker compose up -d
```

Service listens on `:8080` with `/health` endpoint. First user to `/link` becomes admin.

### Environment Variables | 环境变量

| Variable | Default | Description |
|----------|---------|-------------|
| `TELEGRAM_BOT_TOKEN` | — | Bot token from @BotFather |
| `MOVIEPILOT_URL` | `http://localhost:4500` | MoviePilot address |
| `MOVIEPILOT_API_KEY` | — | MoviePilot API Key |
| `EMBY_URL` | — | Emby/Jellyfin URL |
| `EMBY_API_KEY` | — | Emby API Key |
| `TMDB_API_KEY` | — | TMDB API Key (v3 auth) |
| `OPENAI_API_KEY` | — | AI provider key (for adventure scenes) |
| `OPENAI_BASE_URL` | — | AI provider base URL |
| `OPENAI_MODEL` | — | AI model name |
| `WEBHOOK_SECRET` | — | HMAC-SHA256 secret for inbound webhooks |
| `TZ` | `Asia/Shanghai` | Timezone |

Full config: [`.env.example`](.env.example)

---

## Webhook Endpoints

| Path | Purpose |
|------|---------|
| `POST /webhook/emby` | Emby import callbacks |
| `POST /webhook/moviepilot` | MoviePilot callbacks |
| `POST /api/summary` | Manual daily summary trigger |
| `GET /health` | Health check |

When `WEBHOOK_SECRET` is set, requests must include `?token=<secret>` or `X-Webhook-Signature: sha256=<hex>`.

---

## Tech Stack | 技术栈

| Layer | Choice |
|-------|--------|
| Language | Go 1.24 |
| Distribution | Single binary, Docker multi-stage (Alpine) |
| Storage | JSON files + SQLite (wish pool, search history, user mapping, social data) |
| Telegram | Bot API (polling/webhook dual mode) |
| Backend | MoviePilot, Emby/Jellyfin, TMDB, OpenAI-compatible AI |

---

## License

[MIT](LICENSE)
