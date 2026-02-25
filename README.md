# YiMao - Emby Telegram Bot

一个用于 Emby/MoviePilot 的 Telegram 机器人，解决媒体库求片和管理问题。

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

## 功能特性

| 功能 | 说明 |
|------|------|
| 🔍 **搜索求片** | 直接在 Telegram 搜片，自动调用 MoviePilot 下载 |
| 📬 **入库通知** | 新片入库实时推送，支持每日汇总模式 |
| 🔎 **库检查** | 求片前检查 Emby 媒体库，避免重复下载 |
| ⭐ **精选推荐** | 从 TMDB 获取热门高分内容，发现新片 |
| ✅ **审核机制** | 用户求片需管理员批准，避免滥用 |
| 🔗 **账号绑定** | 支持 MoviePilot 多用户，各管各的账号 |
| 📊 **配额限制** | 设置用户求片数量限制 |
| 📺 **季数选择** | TV 剧集支持分季订阅，S1-S2 独立选择 |
| 📎 **个人片单** | 收藏感兴趣的内容，支持分类管理 |

---

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/xzb177/YiMao/master/install.sh | bash
```

或者手动安装：

```bash
git clone https://github.com/xzb177/YiMao.git
cd YiMao
cp .env.example .env
# 编辑 .env 填写必需配置
docker compose up -d
```

---

## 配置说明

### 必需配置

| 环境变量 | 说明 | 获取方式 |
|---------|------|---------|
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | [@BotFather](https://t.me/BotFather) 创建机器人获取 |
| `MOVIEPILOT_URL` | MoviePilot 地址 | 如 http://localhost:4500 |
| `MOVIEPILOT_API_KEY` | MoviePilot API Key | MoviePilot 设置 -> API安全 |
| `ADMINS` | 管理员 Telegram ID | [@userinfobot](https://t.me/userinfobot) 发消息获取 |

### 可选配置

| 环境变量 | 说明 | 默认值 |
|---------|------|-------|
| `EMBY_URL` | Emby 地址 | - |
| `EMBY_API_KEY` | Emby API Key | - |
| `TMDB_API_KEY` | TMDB API Key | 内置默认 Key |
| `TZ` | 时区 | Asia/Shanghai |

---

## 命令列表

### 用户命令

| 命令 | 功能 |
|------|------|
| `/start` | 打开主菜单 |
| `/search 关键词` | 搜索影视 |
| `/link 账号 密码` | 绑定 MoviePilot 账号 |

### 管理员功能

私聊机器人可访问：
- 审核求片请求
- 设置通知模式
- 查看用户反馈
- 配额管理

---

## 常用操作

```bash
# 查看日志
docker logs -f emby-telegram-bot

# 重启服务
docker compose restart

# 停止服务
docker compose down

# 更新代码
git pull && docker compose up -d --build
```

---

## 架构设计

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Telegram  │────▶│    Bot      │────▶│  MoviePilot │
│   User      │     │  (Handlers) │     │   (下载)    │
└─────────────┘     └─────────────┘     └─────────────┘
                          │
                          ▼
                    ┌─────────────┐
                    │  Emby/Jellyfin│
                    │   (媒体库)   │
                    └─────────────┘
```

- **Handlers**: 处理 Telegram 回调和命令
- **Services**: 封装外部 API 调用（MoviePilot、Emby、TMDB）
- **Session**: 管理用户会话和状态
- **Security**: 限流、输入验证、敏感信息过滤

---

## 技术栈

- **语言**: Go 1.24+
- **框架**: Telegram Bot API
- **存储**: JSON 文件
- **部署**: Docker Compose

---

## 相关项目

- [MoviePilot](https://github.com/jxxghp/MoviePilot) - 媒体刮削下载
- [Emby](https://emby.media/) - 媒体服务器
- [TMDB](https://www.themoviedb.org/) - 影视数据库

---

## License

[MIT](LICENSE)
