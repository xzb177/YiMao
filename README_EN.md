# YiMao · 云海求片助手

<div align="center">

**Telegram media request assistant focused on search, subscription, progress tracking, and library notifications, with an optional five-stage movie adventure. Deep MoviePilot + Emby/Jellyfin integration.**

[中文](README.md) · [![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org) [![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com) [![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

</div>

---

## Overview

YiMao (云海求片助手) is a Telegram Bot that integrates the complete media request lifecycle — search → subscribe → track → notify — into a single chat interface. Deeply integrated with [MoviePilot](https://github.com/jxxghp/MoviePilot), powered by [TMDB](https://www.themoviedb.org) metadata, with optional [Emby](https://emby.media) / [Jellyfin](https://jellyfin.org) integration for media library checking and import notifications.

**Clear Product Hierarchy**

```text
Default path:
  🔍 Search & Request — Search → One-click Subscribe → Track → Notify

Optional activity:
  ⚔️ Movie Adventure — Five interactive stages → Auto-submit after completion
```

Search & Request is always available and never requires playing the adventure.

---

## Core Features

### 🔍 Search & Request
- Send a movie/show name (Chinese or English), bot returns TMDB search results
- Detail view with overview, year, rating, genres, poster
- One-click subscribe with quota checking and duplicate detection
- Series support with season selection

### ⚔️ Movie Adventure (Optional)
- Five interactive stages: Discovery → Turning Point → Conflict → Choice → Finale
- AI-generated scenes grounded in TMDB metadata (keywords, cast, director, taglines)
- Four close options per stage, judged through plot and character clues
- Keeps HP, combos, ratings, and progress records
- Completion automatically submits the request; direct search remains available at any time
- Leaderboard, daily challenges, and reward blind boxes

### 🎮 Game Center
- Adventure Leaderboard (TOP10 rankings)
- Daily Challenge (one shared movie prompt per day)
- Reward Blind Box (free after adventure completion)
- Intelligence Station (AI movie narration)
- Personal stats, streak tracking, achievement system

### 📊 Request Tracking
- `/requests` — unified request view grouped by status
- Subscription status sync from MoviePilot (searching → downloading → completed)
- Episode progress bar in import notifications
- Candidate resource indicators (⚡ abundant / 🔄 waiting / 🐢 no source)

### 🌟 Wish Pool
- `/wish` — add unfindable content to wish pool
- Scheduled re-scan with staggered intervals
- Crowd-counting: same wish from multiple users auto-merged
- Auto-expiry after configurable days

### 🔔 Notification System
- **Import notifications** — push when download completes and imports to Emby
- **Daily summary** — configurable push time
- **Weekly report** — personal search/request stats, quota usage, hot keywords
- **Per-user notification toggles** — import, daily, weekly, announcements

### 🛡️ Security
- HMAC-SHA256 webhook signature verification
- Anti-cheat: tried-choice tracking, choice locking, input sanitization
- Session management with configurable limits
- API auth with configurable API keys
- Logger credential sanitization (API keys, tokens, passwords)
- Rate limiting and IP blocking

---

## Commands

| Command | Description | Access |
|---------|-------------|--------|
| `/start` | Main menu | All |
| `/search` | Search & Request | All |
| `/adventure` | Movie Adventure (five stages) | All |
| `/game` | Game center | All |
| `/wish` | Wish pool | All |
| `/requests` | Request progress | All |
| `/quota` | Check quota | All |
| `/link` | Bind MoviePilot account | All |
| `/portrait` | Viewing profile | All |
| `/help` | Help | All |

---

## Quick Start

### Docker (Recommended)

```bash
git clone https://github.com/xzb177/YiMao.git /opt/YiMao
cd /opt/YiMao
./install.sh
# Fill the generated .env, then run:
./manage.sh install
```

The service listens on `:8080`; `/health` is the health endpoint. New deployments must set `ADMIN_USER_IDS`; its first numeric ID becomes root administrator. `/link` only binds a MoviePilot account and never grants administrator privileges. `API_KEYS` is required while API authentication is enabled.

### Environment Variables

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
| `ENABLE_API_AUTH` | `true` | Enables HTTP API key authentication |
| `API_KEYS` | — | JSON object of HTTP API keys; required when auth is enabled |
| `ADMIN_USER_IDS` | — | Required comma-separated Telegram administrator IDs; first ID is root |
| `TZ` | `Asia/Shanghai` | Timezone |

Full config: [`.env.example`](.env.example)

Before release, run the isolated [staging and device acceptance workflow](docs/STAGING.md) with a test bot and test MoviePilot instance, then record evidence in the [RC acceptance report](docs/RC_ACCEPTANCE_TEMPLATE.md).

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

## Tech Stack

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
