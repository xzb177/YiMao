# YiMao · 云海求片助手

<div align="center">

**Telegram media task center for search, requests, progress, safe upgrades, and playback-ready notifications, with an App-first Mini App and MoviePilot + Emby/Jellyfin integration.**

[中文](README.md) · [![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org) [![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com) [![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

</div>

---

## Overview

YiMao (云海求片助手) is a Telegram Bot that integrates the complete media request lifecycle — search → subscribe → track → notify — into a single chat interface. Deeply integrated with [MoviePilot](https://github.com/jxxghp/MoviePilot), powered by [TMDB](https://www.themoviedb.org) metadata, with optional [Emby](https://emby.media) / [Jellyfin](https://jellyfin.org) integration for media library checking and import notifications.

**Clear Product Hierarchy**

```text
Default path:
  🔍 Search & Request — Search → One-click Subscribe → Track → Notify

Mini App:
  Home → Search/detail → Request or wash → Task timeline
```

Search & Request remains the default Bot path. The Mini App opens directly on the real task home.

---

## Core Features

### 🔍 Search & Request
- Send a movie/show name (Chinese or English), bot returns TMDB search results
- Detail view with overview, year, rating, genres, poster
- One-click subscribe with quota checking and duplicate detection
- Series support with season selection

### Mini App Task Center
- App-first home with ready-to-watch items, blockers, active work, request, and wash shortcuts
- Search/detail, season selection, request results, task progress, cancellation, watchlist, wishes, and issue reporting
- Native dialogs, safe-area support, a stable three-column mobile dock, request race guards, and recoverable error states

### 🎮 Game Center
- Intelligence Station (AI movie narration)
- Movie blind box and destiny roulette
- Viewing profile

Roulette remains available through both its entry and spin actions.

### 📊 Request Tracking
- `/requests` — unified request view grouped by status
- Subscription status sync from MoviePilot (searching → downloading → completed)
- Episode progress bar in import notifications
- Candidate resource indicators (⚡ abundant / 🔄 waiting / 🐢 no source)
- Request and wash tasks share one timeline; pending tasks can be cancelled by their owner
- Watchlist/carpool arrivals, issue feedback, and per-user completion notifications remain isolated by user

### Administrator Workflow
- Review request and wash work orders, handle feedback, and manage notification settings
- Claim/release wash work, retain the old version, and verify a distinct new Emby MediaSource before completion
- Approved wash details use a short Telegram callback and an explicit completion confirmation; viewing details never completes the work order

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
- Telegram Mini App `initData` authentication
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
| `/game` | Game center | All |
| `/wish` | Wish pool | All |
| `/requests` | Request progress | All |
| `/watchlist` | Same request-progress view | All |
| `/quota` | Check quota | All |
| `/link` | Bind MoviePilot account | All |
| `/unlink` | Confirm and remove the account binding | Private/direct |
| `/resetpw` | Reset the bound account password | Private/direct |
| `/portrait` | Viewing profile | All |
| `/narrate` | AI movie narration | Private menu |
| `/review` | Write a movie review | Private menu |
| `/status` | Bot status and redacted admin diagnostics | Private/direct |
| `/id` | Current chat and user IDs | Direct |
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
| `MOVIEPILOT_URL` | — | Required MoviePilot address; set the deployment URL explicitly |
| `MOVIEPILOT_API_KEY` | — | MoviePilot API Key |
| `EMBY_URL` | — | Emby/Jellyfin URL |
| `EMBY_API_KEY` | — | Emby API Key |
| `TMDB_API_KEY` | — | TMDB API Key (v3 auth) |
| `OPENAI_API_KEY` | — | AI provider key for movie narration and the optional assistant |
| `OPENAI_BASE_URL` | — | AI provider base URL |
| `OPENAI_MODEL` | — | AI model name |
| `MINI_APP_URL` | — | Public HTTPS Mini App base URL used by Telegram menus and Bot buttons; credentials, query parameters and fragments are rejected |

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
