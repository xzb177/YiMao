# YiMao - Emby Telegram Bot

> 🤖 智能影视 Telegram 机器人 - 支持搜索求片、实时通知、AI 推荐

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

## ✨ 功能特性

| 类别 | 功能 |
|------|------|
| 🔍 **智能求片** | 搜索媒体、一键请求、配额管理、状态跟踪 |
| 📬 **入库推送** | 新内容入库通知，带横屏海报和详细信息 |
| 🔔 **请求通知** | 批准/拒绝/可用状态自动通知 |
| 🐛 **问题反馈** | 音频/视频/字幕问题报告，管理员快捷回复 |
| 👥 **账号管理** | 账号绑定、配额系统、权限控制 |
| 🤖 **AI 推荐** | 热门电影、热播剧集、最新上映、智能推荐 |
| 💬 **智能对话** | 自然语言处理，支持中英文交互 |

---

## 📋 命令列表

### 基础命令
| 命令 | 说明 |
|------|------|
| `/start` | 开始使用 |
| `/help` | 帮助 |
| `/search <关键词>` | 搜索媒体 |
| `/recommend` | 智能推荐 |
| `/trending` | 热门推荐 |
| `/my` | 我的请求 |

### 账号管理
| 命令 | 说明 |
|------|------|
| `/link 账号 密码` | 绑定账号 |
| `/unlink` | 解绑账号 |
| `/quota` | 查看配额 |
| `/prefs` | 通知设置 |

### 管理员
| 命令 | 说明 |
|------|------|
| `/pending` | 待处理请求 |
| `/approve <ID>` | 批准请求 |
| `/decline <ID>` | 拒绝请求 |
| `/users` | 用户列表 |
| `/stats` | 统计数据 |

---

## 🚀 快速开始

### Docker 部署（推荐）

```bash
# 克隆仓库
git clone https://github.com/xzb177/YiMao.git
cd YiMao

# 配置环境变量
cp .env.example .env
nano .env

# 启动容器
docker compose up -d
```

### 本地运行

```bash
# 配置环境变量
cp .env.example .env
nano .env

# 编译运行
go build
./emby-telegram-bot
```

---

## 📸 功能预览

### 电影入库通知
```
✅ 入库成功：不是你不爱你 (2023)
───────────────────

🎬 名称：不是你不爱你 (2023)

🏷️ 类别：华语电影

💎 质量： BluRay 1080p

📦 总大小：8.0G

📁 文件数量：1 个
```

### 剧集入库通知（整季）
```
✅ 入库成功：恒久定律 (2024) S01 E01-E10
───────────────────

🎬 名称：恒久定律 (2024) S01 E01-E10

🏷️ 类别：国产剧

💎 质量： WEB-DL 1080p

📦 总大小：5.81G

📁 文件数量：10 个
```

### 单集动态通知
```
✅ 新增第5集 三体 S01E05
───────────────────

🎬 名称：三体 S01 S01E05

📊 当前进度：共5集

💎 质量：WEB-DL 1080p

📦 总大小：1.2G

📁 文件数量：1 个
```

### 请求批准通知
```
╔══════════════════════════════════╗
║     ✅ 请求已批准 · Approved        ║
╚══════════════════════════════════╝

📦 三体
👤 用户A 请求
─────────────────────────────────
🎬 正在处理中，请耐心等待~
```

### AI 推荐
```
🔥 热门推荐
───────────────────
1. 沙丘2 (2024) ⭐8.5
2. 奥本海默 (2023) ⭐8.9
3. 流浪地球3 (2024) ⭐8.0
```

---

## 🔧 环境要求

- Go 1.23+ (本地运行)
- Docker & Docker Compose (容器部署)
- 媒体库服务器 (Emby/Jellyfin)
- 请求系统 (Jellyseerr)
- Telegram Bot Token

---

## 📝 配置说明

主要环境变量：

| 变量 | 说明 | 必需 |
|------|------|------|
| `TELEGRAM_BOT_TOKEN` | Bot Token | ✅ |
| `TELEGRAM_CHAT_ID` | 群组 ID | ✅ |
| `EMBY_URL` | 媒体库地址 | ✅ |
| `EMBY_API_KEY` | 媒体库 API Key | ✅ |
| `JELLYSEERR_URL` | 请求系统地址 | ✅ |
| `JELLYSEERR_API_KEY` | 请求系统 API Key | ✅ |
| `ZHIPU_API_KEY` | 智能 AI Key | ❌ |
| `ADMINS` | 管理员 ID 列表 | ❌ |

---

## 📄 开源协议

MIT License

---

## 🔗 相关链接

- GitHub: [xzb177/YiMao](https://github.com/xzb177/YiMao)
