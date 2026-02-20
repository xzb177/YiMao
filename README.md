# Emby Telegram Bot

> Jellyfin/Emby 媒体库 Telegram 机器人，支持智能求片、实时通知、入库推送等功能。

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

## 功能特性

| 类别 | 功能 |
|------|------|
| **智能求片** | 搜索媒体、一键请求、配额管理、状态跟踪 |
| **入库推送** | 新内容入库通知，带横屏海报和详细信息 |
| **请求通知** | 批准/拒绝/可用状态自动通知 |
| **问题反馈** | 用户问题报告与管理员回复系统 |
| **账号管理** | Jellyfin 账号绑定、配额系统、权限控制 |
| **统计分析** | 热门排行、用户活跃度、请求趋势 |
| **AI 推荐** | 热门电影、热播剧集、最新上映、随机推荐 |

---

## 命令列表

### 基础命令
| 命令 | 说明 |
|------|------|
| `/start` | 开始使用 |
| `/help` | 帮助 |
| `/search <关键词>` | 搜索媒体 |
| `/my` | 我的请求 |
| `/status` | 我的状态 |

### 账号管理
| 命令 | 说明 |
|------|------|
| `/link 账号 密码` | 绑定 Jellyfin 账号 |
| `/unlink` | 解绑账号 |
| `/quota` | 查看配额 |

### 管理员
| 命令 | 说明 |
|------|------|
| `/pending` | 待处理请求 |
| `/approve <ID>` | 批准请求 |
| `/decline <ID>` | 拒绝请求 |
| `/users` | 用户列表 |
| `/stats` | 统计数据 |

---

## 快速开始

### 环境要求
- Go 1.21+
- Jellyfin/Emby 媒体库
- Jellyseerr 请求系统
- Telegram Bot Token

### 安装

```bash
# 克隆仓库
git clone https://github.com/xzb177/YiMao.git
cd YiMao

# 配置环境变量
cp .env.example .env
nano .env

# 编译运行
go build
./emby-telegram-bot
```

---

## 截图

### 入库通知
```
✅ 入库成功：电影名 (年份)
───────────────────
🎬 名称：电影名 (年份)
🏷️ 类别：类型
💎 质量：WEB-DL 4K
📦 总大小：3.76G
📁 文件数量：1 个
```

### AI 推荐
```
🔥 热门推荐
───────────────────
1. 沙丘2 (2024) ⭐8.5
2. 奥本海默 (2023) ⭐8.9
3. ...
```

---

## 开源协议

MIT License

---

## 链接

- GitHub: [xzb177/YiMao](https://github.com/xzb177/YiMao)
- Bot: [@oceancloudying_bot](https://t.me/oceancloudying_bot)
