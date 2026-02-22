# YiMao - Emby Telegram Bot

> 🤖 企业级智能影视 Telegram 机器人 - 支持搜索求片、媒体库通知、AI推荐、审核工作流

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

## ✨ 功能特性

| 类别 | 功能 |
|------|------|
| 🔍 **智能求片** | 搜索媒体、一键请求、配额管理、状态跟踪 |
| 📬 **媒体库通知** | 支持单集推送/每日汇总两种模式，管理员可配置 |
| 🔔 **请求通知** | 批准/拒绝/可用状态自动通知 |
| 🐛 **问题反馈** | 音频/视频/字幕问题报告，管理员快捷回复 |
| 👥 **账号管理** | 账号绑定、配额系统、权限控制 |
| 🤖 **AI 推荐** | 热门电影、热播剧集、最新上映、智能推荐 |
| 💬 **智能对话** | 自然语言处理，支持中英文交互 |
| ✅ **审核工作流** | 管理员可审核用户请求，支持批准/拒绝操作 |

---

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                        Telegram Bot API                     │
├─────────────────────────────────────────────────────────────┤
│                     Handlers Layer                         │
│  ┌───────────────┐  ┌──────────────────┐  ┌─────────────┐   │
│  │ Start Handler  │  │   Search Handler │  │  AI Handler │   │
│  │                │  │                  │  │              │   │
│  │ - Admin Menu    │  │ - Search Results  │  │ - Trending   │   │
│  │ - Notif Settings│  │ - Request Media  │  │ - Hot        │   │
│  └───────────────┘  └──────────────────┘  └─────────────┘   │
├─────────────────────────────────────────────────────────────┤
│                     Services Layer                          │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ │ - TelegramClient  │ - MoviePilotClient│  │
│  │ │ - AdminService   │ - UserMapping    │  │
│  │ │ - QuotaService   │ - ReviewService  │  │
│  │ │ - ChatService    │ - WebhookService  │  │
│  │ │ - MediaNotifySvc  │ - TMDBClient     │  │
│  │ └─────────────────────────────────────────────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                     Core Layer                                │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ - Session Manager  -  - Callback Registry - Middleware     │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## 📋 命令列表

### 基础命令
| 命令 | 说明 |
|------|------|
| `/start` | 开始使用 |
| `/help` | 帮助 |
| `/search <关键词>` | 搜索媒体 |

### 账号管理
| 命令 | 说明 |
|------|------|
| `/link 账号 密码` | 绑定 MoviePilot 账号 |
| `/unlink` | 解绑账号 |

### 管理员功能
| 功能 | 说明 |
|------|------|
| 🔧 管理员菜单 | 通过私聊访问管理功能 |
| 📱 通知设置 | 配置媒体库通知模式（单集/每日汇总）|
| 📊 审核请求 | 批准/拒绝用户求片请求 |

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

### 环境变量配置

| 变量 | 说明 | 必需 |
|------|------|------|
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | ✅ |
| `MOVIEPILOT_URL` | MoviePilot 地址 | ✅ |
| `MOVIEPILOT_API_KEY` | MoviePilot API Key | ✅ |
| `EMBY_URL` | 媒体库地址 | ❌ |
| `EMBY_API_KEY` | 媒体库 API Key | ❌ |
| `TMDB_API_KEY` | TMDB API Key | ❌ |
| `ZHIPU_API_KEY` | 智谱 AI Key | ❌ |
| `ADMINS` | 管理员 ID 列表 | ❌ |

---

## 🔔 媒体库通知功能

管理员可通过私聊中的 `🔧 管理员菜单` → `🔔 通知设置` 配置：

### ⚡ 单集推送模式
每次有新内容入库时立即发送通知，包含：
- 媒体标题和年份
- 媒体类型（电影/剧集/动画）
- 入库时间
- 画质量和评分

### 📅 每日汇总模式
在设定的时间汇总当天所有入库内容，格式如下：

```
📅 2026-02-22 总入库目录

├─ 🎬 动画库
│   ├─ 鬼灭之刃 刀匠村篇 EP01-EP12（完结）
│   └─ 我推的孩子 EP01
│
├─ 📺 剧集库
│   ├─ 隧道 第一季 S01E01-S01E06
│   └─ 黑暗荣耀 第一季 S01E01-S01E08
│
└─ 🎥 电影库
    ├─ 铃芽之旅
    └─ 你的名字。

入库总览：
动画：2 部
剧集：2 部
电影：2 部
```

---

## 📸 功能预览

### 媒体库通知

**单集推送：**
```
✅ 入库成功：不是你不爱你 (2023)

🎬 名称：不是你不爱你 (2023)
🏷️ 类别：华语电影
💎 质量：4K
⭐ 评分：8.5
🕒 入库时间：22:30
```

**每日汇总：**
```
📅 2026-02-22 总入库目录

├─ 🎬 动画库
│   ├─ 鬼灭之刃 刀匠村篇 EP01-EP12（完结）
│   └─ 我推的孩子 EP01

入库总览：
动画：2 部
剧集：2 部
电影：2 部
```

---

## 📄 开源协议

MIT License

---

## 🔗 相关链接

- GitHub: [xzb177/YiMao](https://github.com/xzb177/YiMao)
- MoviePilot: [MoviePilot](https://github.com/jxxghp/MoviePilot)
- Emby: [Emby](https://emby.media/)
